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
	"time"

	"github.com/lucasew/mise-ci/internal/artifacts"
	"github.com/lucasew/mise-ci/internal/config"
	"github.com/lucasew/mise-ci/internal/forge"
	"github.com/lucasew/mise-ci/internal/httputil"
	"github.com/lucasew/mise-ci/internal/msgutil"
	"github.com/lucasew/mise-ci/internal/repository"
	"github.com/lucasew/mise-ci/internal/orchestration"
	pb "github.com/lucasew/mise-ci/internal/proto"
	"github.com/lucasew/mise-ci/internal/runner"
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
	token, err := s.Core.GenerateWorkerToken(runID)
	if err != nil {
		s.Logger.Error("generate token", "error", err)
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

	token, err := s.Core.GenerateWorkerToken(runID)
	if err != nil {
		s.Logger.Error("generate token", "error", err)
		s.Core.UpdateStatus(runID, StatusError, nil)
		return
	}

	// Prepare environment
	env := map[string]string{
		"CI":                 "true",
		"MISE_CI":            "true",
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
		"CI":      "true",
		"MISE_CI": "true",
		// Configure git to trust the directory via env vars (cleaner than running git config commands)
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
		run.CommandCh <- &pb.ServerMessage{
			Id: 9999,
			Payload: &pb.ServerMessage_Close{
				Close: &pb.Close{},
			},
		}
	}()

	// Build git clone URL with credentials
	cloneURL := event.Clone
	if creds.Token != "" {
		// Parse URL and add token
		if u, err := url.Parse(event.Clone); err == nil {
			u.User = url.UserPassword("x-access-token", creds.Token)
			cloneURL = u.String()
		}
	}

	s.Logger.Info("cloning repository")
	// Clone typically doesn't need the env vars, but we pass them for consistency if needed later
	if !s.runCommand(run, env, "git", "clone", cloneURL, ".") {
		s.Core.UpdateStatus(run.ID, StatusFailure, nil)
		return false
	}

	if !s.runCommand(run, env, "git", "fetch", "origin", event.Ref) {
		s.Core.UpdateStatus(run.ID, StatusFailure, nil)
		return false
	}

	if !s.runCommand(run, env, "git", "checkout", event.SHA) {
		s.Core.UpdateStatus(run.ID, StatusFailure, nil)
		return false
	}

	if !s.runCommand(run, env, "mise", "trust") {
		s.Core.UpdateStatus(run.ID, StatusFailure, nil)
		return false
	}

	if !s.runCommand(run, env, "mise", "install") {
		s.Core.UpdateStatus(run.ID, StatusFailure, nil)
		return false
	}

	// Check tasks
	tasksOutput, err := s.runCommandCapture(run, env, "mise", "tasks", "--json")
	if err != nil {
		s.Logger.Error("failed to list tasks", "error", err)
		s.Core.UpdateStatus(run.ID, StatusFailure, nil)
		return false
	}

	var tasks []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(tasksOutput), &tasks); err != nil {
		s.Logger.Error("failed to parse tasks json", "error", err)
		s.Core.UpdateStatus(run.ID, StatusFailure, nil)
		return false
	}

	hasCITask := false
	hasCodegenTask := false
	for _, task := range tasks {
		if task.Name == "ci" {
			hasCITask = true
		}
		if task.Name == "codegen" {
			hasCodegenTask = true
		}
	}

	if hasCodegenTask {
		s.Logger.Info("running codegen task")
		// If codegen fails, we log it but continue ("se der pau só avisa no status e segue o jogo")
		if err := s.runCommandSync(run, env, "mise", "run", "codegen"); err != nil {
			s.Logger.Error("codegen failed", "error", err)
			s.Core.AddLog(run.ID, "system", "Codegen task failed, proceeding with caution.")
		} else {
			// Check for dirty tree
			statusOutput, err := s.runCommandCapture(run, env, "git", "status", "--porcelain")
			if err != nil {
				s.Logger.Error("failed to check git status", "error", err)
			} else if strings.TrimSpace(statusOutput) != "" {
				s.Logger.Info("codegen resulted in dirty tree, creating PR")
				s.Core.AddLog(run.ID, "system", "Codegen resulted in file changes. Creating PR...")

				// Create PR
				branchName := "miseci-codegen"

				// Configure git user
				s.runCommand(run, env, "git", "config", "user.name", "mise-ci")
				s.runCommand(run, env, "git", "config", "user.email", "mise-ci@localhost")

				s.runCommand(run, env, "git", "checkout", "-b", branchName)
				s.runCommand(run, env, "git", "add", "-A")
				s.runCommand(run, env, "git", "commit", "-m", "chore: codegen updates")

				// Push
				// Need to re-add token to remote URL if needed, but we can just use the token in env?
				// git push usually needs credentials helper or URL with token.
				// In Orchestrate we already have cloneURL which might have the token.
				// But we are pushing to 'origin'.
				// Let's set the remote url to be sure.
				s.runCommand(run, env, "git", "remote", "set-url", "origin", cloneURL)

				if s.runCommand(run, env, "git", "push", "origin", branchName, "--force") {
					prURL, err := f.CreatePullRequest(ctx, event.Repo, event.Branch, branchName, "chore: codegen updates", "Automated codegen updates triggered by mise-ci.")
					if err != nil {
						s.Logger.Error("failed to create PR", "error", err)
						s.Core.AddLog(run.ID, "system", fmt.Sprintf("Failed to create PR: %v", err))
					} else {
						s.Logger.Info("PR created", "url", prURL)
						s.Core.AddLog(run.ID, "system", fmt.Sprintf("PR created: %s", prURL))
					}
				} else {
					s.Logger.Error("failed to push branch")
					s.Core.AddLog(run.ID, "system", "Failed to push codegen branch")
				}
			}
		}
	}

	if hasCITask {
		if !s.runCommand(run, env, "mise", "run", "ci") {
			s.Core.UpdateStatus(run.ID, StatusFailure, nil)
			return false
		}
		s.Core.UpdateStatus(run.ID, StatusSuccess, nil)
	} else {
		s.Logger.Info("no 'ci' task found, skipping")
		s.Core.AddLog(run.ID, "system", "No 'ci' task found in mise.toml, skipping CI step.")
		s.Core.UpdateStatus(run.ID, StatusSkipped, nil)
	}

	s.Core.AddLog(run.ID, "system", "Build completed")

	return true
}

func (s *Service) runCommand(run *Run, env map[string]string, cmd string, args ...string) bool {
	return s.runCommandSync(run, env, cmd, args...) == nil
}

// runCommandSync executes a command and waits for it to complete
func (s *Service) runCommandSync(run *Run, env map[string]string, cmd string, args ...string) error {
	id := run.NextOpID.Add(1)
	s.Logger.Info("executing command", "cmd", cmd, "args", s.sanitizeArgs(args))
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
		}
	}
	return false
}

func (s *Service) sanitizeArgs(args []string) []string {
	sanitized := make([]string, len(args))
	for i, arg := range args {
		if strings.Contains(arg, "x-access-token") {
			if u, err := url.Parse(arg); err == nil && u.User != nil {
				u.User = url.UserPassword(u.User.Username(), "REDACTED")
				sanitized[i] = u.String()
				continue
			}
		}
		sanitized[i] = arg
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
