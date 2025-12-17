package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	pb "github.com/lucasew/mise-ci/internal/proto"
)

var workerCmd = &cobra.Command{
	Use:   "worker",
	Short: "Runs the worker agent",
	RunE:  runWorker,
}

func init() {
	rootCmd.AddCommand(workerCmd)
	workerCmd.Flags().String("callback", "", "Server callback URL")
	workerCmd.Flags().String("token", "", "Authentication token")
	_ = viper.BindPFlag("callback", workerCmd.Flags().Lookup("callback"))
	_ = viper.BindPFlag("token", workerCmd.Flags().Lookup("token"))
}

func runWorker(cmd *cobra.Command, args []string) error {
	return startWorker()
}

func startWorker() error {
	callback := viper.GetString("callback")
	token := viper.GetString("token")

	if callback == "" || token == "" {
		return fmt.Errorf("callback and token must be set (via flags or env MISE_CI_CALLBACK/MISE_CI_TOKEN)")
	}

	target := callback
	if u, err := url.Parse(callback); err == nil && u.Host != "" {
		target = u.Host
	}

	logger.Info("connecting to server", "target", target)

	conn, err := grpc.Dial(target, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("failed to connect to server: %w", err)
	}
	defer conn.Close()

	client := pb.NewServerClient(conn)

	ctx := metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer "+token)

	stream, err := client.Connect(ctx)
	if err != nil {
		return fmt.Errorf("failed to open stream: %w", err)
	}

	hostname, _ := os.Hostname()
	info := &pb.WorkerMessage{
		Payload: &pb.WorkerMessage_RunnerInfo{
			RunnerInfo: &pb.RunnerInfo{
				Hostname: hostname,
				Os:       runtime.GOOS,
				Arch:     runtime.GOARCH,
			},
		},
	}
	if err := safeSend(stream, info); err != nil {
		return fmt.Errorf("failed to send runner info: %w", err)
	}

	logger.Info("connected and info sent")

	var (
		mu         sync.Mutex
		cancelFunc context.CancelFunc
	)

	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("stream error: %w", err)
		}

		// Cancel previous operation if any
		mu.Lock()
		if cancelFunc != nil {
			cancelFunc()
		}
		// Create new context for this operation
		opCtx, cancel := context.WithCancel(context.Background())
		cancelFunc = cancel
		mu.Unlock()

		switch payload := msg.Payload.(type) {
		case *pb.ServerMessage_Copy:
			go func() {
				defer cancel()
				if err := handleCopy(opCtx, stream, msg.Id, payload.Copy, logger); err != nil {
					sendError(stream, msg.Id, err)
				}
			}()
		case *pb.ServerMessage_Run:
			go func() {
				defer cancel()
				if err := handleRun(opCtx, stream, msg.Id, payload.Run, logger); err != nil {
					sendError(stream, msg.Id, err)
				}
			}()
		case *pb.ServerMessage_Close:
			logger.Info("received close")
			cancel()
			return nil
		default:
			logger.Warn("unknown message type")
			cancel()
		}
	}
	return nil
}

// We need a mutex for sending to stream to be safe
var sendMu sync.Mutex

func safeSend(stream pb.Server_ConnectClient, msg *pb.WorkerMessage) error {
	sendMu.Lock()
	defer sendMu.Unlock()
	return stream.Send(msg)
}

func sendError(stream pb.Server_ConnectClient, id uint64, err error) {
	_ = safeSend(stream, &pb.WorkerMessage{
		Id: id,
		Payload: &pb.WorkerMessage_Error{
			Error: &pb.Error{
				Message: err.Error(),
			},
		},
	})
}

func handleCopy(ctx context.Context, stream pb.Server_ConnectClient, id uint64, cmd *pb.Copy, logger *slog.Logger) error {
	logger.Info("handling copy", "direction", cmd.Direction, "source", cmd.Source, "dest", cmd.Dest)

	if cmd.Direction == pb.Copy_TO_WORKER {
		if strings.HasSuffix(cmd.Source, ".git") {
			return gitClone(ctx, cmd, logger)
		}
		return fmt.Errorf("only git clone supported for now")
	} else if cmd.Direction == pb.Copy_FROM_WORKER {
		return sendFile(ctx, stream, id, cmd.Source, logger)
	}
	return nil
}

func gitClone(ctx context.Context, cmd *pb.Copy, logger *slog.Logger) error {
	u, err := url.Parse(cmd.Source)
	if err != nil {
		return err
	}

	if cmd.Creds != nil {
		if t := cmd.Creds.GetToken(); t != "" {
			u.User = url.UserPassword("x-access-token", t)
		} else if b := cmd.Creds.GetBasic(); b != nil {
			u.User = url.UserPassword(b.Username, b.Password)
		}
	}

	c := exec.CommandContext(ctx, "git", "clone", u.String(), cmd.Dest)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		return fmt.Errorf("git clone failed: %w", err)
	}
	return nil
}

func sendFile(ctx context.Context, stream pb.Server_ConnectClient, id uint64, path string, logger *slog.Logger) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	buf := make([]byte, 1024*64)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		n, err := f.Read(buf)
		if n > 0 {
			if err := safeSend(stream, &pb.WorkerMessage{
				Id: id,
				Payload: &pb.WorkerMessage_FileChunk{
					FileChunk: &pb.FileChunk{
						Data: buf[:n],
						Eof:  false,
					},
				},
			}); err != nil {
				return err
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}

	return safeSend(stream, &pb.WorkerMessage{
		Id: id,
		Payload: &pb.WorkerMessage_FileChunk{
			FileChunk: &pb.FileChunk{
				Data: nil,
				Eof:  true,
			},
		},
	})
}

func handleRun(ctx context.Context, stream pb.Server_ConnectClient, id uint64, cmd *pb.Run, logger *slog.Logger) error {
	logger.Info("handling run", "cmd", cmd.Cmd, "args", cmd.Args)

	c := exec.CommandContext(ctx, cmd.Cmd, cmd.Args...)
	c.Dir = cmd.Workdir
	c.Env = os.Environ()
	for k, v := range cmd.Env {
		c.Env = append(c.Env, fmt.Sprintf("%s=%s", k, v))
	}

	stdout, err := c.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := c.StderrPipe()
	if err != nil {
		return err
	}

	if err := c.Start(); err != nil {
		return err
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		streamOutput(ctx, stream, id, stdout, pb.Output_STDOUT)
	}()
	go func() {
		defer wg.Done()
		streamOutput(ctx, stream, id, stderr, pb.Output_STDERR)
	}()

	err = c.Wait()
	wg.Wait()

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			exitCode = 1
		}
	}

	return safeSend(stream, &pb.WorkerMessage{
		Id: id,
		Payload: &pb.WorkerMessage_Done{
			Done: &pb.Done{
				ExitCode: int32(exitCode),
			},
		},
	})
}

func streamOutput(ctx context.Context, stream pb.Server_ConnectClient, id uint64, r io.Reader, type_ pb.Output_Stream) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
		}

		data := scanner.Bytes()
		b := make([]byte, len(data)+1)
		copy(b, data)
		b[len(data)] = '\n'

		_ = safeSend(stream, &pb.WorkerMessage{
			Id: id,
			Payload: &pb.WorkerMessage_Output{
				Output: &pb.Output{
					Stream: type_,
					Data:   b,
				},
			},
		})
	}
}
