package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/lucasew/mise-ci/internal/core"
)

type DispatcherServer struct {
	core   *core.Core
	logger *slog.Logger
}

func NewDispatcherServer(core *core.Core, logger *slog.Logger) *DispatcherServer {
	return &DispatcherServer{
		core:   core,
		logger: logger,
	}
}

type PollResponse struct {
	RunID       string `json:"run_id"`
	WorkerToken string `json:"worker_token"`
}

// HandlePoll permite que um dispatcher pegue a próxima run da fila
func (s *DispatcherServer) HandlePoll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Validar token de pool worker
	auth := r.Header.Get("Authorization")
	if auth == "" {
		http.Error(w, "missing authorization header", http.StatusUnauthorized)
		return
	}

	token := strings.TrimPrefix(auth, "Bearer ")
	_, tokenType, err := s.core.ValidateToken(token)
	if err != nil {
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}

	if tokenType != core.TokenTypePoolWorker {
		http.Error(w, "invalid token type - pool worker token required", http.StatusForbidden)
		return
	}

	// Wait for the next available run. This will block until a run is ready or timeout.
	runID, err := s.core.DequeueNextRun(r.Context())
	if err != nil {
		s.logger.Error("failed to dequeue run", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	// If runID is empty, it means the request timed out.
	if runID == "" {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// Gerar token worker específico para esta run
	workerToken, err := s.core.GenerateWorkerToken(runID)
	if err != nil {
		http.Error(w, "failed to generate worker token", http.StatusInternalServerError)
		return
	}

	// Retornar runID + token
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(PollResponse{
		RunID:       runID,
		WorkerToken: workerToken,
	}); err != nil {
		// Log error, mas é tarde para enviar status HTTP
		s.logger.Error("failed to encode poll response", "error", err)
	}
}
