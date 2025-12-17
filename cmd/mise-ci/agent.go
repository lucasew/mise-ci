package main

import (
	"crypto/rand"
	"encoding/hex"
	"net"
	"os"

	"github.com/soheilhy/cmux"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"google.golang.org/grpc"

	"mise-ci/internal/config"
	"mise-ci/internal/forge/github"
	"mise-ci/internal/matriz"
	pb "mise-ci/internal/proto"
	"mise-ci/internal/runner/nomad"
	"mise-ci/internal/server"
)

var agentCmd = &cobra.Command{
	Use:   "agent",
	Short: "Runs the Matriz server",
	Run:   runAgent,
}

func init() {
	rootCmd.AddCommand(agentCmd)

	agentCmd.Flags().String("server.http_addr", ":8080", "HTTP listen address")
	_ = viper.BindPFlag("server.http_addr", agentCmd.Flags().Lookup("server.http_addr"))
}

func runAgent(cmd *cobra.Command, args []string) {
	var cfg config.Config
	if err := viper.Unmarshal(&cfg); err != nil {
		logger.Error("failed to unmarshal config", "error", err)
		os.Exit(1)
	}

	if cfg.JWT.Secret == "" {
		b := make([]byte, 32)
		_, _ = rand.Read(b)
		cfg.JWT.Secret = hex.EncodeToString(b)
		logger.Info("generated random jwt secret")
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
	var err error
	if cfg.Runner.Type == "nomad" {
		r, err = nomad.NewNomadRunner(cfg.Runner.Addr, cfg.Runner.JobName)
		if err != nil {
			logger.Error("failed to create runner", "error", err)
			os.Exit(1)
		}
	} else {
		logger.Error("unknown runner type", "type", cfg.Runner.Type)
		os.Exit(1)
	}

	core := matriz.NewCore(logger, cfg.JWT.Secret)
	svc := matriz.NewService(core, f, r, &cfg, logger)

	grpcSrv := server.NewGrpcServer(core, logger)
	httpSrv := server.NewHttpServer(cfg.Server.HTTPAddr, svc, logger)

	// Multiplex
	lis, err := net.Listen("tcp", cfg.Server.HTTPAddr)
	if err != nil {
		logger.Error("failed to listen", "error", err)
		os.Exit(1)
	}

	m := cmux.New(lis)
	grpcL := m.Match(cmux.HTTP2HeaderField("content-type", "application/grpc"))
	httpL := m.Match(cmux.Any())

	s := grpc.NewServer()
	pb.RegisterMatrizServer(s, grpcSrv)

	go func() {
		if err := s.Serve(grpcL); err != nil {
			logger.Error("grpc serve error", "error", err)
		}
	}()

	go func() {
		if err := httpSrv.Serve(httpL); err != nil {
			logger.Error("http serve error", "error", err)
		}
	}()

	logger.Info("agent listening", "addr", cfg.Server.HTTPAddr)
	if err := m.Serve(); err != nil {
		logger.Error("cmux serve error", "error", err)
	}
}
