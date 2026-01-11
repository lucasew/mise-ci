package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/lucasew/mise-ci/internal/version"
)

func (s *UIServer) HandleAdminCleanup(w http.ResponseWriter, r *http.Request) {
    // Only allow GET to render the page
	if r.Method != http.MethodGet {
        http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
        return
    }

	data := map[string]interface{}{
		"Title": "Maintenance",
        "Version": version.Get(),
	}

	if err := s.engine.Render(w, "templates/pages/admin_cleanup.html", data); err != nil {
		s.logger.Error("failed to render template", "template", "admin_cleanup", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func (s *UIServer) HandleBackfillRepoURLs(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
        return
    }

    limitStr := r.FormValue("limit")
    limit, err := strconv.Atoi(limitStr)
    if err != nil || limit <= 0 {
        limit = 100
    }

    // Run in background to avoid blocking? Or blocking to show result?
    // Let's block for now as it's a batch operation
    count, err := s.core.FixMissingRepoURLs(r.Context(), limit)

    data := map[string]interface{}{
        "Title": "Maintenance",
    }

    if err != nil {
        data["Error"] = fmt.Sprintf("Failed to backfill: %v", err)
    } else {
        data["Message"] = fmt.Sprintf("Successfully processed %d runs", count)
    }

    if err := s.engine.Render(w, "templates/pages/admin_cleanup.html", data); err != nil {
        s.logger.Error("failed to render template", "error", err)
    }
}

func (s *UIServer) HandleCleanupStuckRuns(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
        return
    }

    limitStr := r.FormValue("limit")
    limit, err := strconv.Atoi(limitStr)
    if err != nil || limit <= 0 {
        limit = 100
    }

    // Runs older than server start time are considered stuck
    // (since in-memory state is lost on restart)
    cutoff := s.core.StartTime

    count, err := s.core.CleanupStuckRuns(r.Context(), cutoff, limit)

    data := map[string]interface{}{
        "Title": "Maintenance",
    }

    if err != nil {
        data["Error"] = fmt.Sprintf("Failed to cleanup: %v", err)
    } else {
        data["Message"] = fmt.Sprintf("Successfully cleaned up %d stuck runs", count)
    }

    if err := s.engine.Render(w, "templates/pages/admin_cleanup.html", data); err != nil {
        s.logger.Error("failed to render template", "error", err)
    }
}

func (s *UIServer) HandleAdminTokens(w http.ResponseWriter, r *http.Request) {
    tokens, err := s.core.ListWorkerTokens(r.Context())
    if err != nil {
        s.logger.Error("failed to list worker tokens", "error", err)
        http.Error(w, "Internal Server Error", http.StatusInternalServerError)
        return
    }

    data := map[string]interface{}{
        "Title":   "Worker Tokens",
        "Version": version.Get(),
        "Tokens":  tokens,
    }

    if err := s.engine.Render(w, "templates/pages/admin_tokens.html", data); err != nil {
        s.logger.Error("failed to render template", "template", "admin_tokens", "error", err)
        http.Error(w, "Internal Server Error", http.StatusInternalServerError)
    }
}

func (s *UIServer) HandleCreateToken(w http.ResponseWriter, r *http.Request) {
    name := r.FormValue("name")
    if name == "" {
        http.Error(w, "Token name is required", http.StatusBadRequest)
        return
    }

    expiryDaysStr := r.FormValue("expiry")
    expiryDays, err := strconv.Atoi(expiryDaysStr)
    if err != nil {
        http.Error(w, "Invalid expiry value", http.StatusBadRequest)
        return
    }

    var expiry time.Duration
    if expiryDays > 0 {
        expiry = time.Duration(expiryDays) * 24 * time.Hour
    }

    token, err := s.core.CreateWorkerToken(r.Context(), name, expiry)
    if err != nil {
        s.logger.Error("failed to create token", "error", err)
        http.Error(w, "Failed to create token", http.StatusInternalServerError)
        return
    }

    w.Header().Set("Content-Type", "application/json")
    if err := json.NewEncoder(w).Encode(map[string]string{"token": token}); err != nil {
        s.logger.Error("failed to encode json response", "error", err)
    }
}

func (s *UIServer) HandleRevokeToken(w http.ResponseWriter, r *http.Request) {
    id := r.PathValue("id")
    if err := s.core.RevokeWorkerToken(r.Context(), id); err != nil {
        s.logger.Error("failed to revoke token", "error", err)
        http.Error(w, "Failed to revoke token", http.StatusInternalServerError)
        return
    }
    http.Redirect(w, r, "/ui/admin/tokens", http.StatusFound)
}

func (s *UIServer) HandleDeleteToken(w http.ResponseWriter, r *http.Request) {
    id := r.PathValue("id")
    if err := s.core.DeleteWorkerToken(r.Context(), id); err != nil {
        s.logger.Error("failed to delete token", "error", err)
        http.Error(w, "Failed to delete token", http.StatusInternalServerError)
        return
    }
    http.Redirect(w, r, "/ui/admin/tokens", http.StatusFound)
}
