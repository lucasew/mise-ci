package main

import (
	"fmt"
	"net"
	"os"

	"github.com/soheilhy/cmux"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"google.golang.org/grpc"

	"mise-ci/internal/config"
	"mise-ci/internal/core"
	"mise-ci/internal/forge"
	"mise-ci/internal/forge/github"
	pb "mise-ci/internal/proto"
	"mise-ci/internal/runner/nomad"
	"mise-ci/internal/server"
)

var agentCmd = &cobra.Command{
	Use:   "agent",
	Short: "Runs the server",
	RunE:  runAgent,
}

func init() {
	rootCmd.AddCommand(agentCmd)

	agentCmd.Flags().String("server.http_addr", ":8080", "HTTP listen address")
	_ = viper.BindPFlag("server.http_addr", agentCmd.Flags().Lookup("server.http_addr"))
}

func runAgent(cmd *cobra.Command, args []string) error {
	var cfg config.Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return err
	}

	if cfg.JWT.Secret == "" {
		return fmt.Errorf("jwt.secret is required (MISE_CI_JWT_SECRET)")
	}

	// Forges
	var forges []forge.Forge

	// GitHub
	if cfg.GitHub.AppID != 0 {
		key, err := os.ReadFile(cfg.GitHub.PrivateKey)
		if err != nil {
			return fmt.Errorf("failed to read github private key: %w", err)
		}
		f := github.NewGitHubForge(cfg.GitHub.AppID, key, cfg.GitHub.WebhookSecret)
		forges = append(forges, f)
		logger.Info("github forge enabled", "app_id", cfg.GitHub.AppID)
	}

	// Runner
	var r *nomad.NomadRunner
	var err error

	// Nomad
	if cfg.Nomad.JobName != "" {
		nomadAddr := cfg.Nomad.Addr
		if nomadAddr == "" {
			nomadAddr = os.Getenv("NOMAD_ADDR")
		}
		r, err = nomad.NewNomadRunner(nomadAddr, cfg.Nomad.JobName)
		if err != nil {
			return fmt.Errorf("failed to create nomad runner: %w", err)
		}
		logger.Info("nomad runner enabled", "addr", nomadAddr, "job", cfg.Nomad.JobName)
	} else {
		return fmt.Errorf("no runner configured (nomad.job_name missing)")
	}

	core := core.NewCore(logger, cfg.JWT.Secret)
	svc := core.NewService(core, forges, r, &cfg, logger)

	grpcSrv := server.NewGrpcServer(core, logger)
	httpSrv := server.NewHttpServer(cfg.Server.HTTPAddr, svc, logger)

	// Multiplex
	lis, err := net.Listen("tcp", cfg.Server.HTTPAddr)
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}

	m := cmux.New(lis)
	grpcL := m.Match(cmux.HTTP2HeaderField("content-type", "application/grpc"))
	httpL := m.Match(cmux.Any())

	s := grpc.NewServer()
	pb.RegisterServerServer(s, grpcSrv)

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
		return fmt.Errorf("cmux serve error: %w", err)
	}
	return nil
}
