package server

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"

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
	core         *core.Core
	logger       *slog.Logger
	assignRunMux sync.Mutex
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

// validateWorkerAuth handles the initial authentication and run assignment for a connecting worker.
// It returns the run ID, an HTTP status code for errors, and an error.
// A run ID of "" with no error indicates no jobs are available.
func (s *WebSocketServer) validateWorkerAuth(r *http.Request) (string, int, error) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return "", http.StatusUnauthorized, errors.New("missing authorization header")
	}
if !strings.HasPrefix(authHeader, "Bearer ") {
		return "", http.StatusUnauthorized, errors.New("authorization header format must be 'Bearer {token}'")
	}
	token := strings.TrimPrefix(authHeader, "Bearer ")

	// This new function will handle both token types
	runID, err := s.core.ValidateWorkerToken(token)
	if err != nil {
		s.logger.Error("invalid worker token", "error", err)
		return "", http.StatusUnauthorized, fmt.Errorf("invalid token: %w", err)
	}

	// If runID is returned, the token was a specific worker token.
	if runID != "" {
		if _, ok := s.core.GetRun(runID); !ok {
			return "", http.StatusNotFound, fmt.Errorf("run %s not found", runID)
		}
		return runID, http.StatusOK, nil
	}

	// If no runID, it was a pool token. Dequeue a run.
	s.assignRunMux.Lock()
	defer s.assignRunMux.Unlock()
	runID, err = s.core.DequeueNextRun(r.Context())
	if err != nil {
		s.logger.Error("failed to dequeue run", "error", err)
		// Propagate the error up to be handled by the caller
		return "", http.StatusInternalServerError, fmt.Errorf("failed to dequeue run: %w", err)
	}

	// runID can be "" if no jobs are available.
	return runID, http.StatusOK, nil
}

func (s *WebSocketServer) HandleConnect(w http.ResponseWriter, r *http.Request) {
	runID, status, err := s.validateWorkerAuth(r)
	if err != nil {
		http.Error(w, err.Error(), status)
		return
	}

	// Upgrade to WebSocket
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Error("failed to upgrade connection", "error", err)
		return
	}
	defer func() {
		_ = conn.Close()
	}()
	wsAdapter := &wsStreamAdapter{conn: conn}

	// Handshake - receive RunnerInfo
	workerMsg, err := wsAdapter.Recv()
	if err != nil {
		s.logger.Error("failed to read handshake", "error", err)
		return
	}
	info, ok := workerMsg.Payload.(*pb.WorkerMessage_RunnerInfo)
	if !ok {
		s.logger.Error("expected RunnerInfo as first message")
		return
	}
	s.logger.Info("worker connected",
		"hostname", info.RunnerInfo.Hostname,
		"version", info.RunnerInfo.Version,
		"os", info.RunnerInfo.Os,
		"arch", info.RunnerInfo.Arch,
	)

	// Wait for ContextRequest
	if _, err := wsAdapter.Recv(); err != nil {
		s.logger.Error("failed to read context request", "error", err)
		return
	}

	// If no runID, it means no job was available for a pool worker.
	if runID == "" {
		s.logger.Info("no pending runs available for worker")
		// Send empty context to signal no jobs and close connection.
		_ = wsAdapter.Send(&pb.ServerMessage{
			Payload: &pb.ServerMessage_ContextResponse{
				ContextResponse: &pb.ContextResponse{Env: nil},
			},
		})
		return
	}

	// We have a runID, either from token or dequeued.
	run, ok := s.core.GetRun(runID)
	if !ok {
		// This should be rare, as validateWorkerAuth checks this.
		s.logger.Error("assigned run not found", "run_id", runID)
		return
	}

	s.logger.Info("assigned run to worker", "run_id", runID)

	// Version check
	if info.RunnerInfo.Version != version.Get() {
		s.logger.Warn("worker version mismatch", "worker", info.RunnerInfo.Version, "server", version.Get())
		select {
		case run.RetryCh <- struct{}{}:
		default:
		}
		cm := websocket.FormatCloseMessage(websocket.CloseNormalClosure, "version mismatch")
		_ = wsAdapter.conn.WriteMessage(websocket.CloseMessage, cm)
		return
	}

	// Send ContextResponse with run environment
	if err := wsAdapter.Send(&pb.ServerMessage{
		Payload: &pb.ServerMessage_ContextResponse{
			ContextResponse: &pb.ContextResponse{Env: run.Env},
		},
	}); err != nil {
		s.logger.Error("failed to send context response", "run_id", runID, "error", err)
		return
	}

	// Update run status and start streaming
	s.core.UpdateStatus(runID, core.StatusRunning, nil)
	s.core.AddLog(runID, "system", fmt.Sprintf("Worker connected: %s (%s/%s)", info.RunnerInfo.Hostname, info.RunnerInfo.Os, info.RunnerInfo.Arch))
	close(run.ConnectedCh)

	err = stream.HandleBidiStream(
		r.Context(),
		wsAdapter,
		run.CommandCh,
		run.ResultCh,
		stream.BidiConfig[*pb.ServerMessage]{
			Logger: s.logger,
			ShouldClose: func(msg *pb.ServerMessage) bool {
				_, ok := msg.Payload.(*pb.ServerMessage_Close)
				return ok
			},
		},
	)

	// Handle disconnection
	if err != nil {
		var closeErr *websocket.CloseError
		if errors.As(err, &closeErr) && (closeErr.Code == websocket.CloseNormalClosure || closeErr.Code == websocket.CloseGoingAway) {
			s.logger.Info("worker closed connection normally", "run_id", runID)
			return
		}
		var netErr *net.OpError
		if errors.As(err, &netErr) && errors.Is(netErr.Err, net.ErrClosed) {
			s.logger.Info("connection closed by server", "run_id", runID)
			return
		}
		s.logger.Error("worker connection error", "run_id", runID, "error", err)
		if !errors.Is(err, net.ErrClosed) && !errors.Is(err, io.EOF) {
			s.core.AddLog(runID, "system", fmt.Sprintf("Worker disconnected unexpectedly: %v", err))
			s.core.UpdateStatus(runID, core.StatusError, nil)
		}
	} else {
		s.logger.Info("worker disconnected", "run_id", runID)
	}
}
