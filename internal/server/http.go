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
	logger    *slog.Logger
}

func NewHttpServer(addr string, service *core.Service, wsServer *WebSocketServer, logger *slog.Logger) *HttpServer {
	return &HttpServer{
		addr:     addr,
		service:  service,
		wsServer: wsServer,
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
