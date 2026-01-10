package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/lucasew/mise-ci/internal/version"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var dispatcherCmd = &cobra.Command{
	Use:   "dispatcher",
	Short: "Runs the dispatcher (polls for jobs and runs them in Docker containers)",
	Long: `The dispatcher polls the server for available jobs and executes each one
in a separate Docker container. It runs sequentially (one job at a time) until
the queue is empty, then exits.

This is an alternative to Nomad for running workers - useful for running on
a single machine without orchestration infrastructure.

Uses the same environment variables as worker:
  MISE_CI_CALLBACK - Server URL
  MISE_CI_TOKEN - Pool worker token`,
	RunE: runDispatcher,
}

var (
	DefaultImage = fmt.Sprintf("ghcr.io/lucasew/mise-ci:%s", version.Get())
)

func init() {
	rootCmd.AddCommand(dispatcherCmd)

	dispatcherCmd.Flags().String("callback", "", "Server URL (e.g., https://mise-ci.example.com)")
	dispatcherCmd.Flags().String("token", "", "Pool worker token")
	dispatcherCmd.Flags().String("image", DefaultImage, "Docker image to use for workers")
	dispatcherCmd.Flags().Int("max-empty-polls", 3, "Exit after N consecutive empty polls")
	dispatcherCmd.Flags().Duration("poll-interval", 5*time.Second, "Interval between polls when queue is empty")

	// Usar as mesmas env vars do worker para padronização
	_ = viper.BindPFlag("callback", dispatcherCmd.Flags().Lookup("callback"))
	_ = viper.BindPFlag("token", dispatcherCmd.Flags().Lookup("token"))
	_ = viper.BindPFlag("dispatcher.image", dispatcherCmd.Flags().Lookup("image"))
	_ = viper.BindPFlag("dispatcher.max_empty_polls", dispatcherCmd.Flags().Lookup("max-empty-polls"))
	_ = viper.BindPFlag("dispatcher.poll_interval", dispatcherCmd.Flags().Lookup("poll-interval"))
}

type PollResponse struct {
	RunID       string `json:"run_id"`
	WorkerToken string `json:"worker_token"`
}

func runDispatcher(cmd *cobra.Command, args []string) error {
	// Seed random para jitter (evita sequências idênticas em dispatchers simultâneos)
	rand.Seed(time.Now().UnixNano())

	// Usar as mesmas env vars do worker: MISE_CI_CALLBACK e MISE_CI_TOKEN
	serverURL := viper.GetString("callback")
	token := viper.GetString("token")
	image := viper.GetString("dispatcher.image")
	maxEmptyPolls := viper.GetInt("dispatcher.max_empty_polls")
	pollInterval := viper.GetDuration("dispatcher.poll_interval")

	if serverURL == "" || token == "" {
		return fmt.Errorf("callback and token are required (--callback/--token or MISE_CI_CALLBACK/MISE_CI_TOKEN)")
	}

	logger.Info("dispatcher starting",
		"server", serverURL,
		"image", image,
		"max_empty_polls", maxEmptyPolls,
		"poll_interval", pollInterval)

	// Verificar se Docker está disponível
	if err := checkDocker(); err != nil {
		return fmt.Errorf("docker check failed: %w", err)
	}

	// Build poll URL
	pollURL := buildPollURL(serverURL)
	logger.Info("polling endpoint", "url", pollURL)

	emptyPolls := 0
	totalJobs := 0

	for {
		// Poll para próxima run
		run, err := pollForRun(pollURL, token)
		if err != nil {
			logger.Error("poll failed", "error", err)
			time.Sleep(addJitter(pollInterval))
			continue
		}

		if run == nil {
			// Sem jobs disponíveis
			emptyPolls++
			logger.Info("no jobs available", "empty_polls", emptyPolls, "max", maxEmptyPolls)

			if emptyPolls >= maxEmptyPolls {
				logger.Info("max empty polls reached, exiting", "total_jobs_processed", totalJobs)
				return nil
			}

			time.Sleep(addJitter(pollInterval))
			continue
		}

		// Reset contador de polls vazios
		emptyPolls = 0
		totalJobs++

		logger.Info("job acquired", "run_id", run.RunID, "total_jobs", totalJobs)

		// Executar worker em Docker
		if err := runWorkerContainer(serverURL, run, image); err != nil {
			logger.Error("worker container failed", "run_id", run.RunID, "error", err)
			// Continuar para próximo job
			continue
		}

		logger.Info("job completed", "run_id", run.RunID)
	}
}

// addJitter adds ±25% random variation to a duration to prevent thundering herd
func addJitter(d time.Duration) time.Duration {
	// Random factor between 0.75 and 1.25 (±25%)
	jitter := 1.0 + (rand.Float64()-0.5)*0.5
	return time.Duration(float64(d) * jitter)
}

func checkDocker() error {
	cmd := exec.Command("docker", "version")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker is not available or not running: %w", err)
	}
	return nil
}

func buildPollURL(serverURL string) string {
	// Normalizar URL
	u := serverURL
	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		u = "https://" + u
	}
	u = strings.TrimSuffix(u, "/")
	return u + "/api/dispatcher/poll"
}

func pollForRun(pollURL, token string) (*PollResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", pollURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNoContent {
		// Sem jobs disponíveis
		return nil, nil
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}

	var run PollResponse
	if err := json.NewDecoder(resp.Body).Decode(&run); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &run, nil
}

func runWorkerContainer(serverURL string, run *PollResponse, image string) error {
	logger.Info("starting worker container", "run_id", run.RunID, "image", image)

	// Preparar variáveis de ambiente
	env := []string{
		fmt.Sprintf("MISE_CI_CALLBACK=%s", serverURL),
		fmt.Sprintf("MISE_CI_TOKEN=%s", run.WorkerToken),
	}

	// Construir comando Docker
	args := []string{
		"run",
		"--rm",              // Remove container ao finalizar
		"--network", "host", // Usar network do host para acesso ao servidor
	}

	// Adicionar env vars
	for _, e := range env {
		args = append(args, "-e", e)
	}

	// Imagem e comando
	args = append(args, image, "worker")

	logger.Info("executing", "cmd", "docker "+strings.Join(args, " "))

	cmd := exec.Command("docker", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker run failed: %w", err)
	}

	return nil
}

func buildCallbackURL(serverURL string) string {
	// Parse URL para garantir formato correto
	u, err := url.Parse(serverURL)
	if err != nil {
		// Fallback: retornar como está
		return serverURL
	}

	// Garantir que não tem trailing slash
	u.Path = strings.TrimSuffix(u.Path, "/")

	return u.String()
}
