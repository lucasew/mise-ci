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

	"github.com/lucasew/mise-ci/internal/config"
	"github.com/lucasew/mise-ci/internal/forge"
	"github.com/lucasew/mise-ci/internal/httputil"
	"github.com/lucasew/mise-ci/internal/msgutil"
	"github.com/lucasew/mise-ci/internal/orchestration"
	pb "github.com/lucasew/mise-ci/internal/proto"
	"github.com/lucasew/mise-ci/internal/runner"
)

//go:embed testdata/mise.toml
var testProjectMiseToml string

type Service struct {
	Core   *Core
	Forges []forge.Forge
	Runner runner.Runner
	Config *config.Config
	Logger *slog.Logger
}

func NewService(core *Core, forges []forge.Forge, r runner.Runner, cfg *config.Config, logger *slog.Logger) *Service {
	return &Service{
		Core:   core,
		Forges: forges,
		Runner: r,
		Config: cfg,
		Logger: logger,
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

	run := s.Core.CreateRun(runID)
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
	go s.TestOrchestrate(ctx, run)

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
	json.NewEncoder(w).Encode(response)
}

func (s *Service) TestOrchestrate(ctx context.Context, run *Run) {
	// Wait for worker to connect
	s.Logger.Info("waiting for worker to connect", "run_id", run.ID)
	<-run.ConnectedCh
	s.Logger.Info("worker connected, starting orchestration", "run_id", run.ID)

	// Create temporary test project directory
	testDir, err := os.MkdirTemp("", "mise-ci-test-*")
	if err != nil {
		s.Logger.Error("failed to create temp dir", "error", err)
		s.Core.UpdateStatus(run.ID, StatusError, nil)
		return
	}
	defer os.RemoveAll(testDir)

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

	pipeline.
		AddStep("copy-config", "Copying mise.toml to worker", func() error {
			return s.copyFileToWorker(run, 1, "mise.toml", "mise.toml", miseTomlData)
		}).
		AddStep("trust", "Trusting mise configuration", func() error {
			return s.runCommandSync(run, 2, nil, "mise", "trust")
		}).
		AddStep("run-ci", "Starting CI task", func() error {
			return s.runCommandSync(run, 3, nil, "mise", "run", "ci")
		}).
		AddStep("retrieve", "Retrieving output artifacts", func() error {
			return s.copyFileFromWorker(run, 4, "output.txt", outputPath)
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

	// Close connection
	run.CommandCh <- msgutil.NewCloseCommand(5)
}

func (s *Service) StartRun(event *forge.WebhookEvent, f forge.Forge) {
	ctx := context.Background()
	runID := fmt.Sprintf("%s-%d", event.SHA[:8], time.Now().Unix())

	s.Logger.Info("starting run", "run_id", runID)

	run := s.Core.CreateRun(runID)

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

	token, err := s.Core.GenerateWorkerToken(runID)
	if err != nil {
		s.Logger.Error("generate token", "error", err)
		return
	}

	callback := publicURL
	params := runner.RunParams{
		CallbackURL: callback,
		Token:       token,
		Image:       s.Config.Nomad.DefaultImage,
	}

	jobID, err := s.Runner.Dispatch(ctx, params)
	if err != nil {
		s.Logger.Error("dispatch job", "error", err)
		status.State = forge.StateError
		status.Description = "Failed to dispatch job"
		f.UpdateStatus(ctx, event.Repo, event.SHA, status)
		return
	}

	s.Logger.Info("job dispatched", "job_id", jobID)

	success := s.Orchestrate(ctx, run, event, f)

	status.State = forge.StateSuccess
	status.Description = "Build passed"
	if !success {
		status.State = forge.StateFailure
		status.Description = "Build failed"
	}

	if err := f.UpdateStatus(ctx, event.Repo, event.SHA, status); err != nil {
		s.Logger.Error("update status final", "error", err)
	}
}

func (s *Service) Orchestrate(ctx context.Context, run *Run, event *forge.WebhookEvent, f forge.Forge) bool {
	// Wait for worker to connect
	s.Logger.Info("waiting for worker to connect", "run_id", run.ID)
	<-run.ConnectedCh
	s.Logger.Info("worker connected, starting orchestration", "run_id", run.ID)

	creds, err := f.CloneCredentials(ctx, event.Repo)
	if err != nil {
		s.Logger.Error("get credentials", "error", err)
		s.Core.UpdateStatus(run.ID, StatusError, nil)
		return false
	}

	// Prepare generic CI environment variables
	env := map[string]string{
		"CI":      "true",
		"MISE_CI": "true",
	}

	// Add forge-specific environment variables
	forgeEnv := f.GetCIEnv(event)
	for k, v := range forgeEnv {
		env[k] = v
	}

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
	if !s.runCommand(run, 1, env, "git", "clone", cloneURL, ".") {
		s.Core.UpdateStatus(run.ID, StatusFailure, nil)
		return false
	}

	if !s.runCommand(run, 2, env, "mise", "trust") {
		s.Core.UpdateStatus(run.ID, StatusFailure, nil)
		return false
	}

	if !s.runCommand(run, 3, env, "mise", "install") {
		s.Core.UpdateStatus(run.ID, StatusFailure, nil)
		return false
	}

	if !s.runCommand(run, 4, env, "mise", "run", "ci") {
		s.Core.UpdateStatus(run.ID, StatusFailure, nil)
		return false
	}

	s.Core.UpdateStatus(run.ID, StatusSuccess, nil)
	s.Core.AddLog(run.ID, "system", "Build completed successfully")

	run.CommandCh <- &pb.ServerMessage{
		Id: 5,
		Payload: &pb.ServerMessage_Close{
			Close: &pb.Close{},
		},
	}

	return true
}

func (s *Service) runCommand(run *Run, id uint64, env map[string]string, cmd string, args ...string) bool {
	return s.runCommandSync(run, id, env, cmd, args...) == nil
}

// runCommandSync executes a command and waits for it to complete
func (s *Service) runCommandSync(run *Run, id uint64, env map[string]string, cmd string, args ...string) error {
	s.Logger.Info("executing command", "cmd", cmd, "args", args)
	run.CommandCh <- msgutil.NewRunCommand(id, env, cmd, args...)

	if !s.waitForDone(run, cmd) {
		return fmt.Errorf("command failed: %s", cmd)
	}
	return nil
}

// copyFileToWorker sends a file to the worker
func (s *Service) copyFileToWorker(run *Run, id uint64, source, dest string, data []byte) error {
	run.CommandCh <- msgutil.NewCopyToWorker(id, source, dest, data)
	if !s.waitForDone(run, fmt.Sprintf("copy %s", source)) {
		return fmt.Errorf("failed to copy file: %s", source)
	}
	return nil
}

// copyFileFromWorker receives a file from the worker
func (s *Service) copyFileFromWorker(run *Run, id uint64, source, dest string) error {
	run.CommandCh <- msgutil.NewCopyFromWorker(id, source, dest)
	if !s.receiveFile(run, dest, fmt.Sprintf("copy %s", source)) {
		return fmt.Errorf("failed to receive file: %s", source)
	}
	return nil
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

func (s *Service) receiveFile(run *Run, destPath string, context string) bool {
	f, err := os.Create(destPath)
	if err != nil {
		s.Logger.Error("failed to create file", "error", err, "path", destPath)
		return false
	}
	defer f.Close()

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
