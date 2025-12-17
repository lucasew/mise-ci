package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"google.golang.org/protobuf/proto"

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

	// Build WebSocket URL
	wsURL := callback
	if u, err := url.Parse(callback); err == nil {
		// Convert http:// to ws:// and https:// to wss://
		if u.Scheme == "http" {
			u.Scheme = "ws"
		} else if u.Scheme == "https" {
			u.Scheme = "wss"
		} else if u.Scheme == "" {
			u.Scheme = "ws"
		}
		u.Path = "/ws"
		wsURL = u.String()
	}

	logger.Info("connecting to server", "url", wsURL)

	// Connect to WebSocket with auth header
	headers := http.Header{}
	headers.Add("Authorization", "Bearer "+token)

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, headers)
	if err != nil {
		return fmt.Errorf("failed to connect to server: %w", err)
	}
	defer conn.Close()

	// Send handshake
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
	if err := wsafeSend(conn, info); err != nil {
		return fmt.Errorf("failed to send runner info: %w", err)
	}

	logger.Info("connected and info sent")

	var (
		mu         sync.Mutex
		cancelFunc context.CancelFunc
	)

	for {
		_, msgData, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				break
			}
			return fmt.Errorf("read error: %w", err)
		}

		var msg pb.ServerMessage
		if err := proto.Unmarshal(msgData, &msg); err != nil {
			return fmt.Errorf("unmarshal error: %w", err)
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
				if err := handleCopy(opCtx, conn, msg.Id, payload.Copy, logger); err != nil {
					wsendError(conn, msg.Id, err)
				}
			}()
		case *pb.ServerMessage_Run:
			go func() {
				defer cancel()
				if err := handleRun(opCtx, conn, msg.Id, payload.Run, logger); err != nil {
					wsendError(conn, msg.Id, err)
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

// We need a mutex for sending to WebSocket to be safe
var sendMu sync.Mutex

func wsafeSend(conn *websocket.Conn, msg *pb.WorkerMessage) error {
	sendMu.Lock()
	defer sendMu.Unlock()

	data, err := proto.Marshal(msg)
	if err != nil {
		return err
	}
	return conn.WriteMessage(websocket.BinaryMessage, data)
}

func wsendError(conn *websocket.Conn, id uint64, err error) {
	_ = wsafeSend(conn, &pb.WorkerMessage{
		Id: id,
		Payload: &pb.WorkerMessage_Error{
			Error: &pb.Error{
				Message: err.Error(),
			},
		},
	})
}

func handleCopy(ctx context.Context, conn *websocket.Conn, id uint64, cmd *pb.Copy, logger *slog.Logger) error {
	logger.Info("handling copy", "direction", cmd.Direction, "source", cmd.Source, "dest", cmd.Dest)

	if cmd.Direction == pb.Copy_TO_WORKER {
		// Receive file data from server (sent inline in Copy message)
		if err := os.WriteFile(cmd.Dest, cmd.Data, 0644); err != nil {
			return fmt.Errorf("failed to write file: %w", err)
		}
		logger.Info("file received", "dest", cmd.Dest, "size", len(cmd.Data))

		return wsafeSend(conn, &pb.WorkerMessage{
			Id: id,
			Payload: &pb.WorkerMessage_Done{
				Done: &pb.Done{ExitCode: 0},
			},
		})
	} else if cmd.Direction == pb.Copy_FROM_WORKER {
		return sendFile(ctx, conn, id, cmd.Source, logger)
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

func sendFile(ctx context.Context, conn *websocket.Conn, id uint64, path string, logger *slog.Logger) error {
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
			if err := wsafeSend(conn, &pb.WorkerMessage{
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

	return wsafeSend(conn, &pb.WorkerMessage{
		Id: id,
		Payload: &pb.WorkerMessage_FileChunk{
			FileChunk: &pb.FileChunk{
				Data: nil,
				Eof:  true,
			},
		},
	})
}

func handleRun(ctx context.Context, conn *websocket.Conn, id uint64, cmd *pb.Run, logger *slog.Logger) error {
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
		streamOutput(ctx, conn, id, stdout, pb.Output_STDOUT)
	}()
	go func() {
		defer wg.Done()
		streamOutput(ctx, conn, id, stderr, pb.Output_STDERR)
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

	return wsafeSend(conn, &pb.WorkerMessage{
		Id: id,
		Payload: &pb.WorkerMessage_Done{
			Done: &pb.Done{
				ExitCode: int32(exitCode),
			},
		},
	})
}

func streamOutput(ctx context.Context, conn *websocket.Conn, id uint64, r io.Reader, type_ pb.Output_Stream) {
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

		_ = wsafeSend(conn, &pb.WorkerMessage{
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
