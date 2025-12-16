package main

import (
	"log/slog"
	"net"
	"os"

	"google.golang.org/grpc"
	"mise-ci/internal/config"
	"mise-ci/internal/forge/github"
	"mise-ci/internal/matriz"
	"mise-ci/internal/runner/nomad"
	"mise-ci/internal/server"
	pb "mise-ci/internal/proto"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	if len(os.Args) < 2 {
		logger.Error("usage: matriz <config.yaml>")
		os.Exit(1)
	}

	cfg, err := config.Load(os.Args[1])
	if err != nil {
		logger.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	// Components
	var f *github.GitHubForge
	if cfg.Forge.Type == "github" {
		key, err := os.ReadFile(cfg.Forge.PrivateKey)
		if err != nil {
			logger.Error("failed to read private key", "error", err)
			os.Exit(1)
		}
		f = github.NewGitHubForge(cfg.Forge.AppID, key, cfg.Forge.WebhookSecret)
	} else {
		logger.Error("unknown forge type", "type", cfg.Forge.Type)
		os.Exit(1)
	}

	var r *nomad.NomadRunner
	if cfg.Runner.Type == "nomad" {
		r, err = nomad.NewNomadRunner(cfg.Runner.Addr, cfg.Runner.JobTemplate)
		if err != nil {
			logger.Error("failed to create runner", "error", err)
			os.Exit(1)
		}
	} else {
		logger.Error("unknown runner type", "type", cfg.Runner.Type)
		os.Exit(1)
	}

	core := matriz.NewCore(logger, cfg.JWT.Secret)
	svc := matriz.NewService(core, f, r, cfg, logger)

	grpcSrv := server.NewGrpcServer(core, logger)
	httpSrv := server.NewHttpServer(cfg.Server.HTTPAddr, svc, logger)

	// Start servers
	go func() {
		lis, err := net.Listen("tcp", cfg.Server.GRPCAddr)
		if err != nil {
			logger.Error("failed to listen grpc", "error", err)
			os.Exit(1)
		}
		s := grpc.NewServer()
		pb.RegisterMatrizServer(s, grpcSrv)
		logger.Info("grpc server listening", "addr", cfg.Server.GRPCAddr)
		if err := s.Serve(lis); err != nil {
			logger.Error("grpc serve error", "error", err)
			os.Exit(1)
		}
	}()

	if err := httpSrv.Run(); err != nil {
		logger.Error("http serve error", "error", err)
		os.Exit(1)
	}
}
