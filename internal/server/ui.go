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

	"github.com/lucasew/mise-ci/internal/core"
)

//go:embed templates/*
var templatesFS embed.FS

type UIServer struct {
	core      *core.Core
	logger    *slog.Logger
	templates *template.Template
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

	tmpl := template.Must(template.New("").Funcs(funcMap).ParseFS(templatesFS, "templates/*.html"))

	return &UIServer{
		core:      c,
		logger:    logger,
		templates: tmpl,
	}
}

func (s *UIServer) HandleIndex(w http.ResponseWriter, r *http.Request) {
	runs := s.core.GetAllRuns()

	// Sort by start time, newest first
	sort.Slice(runs, func(i, j int) bool {
		return runs[i].StartedAt.After(runs[j].StartedAt)
	})

	data := map[string]interface{}{
		"Runs": runs,
	}

	if err := s.templates.ExecuteTemplate(w, "index.html", data); err != nil {
		s.logger.Error("failed to render template", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
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
		"Run": info,
	}

	if err := s.templates.ExecuteTemplate(w, "run.html", data); err != nil {
		s.logger.Error("failed to render template", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func (s *UIServer) HandleLogs(w http.ResponseWriter, r *http.Request) {
	runID := strings.TrimPrefix(r.URL.Path, "/ui/logs/")
	if runID == "" {
		http.Error(w, "Missing run ID", http.StatusBadRequest)
		return
	}

	// Check if run exists
	_, ok := s.core.GetRunInfo(runID)
	if !ok {
		http.NotFound(w, r)
		return
	}

	// Set headers for SSE
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Get existing logs first
	info, _ := s.core.GetRunInfo(runID)
	for _, log := range info.Logs {
		fmt.Fprintf(w, "data: {\"timestamp\":\"%s\",\"stream\":\"%s\",\"data\":%q}\n\n",
			log.Timestamp.Format(time.RFC3339),
			log.Stream,
			log.Data)
	}

	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	// Subscribe to new logs
	logCh := s.core.SubscribeLogs(runID)
	defer s.core.UnsubscribeLogs(runID, logCh)

	// Stream new logs
	for {
		select {
		case log, ok := <-logCh:
			if !ok {
				return
			}
			fmt.Fprintf(w, "data: {\"timestamp\":\"%s\",\"stream\":\"%s\",\"data\":%q}\n\n",
				log.Timestamp.Format(time.RFC3339),
				log.Stream,
				log.Data)
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		case <-r.Context().Done():
			return
		}
	}
}

func (s *UIServer) HandleStatusStream(w http.ResponseWriter, r *http.Request) {
	// Set headers for SSE
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

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

			finishedAt := "null"
			if status.FinishedAt != nil {
				finishedAt = fmt.Sprintf("\"%s\"", status.FinishedAt.Format(time.RFC3339))
			}

			exitCode := "null"
			if status.ExitCode != nil {
				exitCode = fmt.Sprintf("%d", *status.ExitCode)
			}

			fmt.Fprintf(w, "data: {\"id\":\"%s\",\"status\":\"%s\",\"started_at\":\"%s\",\"finished_at\":%s,\"exit_code\":%s}\n\n",
				status.ID,
				status.Status,
				status.StartedAt.Format(time.RFC3339),
				finishedAt,
				exitCode)

			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		case <-r.Context().Done():
			return
		}
	}
}
