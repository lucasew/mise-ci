package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/lucasew/mise-ci/internal/artifacts"
	"github.com/lucasew/mise-ci/internal/config"
	"github.com/lucasew/mise-ci/internal/core"
	"github.com/lucasew/mise-ci/internal/forge"
	"github.com/lucasew/mise-ci/internal/forge/github"
	"github.com/lucasew/mise-ci/internal/netutil"
	"github.com/lucasew/mise-ci/internal/repository"
	"github.com/lucasew/mise-ci/internal/repository/postgres"
	"github.com/lucasew/mise-ci/internal/repository/sqlite"
	"github.com/lucasew/mise-ci/internal/server"
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
	// Triggering CI build to ensure lint fixes are picked up
	var cfg config.Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return err
	}

	if cfg.JWT.Secret == "" {
		return fmt.Errorf("jwt.secret is required (MISE_CI_JWT_SECRET)")
	}

	// Warn if PublicURL not set
	if cfg.Server.PublicURL == "" {
		logger.Warn("public URL not configured - GitHub status links may not work",
			"fallback", cfg.Server.HTTPAddr,
			"env", "MISE_CI_SERVER_PUBLIC_URL")
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

	// Storage & Database
	dataDir := cfg.Storage.DataDir
	if dataDir == "" {
		dataDir = "./data"
	}

	var repo repository.Repository
	var err error
	if cfg.Database.Driver == "postgres" {
		if cfg.Database.DSN == "" {
			return fmt.Errorf("postgres driver requires 'dsn' (MISE_CI_DATABASE_DSN) to be set")
		}
		repo, err = postgres.NewRepository(cfg.Database.DSN)
		if err != nil {
			return fmt.Errorf("failed to create postgres repository: %w", err)
		}
		logger.Info("repository initialized (postgres)")
	} else {
		// Default to SQLite
		dbPath := cfg.Database.DSN
		if dbPath == "" {
			dbPath = filepath.Join(dataDir, "runs.db")
		}
		repo, err = sqlite.NewRepository(dbPath)
		if err != nil {
			return fmt.Errorf("failed to create sqlite repository: %w", err)
		}
		logger.Info("repository initialized (sqlite)", "path", dbPath)
	}
	defer func() {
		if err := repo.Close(); err != nil {
			logger.Error("failed to close repository", "error", err)
		}
	}()

	artifactStorage := artifacts.NewLocalStorage(filepath.Join(dataDir, "artifacts"))
	logger.Info("artifact storage initialized", "path", filepath.Join(dataDir, "artifacts"))

	appCore := core.NewCore(logger, cfg.JWT.Secret, repo)
	// Inject the first forge if available for maintenance tasks
	if len(forges) > 0 {
		appCore.SetForge(forges[0])
	}

	svc := core.NewService(appCore, forges, artifactStorage, &cfg, logger)

	authMiddleware := server.NewAuthMiddleware(appCore, &cfg.Auth)

	wsSrv := server.NewWebSocketServer(appCore, logger)
	uiSrv := server.NewUIServer(appCore, logger, cfg.Server.PublicURL)
	httpSrv := server.NewHttpServer(cfg.Server.HTTPAddr, svc, wsSrv, uiSrv, authMiddleware, logger)

	// Simple HTTP listener (no multiplexing needed!)
	lis, err := net.Listen("tcp", cfg.Server.HTTPAddr)
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}

	go func() {
		if err := httpSrv.Serve(lis); err != nil {
			logger.Error("http serve error", "error", err)
		}
	}()

	// Configuration summary
	logger.Info("=== mise-ci agent configuration ===")
	logger.Info("server", "http_addr", cfg.Server.HTTPAddr, "public_url", cfg.Server.PublicURL)

	if len(forges) > 0 {
		logger.Info("forges configured", "count", len(forges))
		if cfg.GitHub.AppID != 0 {
			logger.Info("  - github", "app_id", cfg.GitHub.AppID, "webhook_secret_set", cfg.GitHub.WebhookSecret != "")
		}
	} else {
		logger.Info("forges", "status", "none configured")
	}

	logger.Info("runner", "type", "pull", "status", "workers connect to the server")
	logger.Info("jwt", "secret_set", cfg.JWT.Secret != "")

	authConfigured := cfg.Auth.AdminUsername != "" && cfg.Auth.AdminPassword != ""
	if !authConfigured {
		logger.Warn("admin credentials not configured - /ui/ endpoint will be unprotected")
		logger.Warn("set MISE_CI_AUTH_ADMIN_USERNAME and MISE_CI_AUTH_ADMIN_PASSWORD")
	}

	logger.Info("authentication",
		"admin_auth_enabled", authConfigured,
		"ui_tokens_enabled", true)

	logger.Info("websocket", "endpoint", "/ws")
	logger.Info("webui", "url", fmt.Sprintf("http://%s/ui/", cfg.Server.HTTPAddr))
	logger.Info("=================================")
	logger.Info("agent listening", "addr", cfg.Server.HTTPAddr)

	// Validate public URL accessibility in background
	go validatePublicURL(&cfg, logger)

	// Block forever (server runs in goroutine)
	select {}
}

func validatePublicURL(cfg *config.Config, logger *slog.Logger) {
	// Wait a moment for the server to fully start
	time.Sleep(500 * time.Millisecond)

	// Check if PublicURL is set
	if cfg.Server.PublicURL == "" {
		logger.Warn("public_url is not configured - workers may not be able to connect back to the agent")
		logger.Warn("set MISE_CI_SERVER_PUBLIC_URL to the externally accessible URL of this agent")
		return
	}

	// Build validate URL
	publicURL := cfg.Server.PublicURL
	if !strings.HasPrefix(publicURL, "http://") && !strings.HasPrefix(publicURL, "https://") {
		publicURL = "http://" + publicURL
	}
	validateURL := strings.TrimSuffix(publicURL, "/") + "/validate"

	logger.Info("validating public URL accessibility", "url", validateURL)

	// Try to access validate endpoint
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", validateURL, nil)
	if err != nil {
		logger.Error("failed to create validation request", "error", err)
		return
	}

	// Use a client with a safe dialer to prevent SSRF
	safeDialer := &netutil.SafeDialer{
		Timeout: 5 * time.Second,
	}
	safeClient := &http.Client{
		Transport: &http.Transport{
			DialContext: safeDialer.DialContext,
		},
		Timeout: 10 * time.Second,
	}

	resp, err := safeClient.Do(req)
	if err != nil {
		logger.Error("❌ public URL is NOT accessible from this node", "url", validateURL, "error", err)
		logger.Error("workers will NOT be able to connect back to the agent")
		logger.Error("check your network configuration, firewall rules, and public_url setting")
		return
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			logger.Error("failed to close response body", "error", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		logger.Error("public URL returned unexpected status", "url", validateURL, "status", resp.StatusCode)
		return
	}

	// Read and verify response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.Error("failed to read validation response", "error", err)
		return
	}

	response := string(body)
	if !strings.HasPrefix(response, "mise-ci-agent:") {
		logger.Error("validation response does not match expected format", "response", response)
		logger.Error("you may be reaching a different service or a proxy")
		return
	}

	logger.Info("✅ public URL validated successfully - service is accessible from outside", "url", publicURL)
}
