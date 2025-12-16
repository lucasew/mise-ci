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

	pb "mise-ci/internal/proto"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := run(logger); err != nil {
		logger.Error("worker failed", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	callback := os.Getenv("MISE_CI_CALLBACK")
	token := os.Getenv("MISE_CI_TOKEN")

	if callback == "" || token == "" {
		return fmt.Errorf("MISE_CI_CALLBACK and MISE_CI_TOKEN must be set")
	}

	target := callback
	if u, err := url.Parse(callback); err == nil && u.Host != "" {
		target = u.Host
	}

	logger.Info("connecting to matriz", "target", target)

	conn, err := grpc.Dial(target, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("failed to connect to matriz: %w", err)
	}
	defer conn.Close()

	client := pb.NewMatrizClient(conn)

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
	if err := stream.Send(info); err != nil {
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
		case *pb.MatrizMessage_Copy:
			go func() {
				defer cancel()
				if err := handleCopy(opCtx, stream, msg.Id, payload.Copy, logger); err != nil {
					sendError(stream, msg.Id, err)
				}
			}()
		case *pb.MatrizMessage_Run:
			go func() {
				defer cancel()
				if err := handleRun(opCtx, stream, msg.Id, payload.Run, logger); err != nil {
					sendError(stream, msg.Id, err)
				}
			}()
		case *pb.MatrizMessage_Close:
			logger.Info("received close")
			cancel() // Cancel whatever is running
			return nil
		default:
			logger.Warn("unknown message type")
			cancel()
		}
	}
	return nil
}

func sendError(stream pb.Matriz_ConnectClient, id uint64, err error) {
	// Note: concurrent access to stream.Send must be synchronized if strict thread safety is needed.
	// However, gRPC streams are thread-safe for one-way Send.
	// Wait, grpc-go stream.Send is safe to be called concurrently with Recv, BUT
	// concurrent calls to Send are NOT safe.
	// Since we spawn a goroutine per message, but Matriz likely sends messages sequentially for a run,
	// we shouldn't have concurrent Sends unless Matriz sends new command before old one finishes.
	// But if we have overlapping commands (which we handle by cancelling old one), we might have overlap.
	// We should probably lock Send.
	// For simplicity in this implementation, we assume mostly sequential or tolerated race on Close/Error.

	// A better approach would be a send channel.

	_ = safeSend(stream, &pb.WorkerMessage{
		Id: id,
		Payload: &pb.WorkerMessage_Error{
			Error: &pb.Error{
				Message: err.Error(),
			},
		},
	})
}

// We need a mutex for sending to stream to be safe
var sendMu sync.Mutex

func safeSend(stream pb.Matriz_ConnectClient, msg *pb.WorkerMessage) error {
	sendMu.Lock()
	defer sendMu.Unlock()
	return stream.Send(msg)
}

func handleCopy(ctx context.Context, stream pb.Matriz_ConnectClient, id uint64, cmd *pb.Copy, logger *slog.Logger) error {
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

func sendFile(ctx context.Context, stream pb.Matriz_ConnectClient, id uint64, path string, logger *slog.Logger) error {
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

func handleRun(ctx context.Context, stream pb.Matriz_ConnectClient, id uint64, cmd *pb.Run, logger *slog.Logger) error {
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
			// If killed by context, exitCode might be -1 or similar.
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

func streamOutput(ctx context.Context, stream pb.Matriz_ConnectClient, id uint64, r io.Reader, type_ pb.Output_Stream) {
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
