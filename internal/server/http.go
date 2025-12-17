package server

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net"
	"net/http"
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
		rand.Read(b)
		instanceID = hex.EncodeToString(b)
	})
	return instanceID
}

type HttpServer struct {
	addr      string
	service   *core.Service
	wsServer  *WebSocketServer
	uiServer  *UIServer
	logger    *slog.Logger
}

func NewHttpServer(addr string, service *core.Service, wsServer *WebSocketServer, uiServer *UIServer, logger *slog.Logger) *HttpServer {
	return &HttpServer{
		addr:     addr,
		service:  service,
		wsServer: wsServer,
		uiServer: uiServer,
		logger:   logger,
	}
}

func (s *HttpServer) Serve(l net.Listener) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/validate", s.handleValidate)
	mux.HandleFunc("/ws", s.wsServer.HandleConnect)
	mux.HandleFunc("/webhook", s.service.HandleWebhook)
	mux.HandleFunc("/test/dispatch", s.service.HandleTestDispatch)

	// UI routes
	mux.HandleFunc("/ui/", s.uiServer.HandleIndex)
	mux.HandleFunc("/ui/run/", s.uiServer.HandleRun)
	mux.HandleFunc("/ui/logs/", s.uiServer.HandleLogs)
	mux.HandleFunc("/ui/status-stream", s.uiServer.HandleStatusStream)
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
	w.Write([]byte("OK"))
}

func (s *HttpServer) handleValidate(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("mise-ci-agent:" + getInstanceID()))
}
