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
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"google.golang.org/protobuf/proto"

	pb "github.com/lucasew/mise-ci/internal/proto"
	"github.com/lucasew/mise-ci/internal/version"
)

var workerCmd = &cobra.Command{
	Use:   "worker",
	Short: "Runs the worker agent",
	RunE:  runWorker,
}

var workerEnv []string

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

func performHandshake(conn *websocket.Conn) (map[string]string, error) {
	// Send handshake
	hostname, err := os.Hostname()
	if err != nil {
		return nil, fmt.Errorf("failed to get hostname: %w", err)
	}
	info := &pb.WorkerMessage{
		Payload: &pb.WorkerMessage_RunnerInfo{
			RunnerInfo: &pb.RunnerInfo{
				Hostname: hostname,
				Os:       runtime.GOOS,
				Arch:     runtime.GOARCH,
				Version:  version.Get(),
			},
		},
	}
	if err := wsafeSend(conn, info); err != nil {
		return nil, fmt.Errorf("failed to send runner info: %w", err)
	}

	// Request context
	if err := wsafeSend(conn, &pb.WorkerMessage{
		Payload: &pb.WorkerMessage_ContextRequest{
			ContextRequest: &pb.ContextRequest{},
		},
	}); err != nil {
		return nil, fmt.Errorf("failed to send context request: %w", err)
	}

	// Receive context
	_, msgData, err := conn.ReadMessage()
	if err != nil {
		return nil, fmt.Errorf("failed to read context response: %w", err)
	}

	var ctxMsg pb.ServerMessage
	if err := proto.Unmarshal(msgData, &ctxMsg); err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}

	ctxResp, ok := ctxMsg.Payload.(*pb.ServerMessage_ContextResponse)
	if !ok {
		return nil, fmt.Errorf("expected ContextResponse, got %T", ctxMsg.Payload)
	}

	return ctxResp.ContextResponse.Env, nil
}

func startWorker() error {
	callback := viper.GetString("callback")
	token := viper.GetString("token")

	if callback == "" || token == "" {
		return fmt.Errorf("callback and token must be set (via flags or env MISE_CI_CALLBACK/MISE_CI_TOKEN)")
	}

	// Watchdog: kill worker if handshake not finished or no command received in 30s
	watchdog := time.AfterFunc(30*time.Second, func() {
		logger.Error("watchdog timeout: handshake not finished or no command received in 30s")
		os.Exit(1)
	})
	defer watchdog.Stop()

	// Build WebSocket URL
	wsURL := callback
	if u, err := url.Parse(callback); err == nil {
		// Convert http:// to ws:// and https:// to wss://
		switch u.Scheme {
		case "http":
			u.Scheme = "ws"
		case "https":
			u.Scheme = "wss"
		case "":
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
	defer func() {
		if err := conn.Close(); err != nil {
			logger.Error("failed to close websocket connection", "error", err)
		}
	}()

	// Perform handshake and get context
	env, err := performHandshake(conn)
	if err != nil {
		return fmt.Errorf("handshake failed: %w", err)
	}

	// Initialize environment
	workerEnv = os.Environ()
	for k, v := range env {
		workerEnv = append(workerEnv, fmt.Sprintf("%s=%s", k, v))
	}

	logger.Info("connected and context received", "env_vars", len(env))

	var cancelFunc context.CancelFunc

	for {
		_, msgData, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				break
			}
			logger.Warn("connection lost", "error", err)
			return nil
		}

		var msg pb.ServerMessage
		if err := proto.Unmarshal(msgData, &msg); err != nil {
			return fmt.Errorf("unmarshal error: %w", err)
		}

		// Cancel previous operation if any
		if cancelFunc != nil {
			cancelFunc()
		}
		// Create new context for this operation
		opCtx, cancel := context.WithCancel(context.Background())
		cancelFunc = cancel

		logger.Debug("received message", "type", fmt.Sprintf("%T", msg.Payload), "id", msg.Id)

		switch payload := msg.Payload.(type) {
		case *pb.ServerMessage_Copy:
			watchdog.Stop()
			go func() {
				defer func() {
					if r := recover(); r != nil {
						logger.Error("panic in copy handler", "panic", r)
						wsendError(conn, msg.Id, fmt.Errorf("panic: %v", r))
					}
				}()
				defer cancel()

				if payload.Copy == nil {
					logger.Error("received copy message with nil payload")
					wsendError(conn, msg.Id, fmt.Errorf("nil copy payload"))
					return
				}

				if err := handleCopy(opCtx, conn, msg.Id, payload.Copy, logger); err != nil {
					wsendError(conn, msg.Id, err)
				}
			}()
		case *pb.ServerMessage_Run:
			watchdog.Stop()
			go func() {
				defer func() {
					if r := recover(); r != nil {
						logger.Error("panic in run handler", "panic", r)
						wsendError(conn, msg.Id, fmt.Errorf("panic: %v", r))
					}
				}()
				defer cancel()

				if payload.Run == nil {
					logger.Error("received run message with nil payload")
					wsendError(conn, msg.Id, fmt.Errorf("nil run payload"))
					return
				}

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

	switch cmd.Direction {
	case pb.Copy_TO_WORKER:
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
	case pb.Copy_FROM_WORKER:
		return sendFile(ctx, conn, id, cmd.Source, logger)
	}
	return nil
}

func sendFile(ctx context.Context, conn *websocket.Conn, id uint64, path string, logger *slog.Logger) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() {
		if err := f.Close(); err != nil {
			logger.Error("failed to close file", "error", err)
		}
	}()

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
	c.Env = make([]string, 0, len(workerEnv)+len(cmd.Env))
	c.Env = append(c.Env, workerEnv...)
	for k, v := range cmd.Env {
		c.Env = append(c.Env, fmt.Sprintf("%s=%s", k, v))
	}

	// Log environment keys for debugging
	envKeys := make([]string, 0, len(c.Env))
	for _, e := range c.Env {
		if idx := strings.Index(e, "="); idx != -1 {
			envKeys = append(envKeys, e[:idx])
		}
	}
	logger.Debug("running command with env", "keys", envKeys)

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
