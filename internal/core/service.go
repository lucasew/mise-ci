package core

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"time"

	"github.com/lucasew/mise-ci/internal/artifacts"
	"github.com/lucasew/mise-ci/internal/config"
	"github.com/lucasew/mise-ci/internal/forge"
	"github.com/lucasew/mise-ci/internal/httputil"
	"github.com/lucasew/mise-ci/internal/msgutil"
	"github.com/lucasew/mise-ci/internal/orchestration"
	pb "github.com/lucasew/mise-ci/internal/proto"
	"github.com/lucasew/mise-ci/internal/repository"
	"github.com/lucasew/mise-ci/internal/runner"
	"github.com/lucasew/mise-ci/internal/sanitize"
	"strings"
)

//go:embed testdata/mise.toml
var testProjectMiseToml string

type Service struct {
	Core            *Core
	Forges          []forge.Forge
	Runner          runner.Runner
	ArtifactStorage artifacts.Storage
	Config          *config.Config
	Logger          *slog.Logger
}

func NewService(core *Core, forges []forge.Forge, r runner.Runner, artifactStorage artifacts.Storage, cfg *config.Config, logger *slog.Logger) *Service {
	return &Service{
		Core:            core,
		Forges:          forges,
		Runner:          r,
		ArtifactStorage: artifactStorage,
		Config:          cfg,
		Logger:          logger,
	}
}

func (s *Service) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	for _, f := range s.Forges {
		event, err := f.ParseWebhook(r)
		if err != nil {
			s.Logger.Debug("forge parse webhook error", "error", err)
			continue
		}
		if event != nil {
			s.Logger.Info("received webhook", "repo", event.Repo, "sha", event.SHA)
			go s.StartRun(event, f)
			httputil.WriteText(w, http.StatusAccepted, "accepted")
			return
		}
	}

	httputil.WriteText(w, http.StatusOK, "ignored")
}

func (s *Service) HandleTestDispatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httputil.WriteErrorMessage(w, http.StatusMethodNotAllowed, "only POST allowed")
		return
	}

	ctx := context.Background()
	runID := fmt.Sprintf("test-%d", time.Now().Unix())

	s.Logger.Info("test dispatch", "run_id", runID)

	run := s.Core.CreateRun(runID, "", "", "Test Dispatch", "admin", "test")
	// Generate a run-specific token for the test dispatch
	token, err := s.Core.GenerateWorkerToken(runID)
	if err != nil {
		s.Logger.Error("generate worker token", "error", err)
		httputil.WriteError(w, http.StatusInternalServerError, "error: %v", err)
		return
	}

	publicURL := s.Config.Server.PublicURL
	if publicURL == "" {
		publicURL = s.Config.Server.HTTPAddr
	}

	// Prepare initial environment
	env := map[string]string{
		"CI":      "true",
		"MISE_CI": "true",
	}
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		env["GITHUB_TOKEN"] = token
	}
	s.Core.SetRunEnv(runID, env)

	callback := publicURL
	params := runner.RunParams{
		CallbackURL: callback,
		Token:       token,
		Image:       s.Config.Nomad.DefaultImage,
	}

	jobID, err := s.Runner.Dispatch(ctx, params)
	if err != nil {
		s.Logger.Error("dispatch job", "error", err)
		httputil.WriteError(w, http.StatusInternalServerError, "error: %v", err)
		return
	}

	s.Logger.Info("test job dispatched", "job_id", jobID, "run_id", runID)
	s.Core.UpdateStatus(runID, StatusDispatched, nil)

	// Run test orchestration in background
	go s.TestOrchestrate(ctx, run, params)

	// Generate UI URL
	// reusing publicURL defined earlier
	uiURL := s.Core.GetRunUIURL(runID, publicURL)

	// Return JSON response with run details
	response := map[string]string{
		"run_id": runID,
		"job_id": jobID,
		"ui_url": uiURL,
		"status": "dispatched",
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(response); err != nil {
		s.Logger.Error("failed to encode response", "error", err)
	}
}

func (s *Service) TestOrchestrate(ctx context.Context, run *Run, dispatchParams runner.RunParams) {
	// Wait for worker to connect
	s.Logger.Info("waiting for worker to connect", "run_id", run.ID)
	if !s.waitForConnection(ctx, run, dispatchParams) {
		return
	}
	s.Logger.Info("worker connected, starting orchestration", "run_id", run.ID)

	defer func() {
		run.CommandCh <- msgutil.NewCloseCommand(9999)
	}()

	// Create temporary test project directory
	testDir, err := os.MkdirTemp("", "mise-ci-test-*")
	if err != nil {
		s.Logger.Error("failed to create temp dir", "error", err)
		s.Core.UpdateStatus(run.ID, StatusError, nil)
		return
	}
	defer func() {
		if err := os.RemoveAll(testDir); err != nil {
			s.Logger.Error("failed to remove test directory", "path", testDir, "error", err)
		}
	}()

	// Write mise.toml
	miseTomlPath := testDir + "/mise.toml"
	if err := os.WriteFile(miseTomlPath, []byte(testProjectMiseToml), 0644); err != nil {
		s.Logger.Error("failed to write mise.toml", "error", err)
		return
	}

	s.Logger.Info("test project created", "dir", testDir)

	miseTomlData, err := os.ReadFile(miseTomlPath)
	if err != nil {
		s.Logger.Error("failed to read mise.toml", "error", err)
		return
	}

	outputPath := testDir + "/output.txt"

	pipeline := orchestration.NewPipeline(run.ID, s.Core, s.Logger)

	// Prepare env for test run
	env := map[string]string{}
	// Inject GITHUB_TOKEN if available in server environment to avoid rate limits
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		env["GITHUB_TOKEN"] = token
	}

	pipeline.
		AddStep("copy-config", "Copying mise.toml to worker", func() error {
			return s.copyFileToWorker(run, "mise.toml", "mise.toml", miseTomlData)
		}).
		AddStep("trust", "Trusting mise configuration", func() error {
			return s.runCommandSync(run, env, "mise", "trust")
		}).
		AddStep("run-ci", "Starting CI task", func() error {
			return s.runCommandSync(run, env, "mise", "run", "ci")
		}).
		AddStep("retrieve", "Retrieving output artifacts", func() error {
			return s.copyFileFromWorker(run, "output.txt", outputPath)
		})

	if err := pipeline.Run(); err != nil {
		s.Core.UpdateStatus(run.ID, StatusFailure, nil)
		return
	}

	// Read and log the output
	s.Core.AddLog(run.ID, "system", "Processing output...")
	output, err := os.ReadFile(outputPath)
	if err != nil {
		s.Logger.Error("failed to read output.txt", "error", err)
		return
	}

	s.Logger.Info("=== Test Output ===")
	s.Logger.Info(string(output))
	s.Logger.Info("===================")
	s.Logger.Info("test completed successfully")

	s.Core.AddLog(run.ID, "system", "All tasks completed successfully!")

	s.Core.UpdateStatus(run.ID, StatusSuccess, nil)
	s.Core.AddLog(run.ID, "system", "Test run completed successfully")
}

func (s *Service) StartRun(event *forge.WebhookEvent, f forge.Forge) {
	ctx := context.Background()
	runID := fmt.Sprintf("%s-%d", event.SHA[:8], time.Now().Unix())

	s.Logger.Info("starting run", "run_id", runID)

	// Ensure Repo exists
	repo, err := s.Core.repo.GetRepo(ctx, event.Clone)
	if err != nil {
		// Assume not found or error, try to create
		newRepo := &repository.Repo{
			CloneURL: event.Clone,
		}
		if err := s.Core.repo.CreateRepo(ctx, newRepo); err != nil {
			s.Logger.Error("failed to create repo", "error", err)
			// Proceeding might fail due to FK constraint if we don't have a valid repo_id?
			// If CreateRepo failed, maybe it exists now (race condition)?
			// Let's try to get it again
			r, errGet := s.Core.repo.GetRepo(ctx, event.Clone)
			if errGet == nil {
				repo = r
			} else {
				// We can't proceed without a repo_id
				s.Logger.Error("cannot link run to repo", "error", err)
				return
			}
		} else {
			repo = newRepo
		}
	}

	// Clean clone URL is already in event.Clone (no credentials)
	run := s.Core.CreateRun(runID, event.Link, repo.CloneURL, event.CommitMessage, event.Author, event.Branch)

	publicURL := s.Config.Server.PublicURL
	if publicURL == "" {
		publicURL = s.Config.Server.HTTPAddr
	}
	targetURL := s.Core.GetRunUIURL(runID, publicURL)

	status := forge.Status{
		State:       forge.StatePending,
		Context:     "mise-ci",
		Description: "Run started",
		TargetURL:   targetURL,
	}
	if err := f.UpdateStatus(ctx, event.Repo, event.SHA, status); err != nil {
		s.Logger.Error("update status pending", "error", err)
	}

	creds, err := f.CloneCredentials(ctx, event.Repo)
	if err != nil {
		s.Logger.Error("get credentials", "error", err)
		s.Core.UpdateStatus(runID, StatusError, nil)
		status.State = forge.StateError
		status.Description = "Failed to get credentials"
		if err := f.UpdateStatus(ctx, event.Repo, event.SHA, status); err != nil {
			s.Logger.Error("update status failed", "error", err)
		}
		return
	}
	s.Logger.Debug("clone credentials obtained successfully")

	// Generate a run-specific token
	token, err := s.Core.GenerateWorkerToken(runID)
	if err != nil {
		s.Logger.Error("generate worker token", "error", err)
		s.Core.UpdateStatus(runID, StatusError, nil)
		return
	}

	// Prepare environment
	env := map[string]string{
		"CI":                 "true",
		"MISE_CI":            "true",
		// Configure git to trust the directory via env vars (cleaner than running git config commands)
		"GIT_CONFIG_COUNT":   "1",
		"GIT_CONFIG_KEY_0":   "safe.directory",
		"GIT_CONFIG_VALUE_0": "*",
	}

	forgeEnv := f.GetCIEnv(event)
	for k, v := range forgeEnv {
		env[k] = v
	}

	if _, isGithub := env["GITHUB_SERVER_URL"]; isGithub && creds.Token != "" {
		env["GITHUB_TOKEN"] = creds.Token
	}

	s.Core.SetRunEnv(runID, env)

	callback := publicURL
	params := runner.RunParams{
		CallbackURL: callback,
		Token:       token,
		Image:       s.Config.Nomad.DefaultImage,
	}

	jobID, err := s.Runner.Dispatch(ctx, params)
	if err != nil {
		s.Logger.Error("dispatch job", "error", err)
		s.Core.UpdateStatus(runID, StatusError, nil)
		status.State = forge.StateError
		status.Description = "Failed to dispatch job"
		if err := f.UpdateStatus(ctx, event.Repo, event.SHA, status); err != nil {
			s.Logger.Error("update status failed", "error", err)
		}
		return
	}

	s.Logger.Info("job dispatched", "job_id", jobID)
	s.Core.UpdateStatus(runID, StatusDispatched, nil)

	success := s.Orchestrate(ctx, run, event, f, creds, params)

	status.State = forge.StateSuccess
	status.Description = "Build passed"
	if !success {
		status.State = forge.StateFailure
		status.Description = "Build failed"
	} else {
		// Check if it was skipped (we need to inspect the run status from core)
		runInfo, ok := s.Core.GetRunInfo(runID)
		if ok && runInfo.Status == StatusSkipped {
			status.State = forge.StateSkipped
			status.Description = "Build skipped (no ci task)"
		}
	}

	if err := f.UpdateStatus(ctx, event.Repo, event.SHA, status); err != nil {
		s.Logger.Error("update status final", "error", err)
	}
}

func (s *Service) Orchestrate(ctx context.Context, run *Run, event *forge.WebhookEvent, f forge.Forge, creds *forge.Credentials, dispatchParams runner.RunParams) bool {
	// Prepare generic CI environment variables
	env := map[string]string{
		"CI":                 "true",
		"MISE_CI":            "true",
		"GIT_CONFIG_COUNT":   "1",
		"GIT_CONFIG_KEY_0":   "safe.directory",
		"GIT_CONFIG_VALUE_0": "*",
	}

	// Add forge-specific environment variables
	forgeEnv := f.GetCIEnv(event)
	for k, v := range forgeEnv {
		env[k] = v
	}

	// Set GITHUB_TOKEN if we have a token (assuming GitHub forge)
	// We check if GITHUB_SERVER_URL is set to confirm it is indeed a GitHub environment
	if _, isGithub := env["GITHUB_SERVER_URL"]; isGithub && creds.Token != "" {
		s.Logger.Debug("injecting GITHUB_TOKEN into run environment")
		env["GITHUB_TOKEN"] = creds.Token
	}

	// Fetch variables from Forge (e.g., GitHub Variables)
	// We cannot fetch Secrets as their values are never returned by the API.
	// We handle errors softly to avoid breaking the build if permissions are missing.
	// This is done BEFORE waiting for connection to avoid watchdog timeout on worker if this takes too long
	vars, err := f.GetVariables(ctx, event.Repo)
	if err != nil {
		s.Logger.Warn("failed to fetch repository variables", "error", err)
		s.Core.AddLog(run.ID, "system", fmt.Sprintf("Warning: Failed to fetch repository variables: %v. Ensure the App has 'Variables: Read' permission.", err))
	} else {
		count := 0
		for k, v := range vars {
			env[k] = v
			count++
		}
		if count > 0 {
			s.Logger.Info("injected repository variables", "count", count)
			s.Core.AddLog(run.ID, "system", fmt.Sprintf("Injected %d repository variables.", count))
		}
	}
	// Clarify regarding secrets
	s.Core.AddLog(run.ID, "system", "Note: Only repository 'Variables' are fetched. 'Secrets' cannot be retrieved via API.")

	// Wait for worker to connect
	s.Logger.Info("waiting for worker to connect", "run_id", run.ID)
	if !s.waitForConnection(ctx, run, dispatchParams) {
		return false
	}
	s.Logger.Info("worker connected, starting orchestration", "run_id", run.ID)

	defer func() {
		run.CommandCh <- msgutil.NewCloseCommand(9999)
	}()

	// Build git clone URL with credentials
	cloneURL := event.Clone
	if creds.Token != "" {
		if u, err := url.Parse(event.Clone); err == nil {
			u.User = url.UserPassword("x-access-token", creds.Token)
			cloneURL = u.String()
		}
	}

	pipeline := orchestration.NewPipeline(run.ID, s.Core, s.Logger)
	var tasks []struct {
		Name string `json:"name"`
	}

	pipeline.AddStep("clone", "Cloning repository", func() error {
		return s.runCommandSync(run, env, "git", "clone", cloneURL, ".")
	}).AddStep("fetch", "Fetching ref", func() error {
		return s.runCommandSync(run, env, "git", "fetch", "origin", event.Ref)
	}).AddStep("checkout", "Checking out SHA", func() error {
		return s.runCommandSync(run, env, "git", "checkout", event.SHA)
	}).AddStep("trust", "Trusting mise", func() error {
		return s.runCommandSync(run, env, "mise", "trust")
	}).AddStep("install", "Installing tools", func() error {
		return s.runCommandSync(run, env, "mise", "install")
	}).AddStep("tasks", "Listing tasks", func() error {
		tasksOutput, err := s.runCommandCapture(run, env, "mise", "tasks", "--json")
		if err != nil {
			return err
		}
		return json.Unmarshal([]byte(tasksOutput), &tasks)
	}).AddStep("codegen", "Running codegen", func() error {
		if !hasTask(tasks, "codegen") {
			s.Logger.Info("no 'codegen' task found, skipping")
			return nil
		}
		if err := s.runCommandSync(run, env, "mise", "run", "codegen"); err != nil {
			s.Logger.Error("codegen failed, continuing", "error", err)
			s.Core.AddLog(run.ID, "system", "Codegen task failed, proceeding with caution.")
			return nil // soft fail
		}
		statusOutput, err := s.runCommandCapture(run, env, "git", "status", "--porcelain")
		if err != nil {
			// Log error and continue, don't fail the run
			s.Logger.Error("failed to check git status", "error", err)
			return nil
		}
		if strings.TrimSpace(statusOutput) == "" {
			return nil
		}
		s.Core.AddLog(run.ID, "system", "Codegen resulted in file changes. Creating PR...")
		branchName := "miseci-codegen"
		prPipeline := orchestration.NewPipeline(run.ID, s.Core, s.Logger)
		prPipeline.AddStep("configure-git", "Configuring git user", func() error {
			if err := s.runCommandSync(run, env, "git", "config", "user.name", "mise-ci"); err != nil {
				return err
			}
			return s.runCommandSync(run, env, "git", "config", "user.email", "mise-ci@localhost")
		}).AddStep("checkout-branch", "Checking out new branch", func() error {
			return s.runCommandSync(run, env, "git", "checkout", "-b", branchName)
		}).AddStep("add-changes", "Adding changes", func() error {
			return s.runCommandSync(run, env, "git", "add", "-A")
		}).AddStep("commit-changes", "Committing changes", func() error {
			return s.runCommandSync(run, env, "git", "commit", "-m", "chore: codegen updates")
		}).AddStep("push-changes", "Pushing changes", func() error {
			if err := s.runCommandSync(run, env, "git", "remote", "set-url", "origin", cloneURL); err != nil {
				return err
			}
			return s.runCommandSync(run, env, "git", "push", "origin", branchName, "--force")
		}).AddStep("create-pr", "Creating pull request", func() error {
			prURL, err := f.CreatePullRequest(ctx, event.Repo, event.Branch, branchName, "chore: codegen updates", "Automated codegen updates triggered by mise-ci.")
			if err != nil {
				return err
			}
			s.Core.AddLog(run.ID, "system", fmt.Sprintf("PR created: %s", prURL))
			return nil
		})
		if err := prPipeline.Run(); err != nil {
			s.Core.AddLog(run.ID, "system", fmt.Sprintf("Failed to create PR: %v", err))
		}
		return nil
	}).AddStep("ci", "Running CI", func() error {
		if !hasTask(tasks, "ci") {
			s.Logger.Info("no 'ci' task found, skipping")
			s.Core.AddLog(run.ID, "system", "No 'ci' task found in mise.toml, skipping CI step.")
			s.Core.UpdateStatus(run.ID, StatusSkipped, nil)
			return nil
		}
		return s.runCommandSync(run, env, "mise", "run", "ci")
	})

	if err := pipeline.Run(); err != nil {
		s.Core.UpdateStatus(run.ID, StatusFailure, nil)
		return false
	}

	runInfo, ok := s.Core.GetRunInfo(run.ID)
	if ok && runInfo.Status != StatusSkipped {
		s.Core.UpdateStatus(run.ID, StatusSuccess, nil)
	}

	s.Core.AddLog(run.ID, "system", "Build completed")

	return true
}

func hasTask(tasks []struct {
	Name string `json:"name"`
}, name string) bool {
	for _, task := range tasks {
		if task.Name == name {
			return true
		}
	}
	return false
}

func (s *Service) ListWorkerTokens(ctx context.Context) ([]*repository.WorkerToken, error) {
    return s.Core.repo.ListWorkerTokens(ctx)
}

func (s *Service) RevokeWorkerToken(ctx context.Context, id string) error {
    return s.Core.repo.RevokeWorkerToken(ctx, id, time.Now())
}

func (s *Service) DeleteWorkerToken(ctx context.Context, id string) error {
    return s.Core.repo.DeleteWorkerToken(ctx, id)
}

// runCommandSync executes a command and waits for it to complete
func (s *Service) runCommandSync(run *Run, env map[string]string, cmd string, args ...string) error {
	id := run.NextOpID.Add(1)
	s.Logger.Info("executing command", "cmd", cmd, "args", sanitize.SanitizeArgs(args))
	run.CommandCh <- msgutil.NewRunCommand(id, env, cmd, args...)

	if !s.waitForDone(run, cmd) {
		return fmt.Errorf("command failed: %s", cmd)
	}
	return nil
}

// runCommandCapture executes a command and captures stdout, returning it as a string
func (s *Service) runCommandCapture(run *Run, env map[string]string, cmd string, args ...string) (string, error) {
	id := run.NextOpID.Add(1)
	s.Logger.Info("executing command capture", "cmd", cmd, "args", s.sanitizeArgs(args))
	run.CommandCh <- msgutil.NewRunCommand(id, env, cmd, args...)

	var outputBuilder strings.Builder
	success := false

	// Custom loop to capture output
	for msg := range run.ResultCh {
		switch payload := msg.Payload.(type) {
		case *pb.WorkerMessage_Done:
			if payload.Done.ExitCode != 0 {
				return outputBuilder.String(), fmt.Errorf("exit code %d", payload.Done.ExitCode)
			}
			success = true
			goto Done
		case *pb.WorkerMessage_Error:
			return outputBuilder.String(), fmt.Errorf("worker error: %s", payload.Error.Message)
		case *pb.WorkerMessage_Output:
			if payload.Output.Stream == pb.Output_STDOUT {
				outputBuilder.Write(payload.Output.Data)
			}
			// We don't log to user logs here to keep it clean, unless debugging is needed
		}
	}

Done:
	if success {
		return outputBuilder.String(), nil
	}
	return outputBuilder.String(), fmt.Errorf("stream closed unexpectedly")
}

// copyFileToWorker sends a file to the worker
func (s *Service) copyFileToWorker(run *Run, source, dest string, data []byte) error {
	id := run.NextOpID.Add(1)
	run.CommandCh <- msgutil.NewCopyToWorker(id, source, dest, data)
	if !s.waitForDone(run, fmt.Sprintf("copy %s", source)) {
		return fmt.Errorf("failed to copy file: %s", source)
	}
	return nil
}

// copyFileFromWorker receives a file from the worker
func (s *Service) copyFileFromWorker(run *Run, source, dest string) error {
	id := run.NextOpID.Add(1)
	run.CommandCh <- msgutil.NewCopyFromWorker(id, source, dest)
	if !s.receiveFile(run, dest, fmt.Sprintf("copy %s", source)) {
		return fmt.Errorf("failed to receive file: %s", source)
	}
	return nil
}

func (s *Service) waitForConnection(ctx context.Context, run *Run, dispatchParams runner.RunParams) bool {
	for {
		select {
		case <-run.ConnectedCh:
			return true
		case <-run.RetryCh:
			s.Logger.Info("received retry signal, redispatching worker", "run_id", run.ID)
			s.Core.AddLog(run.ID, "system", "Worker handshake failed (version mismatch). Enqueuing another worker...")
			if _, err := s.Runner.Dispatch(ctx, dispatchParams); err != nil {
				s.Logger.Error("failed to redispatch worker", "error", err)
				s.Core.AddLog(run.ID, "system", fmt.Sprintf("Failed to enqueue worker: %v", err))
				// We don't return false here, we wait for next retry or timeout
			}
		case <-time.After(10 * time.Minute):
			s.Logger.Error("timeout waiting for worker to connect", "run_id", run.ID)
			s.Core.UpdateStatus(run.ID, StatusError, nil)
			return false
		}
	}
}

func (s *Service) waitForDone(run *Run, context string) bool {
	for msg := range run.ResultCh {
		switch payload := msg.Payload.(type) {
		case *pb.WorkerMessage_Done:
			if payload.Done.ExitCode != 0 {
				s.Logger.Error("command failed", "context", context, "exit_code", payload.Done.ExitCode)
				s.Core.AddLog(run.ID, "system", fmt.Sprintf("Command '%s' failed with exit code %d", context, payload.Done.ExitCode))
				return false
			}
			s.Core.AddLog(run.ID, "system", fmt.Sprintf("Command '%s' completed successfully", context))
			return true
		case *pb.WorkerMessage_Error:
			s.Logger.Error("worker error", "context", context, "message", payload.Error.Message)
			s.Core.AddLog(run.ID, "system", fmt.Sprintf("Error: %s", payload.Error.Message))
			return false
		case *pb.WorkerMessage_Output:
			stream := "stdout"
			if payload.Output.Stream == pb.Output_STDERR {
				stream = "stderr"
			}
			s.Core.AddLog(run.ID, stream, string(payload.Output.Data))
		case *pb.WorkerMessage_FileChunk:
			// Ignore file chunks here - they should be handled by receiveFile
			s.Logger.Warn("unexpected file chunk in waitForDone", "context", context)
		case *pb.WorkerMessage_SaveArtifact:
			s.Logger.Info("received artifact", "name", payload.SaveArtifact.Name, "run_id", run.ID)
			if err := s.HandleArtifact(run.ID, payload.SaveArtifact.Name, payload.SaveArtifact.Data); err != nil {
				s.Logger.Error("failed to handle artifact", "error", err)
				s.Core.AddLog(run.ID, "system", fmt.Sprintf("Failed to save artifact %s: %v", payload.SaveArtifact.Name, err))
			}
		}
	}
	return false
}

func (s *Service) HandleArtifact(runID, name string, data []byte) error {
	ctx := context.Background()

	// If it is a SARIF file, ingest it
	if strings.HasSuffix(name, ".sarif") {
		s.Logger.Info("ingesting sarif file", "run_id", runID, "name", name)
		if err := s.IngestSARIF(ctx, runID, data); err != nil {
			return fmt.Errorf("failed to ingest sarif: %w", err)
		}
		return nil
	}

	// Save other artifacts to storage
	// We wrap data in a byte reader
	if err := s.ArtifactStorage.Save(ctx, runID, name, strings.NewReader(string(data))); err != nil {
		s.Logger.Error("failed to save artifact to storage", "error", err)
		return err
	}

	return nil
}

type secretPattern struct {
	Pattern     *regexp.Regexp
	Replacement string
}

// secretPatterns is a list of compiled regular expressions and their corresponding replacements for detecting secrets.
var secretPatterns = []secretPattern{
	{
		// Redact value in 'Authorization: Bearer <token>' or 'Authorization: token <token>'
		Pattern:     regexp.MustCompile(`(?i)(Authorization:\s*(?:Bearer|token)\s+)\S+`),
		Replacement: `${1}[REDACTED]`,
	},
	{
		// Redact common key formats like 'api-key: value' or 'password = "value"'
		Pattern:     regexp.MustCompile(`(?i)((?:api-key|token|secret|password|key)(?:[\s=:]*['"]?))([a-zA-Z0-9_.-]{20,})(['"]?)`),
		Replacement: `${1}[REDACTED]${3}`,
	},
	{
		// Redact specific token patterns
		Pattern:     regexp.MustCompile(`ghp_[a-zA-Z0-9]{36}`),
		Replacement: "[REDACTED]",
	},
	{
		Pattern:     regexp.MustCompile(`glpat-[a-zA-Z0-9_-]{20,}`),
		Replacement: "[REDACTED]",
	},
	{
		// Redact JWTs
		Pattern:     regexp.MustCompile(`ey[J-Za-z0-9-_=]+\.[J-Za-z0-9-_=]+\.[J-Za-z0-9-_.+/=]*`),
		Replacement: "[REDACTED]",
	},
}

func (s *Service) sanitizeArgs(args []string) []string {
	sanitized := make([]string, len(args))
	for i, arg := range args {
		sanitizedArg := arg

		// 1. Handle well-formed URLs first, as it's the most robust method.
		if u, err := url.Parse(sanitizedArg); err == nil && u.User != nil {
			if _, isSet := u.User.Password(); isSet {
				u.User = url.UserPassword(u.User.Username(), "[REDACTED]")
				sanitized[i] = u.String()
				continue // Argument sanitized, move to the next one.
			}
		}

		// 2. If not a URL with a password, apply regex for other common secret patterns.
		for _, p := range secretPatterns {
			sanitizedArg = p.Pattern.ReplaceAllString(sanitizedArg, p.Replacement)
		}
		sanitized[i] = sanitizedArg
	}
	return sanitized
}

func (s *Service) receiveFile(run *Run, destPath string, context string) bool {
	f, err := os.Create(destPath)
	if err != nil {
		s.Logger.Error("failed to create file", "error", err, "path", destPath)
		return false
	}
	defer func() {
		if err := f.Close(); err != nil {
			s.Logger.Error("failed to close file", "path", destPath, "error", err)
		}
	}()

	for msg := range run.ResultCh {
		switch payload := msg.Payload.(type) {
		case *pb.WorkerMessage_FileChunk:
			if len(payload.FileChunk.Data) > 0 {
				if _, err := f.Write(payload.FileChunk.Data); err != nil {
					s.Logger.Error("failed to write chunk", "error", err)
					return false
				}
			}
			if payload.FileChunk.Eof {
				s.Logger.Info("file received successfully", "path", destPath)
				return true
			}
		case *pb.WorkerMessage_Error:
			s.Logger.Error("worker error", "context", context, "message", payload.Error.Message)
			return false
		case *pb.WorkerMessage_Done:
			// File transfer complete
			return true
		case *pb.WorkerMessage_Output:
			// Ignore output during file transfer
		}
	}
	return false
}
