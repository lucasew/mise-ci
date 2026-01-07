package server

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"

	"github.com/lucasew/mise-ci/internal/core"
)

var (
	instanceID     string
	instanceIDOnce sync.Once
)

func getInstanceID() string {
	instanceIDOnce.Do(func() {
		b := make([]byte, 16)
		if _, err := rand.Read(b); err != nil {
			panic("failed to generate instance ID: " + err.Error())
		}
		instanceID = hex.EncodeToString(b)
	})
	return instanceID
}

type HttpServer struct {
	addr           string
	service        *core.Service
	wsServer       *WebSocketServer
	uiServer       *UIServer
	authMiddleware *AuthMiddleware
	logger         *slog.Logger
}

func NewHttpServer(addr string, service *core.Service, wsServer *WebSocketServer, uiServer *UIServer, authMiddleware *AuthMiddleware, logger *slog.Logger) *HttpServer {
	return &HttpServer{
		addr:           addr,
		service:        service,
		wsServer:       wsServer,
		uiServer:       uiServer,
		authMiddleware: authMiddleware,
		logger:         logger,
	}
}

func (s *HttpServer) Serve(l net.Listener) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/validate", s.handleValidate)
	mux.HandleFunc("/ws", s.wsServer.HandleConnect)
	mux.HandleFunc("/webhook", s.service.HandleWebhook)
	mux.HandleFunc("/ui/test/dispatch", s.authMiddleware.RequireBasicAuth(s.service.HandleTestDispatch))

	// UI routes
	mux.HandleFunc("/ui/", s.authMiddleware.RequireBasicAuth(s.uiServer.HandleIndex))
	// Use a dispatcher for /ui/run/{run_id} to handle both the page and the raw log file
	// This avoids Go 1.22+ routing issues with wildcards containing dots
	mux.HandleFunc("/ui/run/{run_id}", s.authMiddleware.RequireRunToken(func(w http.ResponseWriter, r *http.Request) {
		runID := r.PathValue("run_id")
		if strings.HasSuffix(runID, ".log") {
			s.uiServer.HandleRunLogsText(w, r)
		} else {
			s.uiServer.HandleRun(w, r)
		}
	}))
	mux.HandleFunc("/ui/logs/", s.authMiddleware.RequireRunToken(s.uiServer.HandleLogs))
	mux.HandleFunc("/ui/status-stream", s.authMiddleware.RequireStatusStreamAuth(s.uiServer.HandleStatusStream))
	mux.HandleFunc("/ui/issues", s.authMiddleware.RequireBasicAuth(s.uiServer.HandleRepoIssues))

	// Admin routes
	mux.HandleFunc("/ui/admin/cleanup", s.authMiddleware.RequireBasicAuth(s.uiServer.HandleAdminCleanup))
	mux.HandleFunc("/ui/admin/cleanup/repo-urls", s.authMiddleware.RequireBasicAuth(s.uiServer.HandleBackfillRepoURLs))
	mux.HandleFunc("/ui/admin/cleanup/stuck-runs", s.authMiddleware.RequireBasicAuth(s.uiServer.HandleCleanupStuckRuns))

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/ui/", http.StatusFound)
		} else {
			http.NotFound(w, r)
		}
	})

	return http.Serve(l, mux)
}

func (s *HttpServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte("OK")); err != nil {
		s.logger.Error("failed to write health response", "error", err)
	}
}

func (s *HttpServer) handleValidate(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte("mise-ci-agent:" + getInstanceID())); err != nil {
		s.logger.Error("failed to write validate response", "error", err)
	}
}
