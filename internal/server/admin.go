package server

import (
	"fmt"
	"net/http"
	"strconv"
)

func (s *UIServer) HandleAdminCleanup(w http.ResponseWriter, r *http.Request) {
    // Only allow GET to render the page
	if r.Method != http.MethodGet {
        http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
        return
    }

	data := map[string]interface{}{
		"Title": "Maintenance",
        "Version": "1.0.0", // TODO: use actual version
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
