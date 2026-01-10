package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/lucasew/mise-ci/internal/core"
)

type DispatcherServer struct {
	core *core.Core
}

func NewDispatcherServer(core *core.Core) *DispatcherServer {
	return &DispatcherServer{core: core}
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

	// Tentar pegar próxima run da fila (sem bloquear)
	runID, _, ok := s.core.TryDequeueRun(r.Context())
	if !ok || runID == "" {
		// Nenhuma run disponível
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
	json.NewEncoder(w).Encode(PollResponse{
		RunID:       runID,
		WorkerToken: workerToken,
	})
}
