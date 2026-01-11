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

    count, err := s.core.CleanupStuckRuns(r.Context(), cutoff, limit, s.publicURL)

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
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

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
		"Now":     time.Now(),
	}

	if err := s.engine.Render(w, "templates/pages/admin_tokens.html", data); err != nil {
		s.logger.Error("failed to render template", "template", "admin_tokens", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func (s *UIServer) HandleGeneratePoolToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	expiryStr := r.FormValue("expiry")
	var expiry time.Duration

	if expiryStr == "never" || expiryStr == "" {
		expiry = 0 // Token sem expiração
	} else {
		// Parse duration em dias
		days, err := strconv.Atoi(expiryStr)
		if err != nil || days < 0 {
			http.Error(w, "Invalid expiry value", http.StatusBadRequest)
			return
		}
		expiry = time.Duration(days) * 24 * time.Hour
	}

	token, err := s.core.GeneratePoolWorkerTokenWithExpiry(expiry)
	if err != nil {
		s.logger.Error("failed to generate pool token", "error", err)
		http.Error(w, "Failed to generate token", http.StatusInternalServerError)
		return
	}

	response := map[string]interface{}{
		"token":  token,
		"expiry": expiryStr,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		s.logger.Error("failed to encode json response", "error", err)
	}
}
