package server

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"

	"github.com/gorilla/websocket"
	"google.golang.org/protobuf/proto"

	"github.com/lucasew/mise-ci/internal/core"
	pb "github.com/lucasew/mise-ci/internal/proto"
	"github.com/lucasew/mise-ci/internal/stream"
	"github.com/lucasew/mise-ci/internal/version"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for now
	},
}

type WebSocketServer struct {
	core   *core.Core
	logger *slog.Logger
}

func NewWebSocketServer(core *core.Core, logger *slog.Logger) *WebSocketServer {
	return &WebSocketServer{
		core:   core,
		logger: logger,
	}
}

// wsStreamAdapter adapts websocket.Conn to MessageStream interface
type wsStreamAdapter struct {
	conn *websocket.Conn
}

func (a *wsStreamAdapter) Send(msg *pb.ServerMessage) error {
	data, err := proto.Marshal(msg)
	if err != nil {
		return err
	}
	return a.conn.WriteMessage(websocket.BinaryMessage, data)
}

func (a *wsStreamAdapter) Recv() (*pb.WorkerMessage, error) {
	_, msgData, err := a.conn.ReadMessage()
	if err != nil {
		return nil, err
	}

	var workerMsg pb.WorkerMessage
	if err := proto.Unmarshal(msgData, &workerMsg); err != nil {
		return nil, err
	}
	return &workerMsg, nil
}

func (s *WebSocketServer) HandleConnect(w http.ResponseWriter, r *http.Request) {
	// 1. Auth - get token from Authorization header
	auth := r.Header.Get("Authorization")
	if auth == "" {
		http.Error(w, "missing authorization header", http.StatusUnauthorized)
		return
	}

	token := strings.TrimPrefix(auth, "Bearer ")
	runID, tokenType, err := s.core.ValidateToken(token)
	if err != nil {
		s.logger.Error("invalid token", "error", err)
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}

	if tokenType != core.TokenTypeWorker && tokenType != core.TokenTypePoolWorker {
		s.logger.Error("invalid token type for worker connection", "type", tokenType)
		http.Error(w, "invalid token type", http.StatusForbidden)
		return
	}

	// 2. Upgrade to WebSocket
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Error("failed to upgrade connection", "error", err)
		return
	}
	defer func() {
		_ = conn.Close()
	}()

	// 3. Para pool workers, aguardar e atribuir uma run da fila
	if tokenType == core.TokenTypePoolWorker {
		s.logger.Info("pool worker connected, waiting for run assignment")
		var queueStatus core.RunStatus
		runID, queueStatus, err = s.core.WaitForRun(r.Context())
		if err != nil {
			s.logger.Error("failed to get run from queue", "error", err)
			http.Error(w, "no runs available", http.StatusServiceUnavailable)
			return
		}
		s.logger.Info("run assigned to pool worker", "run_id", runID, "from_queue", queueStatus)
	}

	run, ok := s.core.GetRun(runID)
	if !ok {
		s.logger.Error("run not found", "run_id", runID)
		return
	}

	s.logger.Info("worker connected", "run_id", runID)

	wsAdapter := &wsStreamAdapter{conn: conn}

	err = stream.HandleBidiStream(
		r.Context(),
		wsAdapter,
		run.CommandCh,
		run.ResultCh,
		stream.BidiConfig[*pb.ServerMessage]{
			Logger: s.logger,
			OnConnect: func() error {
				// 3. Handshake - receive RunnerInfo
				workerMsg, err := wsAdapter.Recv()
				if err != nil {
					return fmt.Errorf("failed to read handshake: %w", err)
				}

				if info, ok := workerMsg.Payload.(*pb.WorkerMessage_RunnerInfo); ok {
					s.logger.Info("received runner info", "hostname", info.RunnerInfo.Hostname)
					if info.RunnerInfo.Version != version.Get() {
						s.logger.Warn("worker version mismatch", "worker", info.RunnerInfo.Version, "server", version.Get())
						// Signal retry
						select {
						case run.RetryCh <- struct{}{}:
						default:
						}
						// Send close frame so worker exits with 0
						cm := websocket.FormatCloseMessage(websocket.CloseNormalClosure, "version mismatch")
						_ = wsAdapter.conn.WriteMessage(websocket.CloseMessage, cm)
						return fmt.Errorf("worker version mismatch: worker=%s server=%s", info.RunnerInfo.Version, version.Get())
					}

					// Wait for ContextRequest
					workerMsg, err = wsAdapter.Recv()
					if err != nil {
						return fmt.Errorf("failed to read context request: %w", err)
					}
					if _, ok := workerMsg.Payload.(*pb.WorkerMessage_ContextRequest); !ok {
						return errors.New("expected ContextRequest after RunnerInfo")
					}

					// Send ContextResponse
					contextResp := &pb.ServerMessage{
						Payload: &pb.ServerMessage_ContextResponse{
							ContextResponse: &pb.ContextResponse{
								Env: run.Env,
							},
						},
					}
					if err := wsAdapter.Send(contextResp); err != nil {
						return fmt.Errorf("failed to send context response: %w", err)
					}

					// Update status to running when worker connects
					s.core.UpdateStatus(runID, core.StatusRunning, nil)
					s.core.AddLog(runID, "system", fmt.Sprintf("Worker connected: %s (%s/%s)", info.RunnerInfo.Hostname, info.RunnerInfo.Os, info.RunnerInfo.Arch))

					// Signal that worker is connected
					close(run.ConnectedCh)
					return nil
				}
				return errors.New("expected RunnerInfo as first message")
			},
			ShouldClose: func(msg *pb.ServerMessage) bool {
				_, ok := msg.Payload.(*pb.ServerMessage_Close)
				return ok
			},
		},
	)

	if err != nil {
		// Normal closure
    var closeErr *websocket.CloseError
    if errors.As(err, &closeErr) && (closeErr.Code == websocket.CloseNormalClosure || closeErr.Code == websocket.CloseGoingAway) {
      s.logger.Info("worker closed connection normally", "run_id", runID)
      return
    }

		// Connection closed by us (after sending Close message)
		var netErr *net.OpError
		if errors.As(err, &netErr) && errors.Is(netErr.Err, net.ErrClosed) {
			s.logger.Info("connection closed by server", "run_id", runID)
			return
		}

		s.logger.Error("worker connection error", "run_id", runID, "error", err)
		// Only log to system log if it's not a normal close/EOF that might be wrapped
    if !errors.Is(err, net.ErrClosed) && !errors.Is(err, io.EOF) {
			s.core.AddLog(runID, "system", fmt.Sprintf("Worker disconnected unexpectedly: %v", err))
			s.core.UpdateStatus(runID, core.StatusError, nil)
		}
	} else {
		s.logger.Info("worker disconnected", "run_id", runID)
	}
}
