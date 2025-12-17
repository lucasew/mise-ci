package server

import (
	"log/slog"
	"net"
	"net/http"

	"mise-ci/internal/core"
)

type HttpServer struct {
	addr    string
	service *core.Service
	logger  *slog.Logger
}

func NewHttpServer(addr string, service *core.Service, logger *slog.Logger) *HttpServer {
	return &HttpServer{
		addr:    addr,
		service: service,
		logger:  logger,
	}
}

func (s *HttpServer) Serve(l net.Listener) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/webhook", s.service.HandleWebhook)

	return http.Serve(l, mux)
}
