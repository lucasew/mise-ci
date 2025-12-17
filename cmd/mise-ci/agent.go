package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
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
			return fmt.Errorf("failed to read private key: %w", err)
		}
		f = github.NewGitHubForge(cfg.Forge.AppID, key, cfg.Forge.WebhookSecret)
	} else {
		return fmt.Errorf("unknown forge type: %s", cfg.Forge.Type)
	}

	var r *nomad.NomadRunner
	var err error
	if cfg.Runner.Type == "nomad" {
		r, err = nomad.NewNomadRunner(cfg.Runner.Addr, cfg.Runner.JobName)
		if err != nil {
			return fmt.Errorf("failed to create runner: %w", err)
		}
	} else {
		return fmt.Errorf("unknown runner type: %s", cfg.Runner.Type)
	}

	core := matriz.NewCore(logger, cfg.JWT.Secret)
	svc := matriz.NewService(core, f, r, &cfg, logger)

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
		return fmt.Errorf("cmux serve error: %w", err)
	}
	return nil
}
