package server

import (
	"log/slog"
	"net/http"

	"mise-ci/internal/matriz"
)

type HttpServer struct {
	addr    string
	service *matriz.Service
	logger  *slog.Logger
}

func NewHttpServer(addr string, service *matriz.Service, logger *slog.Logger) *HttpServer {
	return &HttpServer{
		addr:    addr,
		service: service,
		logger:  logger,
	}
}

func (s *HttpServer) Run() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/webhook", s.service.HandleWebhook)

	s.logger.Info("http server listening", "addr", s.addr)
	return http.ListenAndServe(s.addr, mux)
}
