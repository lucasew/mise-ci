package server

import (
	"embed"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/abiosoft/mold"
	"github.com/lucasew/mise-ci/internal/core"
	"github.com/lucasew/mise-ci/internal/httputil"
	"github.com/lucasew/mise-ci/internal/sseutil"
)

//go:embed templates/*
var templatesFS embed.FS

type UIServer struct {
	core   *core.Core
	logger *slog.Logger
	engine mold.Engine
}

func NewUIServer(c *core.Core, logger *slog.Logger) *UIServer {
	funcMap := template.FuncMap{
		"formatTime": func(t time.Time) string {
			return t.Format("2006-01-02 15:04:05")
		},
		"formatDuration": func(start time.Time, end *time.Time) string {
			if end == nil {
				return time.Since(start).Round(time.Second).String()
			}
			return end.Sub(start).Round(time.Second).String()
		},
		"statusClass": func(status core.RunStatus) string {
			switch status {
			case core.StatusSuccess:
				return "success"
			case core.StatusFailure:
				return "failure"
			case core.StatusError:
				return "error"
			case core.StatusRunning:
				return "running"
			case core.StatusScheduled:
				return "scheduled"
			default:
				return ""
			}
		},
	}

	engine := mold.Must(mold.New(templatesFS, mold.WithLayout("templates/layouts/layout.html"), mold.WithFuncMap(funcMap)))

	return &UIServer{
		core:   c,
		logger: logger,
		engine: engine,
	}
}

func (s *UIServer) HandleIndex(w http.ResponseWriter, r *http.Request) {
	runs := s.core.GetAllRuns()

	// Sort by start time, newest first
	sort.Slice(runs, func(i, j int) bool {
		return runs[i].StartedAt.After(runs[j].StartedAt)
	})

	data := map[string]interface{}{
		"Title": "Runs",
		"Runs":  runs,
	}

	if err := s.engine.Render(w, "templates/pages/index.html", data); err != nil {
		s.logger.Error("failed to render template", "template", "index", "error", err)
		httputil.WriteError(w, http.StatusInternalServerError, "Internal Server Error")
	}
}

func (s *UIServer) HandleRun(w http.ResponseWriter, r *http.Request) {
	runID := strings.TrimPrefix(r.URL.Path, "/ui/run/")
	if runID == "" {
		http.NotFound(w, r)
		return
	}

	info, ok := s.core.GetRunInfo(runID)
	if !ok {
		http.NotFound(w, r)
		return
	}

	data := map[string]interface{}{
		"Title": fmt.Sprintf("Run %s", info.ID),
		"Run":   info,
	}

	if err := s.engine.Render(w, "templates/pages/run.html", data); err != nil {
		s.logger.Error("failed to render template", "template", "run", "error", err)
		httputil.WriteError(w, http.StatusInternalServerError, "Internal Server Error")
	}
}

func (s *UIServer) HandleLogs(w http.ResponseWriter, r *http.Request) {
	runID := strings.TrimPrefix(r.URL.Path, "/ui/logs/")
	if runID == "" {
		httputil.WriteErrorMessage(w, http.StatusBadRequest, "Missing run ID")
		return
	}

	// Subscribe to new logs first to avoid gaps
	logCh := s.core.SubscribeLogs(runID)
	defer s.core.UnsubscribeLogs(runID, logCh)

	// Check if run exists and get initial history
	info, ok := s.core.GetRunInfo(runID)
	if !ok {
		http.NotFound(w, r)
		return
	}

	sseutil.SetHeaders(w)

	// Get existing logs first (GetRunInfo returns a safe copy)
	for _, log := range info.Logs {
		sseutil.WriteEvent(w, map[string]interface{}{
			"timestamp": log.Timestamp.Format(time.RFC3339),
			"stream":    log.Stream,
			"data":      log.Data,
		})
	}

	sseutil.Flush(w)

	// Stream new logs
	for {
		select {
		case log, ok := <-logCh:
			if !ok {
				return
			}
			sseutil.WriteEvent(w, map[string]interface{}{
				"timestamp": log.Timestamp.Format(time.RFC3339),
				"stream":    log.Stream,
				"data":      log.Data,
			})
			sseutil.Flush(w)
		case <-r.Context().Done():
			return
		}
	}
}

func (s *UIServer) HandleStatusStream(w http.ResponseWriter, r *http.Request) {
	sseutil.SetHeaders(w)

	// Subscribe to status changes
	statusCh := s.core.SubscribeStatus()
	defer s.core.UnsubscribeStatus(statusCh)

	// Stream status updates
	for {
		select {
		case status, ok := <-statusCh:
			if !ok {
				return
			}

			data := map[string]interface{}{
				"id":          status.ID,
				"status":      status.Status,
				"started_at":  status.StartedAt.Format(time.RFC3339),
				"finished_at": nil,
				"exit_code":   nil,
			}

			if status.FinishedAt != nil {
				data["finished_at"] = status.FinishedAt.Format(time.RFC3339)
			}
			if status.ExitCode != nil {
				data["exit_code"] = *status.ExitCode
			}

			sseutil.WriteEvent(w, data)
			sseutil.Flush(w)
		case <-r.Context().Done():
			return
		}
	}
}
