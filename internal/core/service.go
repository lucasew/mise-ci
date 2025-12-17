package core

import (
	"context"
	_ "embed"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/lucasew/mise-ci/internal/config"
	"github.com/lucasew/mise-ci/internal/forge"
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
			w.WriteHeader(http.StatusAccepted)
			w.Write([]byte("accepted"))
			return
		}
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ignored"))
}

func (s *Service) HandleTestDispatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		w.Write([]byte("only POST allowed"))
		return
	}

	ctx := context.Background()
	runID := fmt.Sprintf("test-%d", time.Now().Unix())

	s.Logger.Info("test dispatch", "run_id", runID)

	run := s.Core.CreateRun(runID)
	token, err := s.Core.GenerateToken(runID)
	if err != nil {
		s.Logger.Error("generate token", "error", err)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("error: %v", err)))
		return
	}

	callback := s.Config.Server.PublicURL
	if callback == "" {
		callback = s.Config.Server.HTTPAddr
	}

	params := runner.RunParams{
		CallbackURL: callback,
		Token:       token,
		Image:       s.Config.Nomad.DefaultImage,
	}

	jobID, err := s.Runner.Dispatch(ctx, params)
	if err != nil {
		s.Logger.Error("dispatch job", "error", err)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("error: %v", err)))
		return
	}

	s.Logger.Info("test job dispatched", "job_id", jobID, "run_id", runID)

	// Run test orchestration in background
	go s.TestOrchestrate(ctx, run)

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(fmt.Sprintf("dispatched: run_id=%s job_id=%s", runID, jobID)))
}

func (s *Service) TestOrchestrate(ctx context.Context, run *Run) {
	// Create temporary test project directory
	testDir, err := os.MkdirTemp("", "mise-ci-test-*")
	if err != nil {
		s.Logger.Error("failed to create temp dir", "error", err)
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

	// 1. Send mise.toml to worker
	s.Logger.Info("sending mise.toml to worker")
	miseTomlData, err := os.ReadFile(miseTomlPath)
	if err != nil {
		s.Logger.Error("failed to read mise.toml", "error", err)
		return
	}

	run.CommandCh <- &pb.ServerMessage{
		Id: 1,
		Payload: &pb.ServerMessage_Copy{
			Copy: &pb.Copy{
				Direction: pb.Copy_TO_WORKER,
				Source:    "mise.toml",
				Dest:      "mise.toml",
				Data:      miseTomlData,
			},
		},
	}
	if !s.waitForDone(run, "copy mise.toml") {
		s.Logger.Error("failed to copy mise.toml")
		return
	}

	// 2. Trust mise config
	if !s.runCommand(run, 2, "mise", "trust") {
		s.Logger.Error("mise trust failed")
		return
	}

	// 3. Run CI task
	if !s.runCommand(run, 3, "mise", "run", "ci") {
		s.Logger.Error("mise run ci failed")
		return
	}

	// 4. Copy output.txt back from worker
	s.Logger.Info("requesting output.txt from worker")
	outputPath := testDir + "/output.txt"
	run.CommandCh <- &pb.ServerMessage{
		Id: 4,
		Payload: &pb.ServerMessage_Copy{
			Copy: &pb.Copy{
				Direction: pb.Copy_FROM_WORKER,
				Source:    "output.txt",
				Dest:      outputPath,
			},
		},
	}
	if !s.waitForDone(run, "copy output.txt") {
		s.Logger.Error("failed to copy output.txt")
		return
	}

	// 5. Read and log the output
	output, err := os.ReadFile(outputPath)
	if err != nil {
		s.Logger.Error("failed to read output.txt", "error", err)
		return
	}

	s.Logger.Info("=== Test Output ===")
	s.Logger.Info(string(output))
	s.Logger.Info("===================")
	s.Logger.Info("test completed successfully")

	// Close connection
	run.CommandCh <- &pb.ServerMessage{
		Id: 5,
		Payload: &pb.ServerMessage_Close{
			Close: &pb.Close{},
		},
	}
}

func (s *Service) StartRun(event *forge.WebhookEvent, f forge.Forge) {
	ctx := context.Background()
	runID := fmt.Sprintf("%s-%d", event.SHA[:8], time.Now().Unix())

	s.Logger.Info("starting run", "run_id", runID)

	status := forge.Status{
		State:       forge.StatePending,
		Context:     "mise-ci",
		Description: "Run started",
		TargetURL:   "",
	}
	if err := f.UpdateStatus(ctx, event.Repo, event.SHA, status); err != nil {
		s.Logger.Error("update status pending", "error", err)
	}

	run := s.Core.CreateRun(runID)
	token, err := s.Core.GenerateToken(runID)
	if err != nil {
		s.Logger.Error("generate token", "error", err)
		return
	}

	callback := s.Config.Server.PublicURL
	if callback == "" {
		callback = s.Config.Server.HTTPAddr
	}

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
	creds, err := f.CloneCredentials(ctx, event.Repo)
	if err != nil {
		s.Logger.Error("get credentials", "error", err)
		return false
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
	if !s.runCommand(run, 1, "git", "clone", cloneURL, ".") {
		return false
	}

	if !s.runCommand(run, 2, "mise", "trust") {
		return false
	}

	if !s.runCommand(run, 3, "mise", "install") {
		return false
	}

	if !s.runCommand(run, 4, "mise", "run", "ci") {
		return false
	}

	run.CommandCh <- &pb.ServerMessage{
		Id: 5,
		Payload: &pb.ServerMessage_Close{
			Close: &pb.Close{},
		},
	}

	return true
}

func (s *Service) runCommand(run *Run, id uint64, cmd string, args ...string) bool {
	s.Logger.Info("sending run", "cmd", cmd, "args", args)
	run.CommandCh <- &pb.ServerMessage{
		Id: id,
		Payload: &pb.ServerMessage_Run{
			Run: &pb.Run{
				Cmd:  cmd,
				Args: args,
			},
		},
	}
	return s.waitForDone(run, cmd)
}

func (s *Service) waitForDone(run *Run, context string) bool {
	for msg := range run.ResultCh {
		switch payload := msg.Payload.(type) {
		case *pb.WorkerMessage_Done:
			if payload.Done.ExitCode != 0 {
				s.Logger.Error("command failed", "context", context, "exit_code", payload.Done.ExitCode)
				return false
			}
			return true
		case *pb.WorkerMessage_Error:
			s.Logger.Error("worker error", "context", context, "message", payload.Error.Message)
			return false
		case *pb.WorkerMessage_Output:
			// log
		}
	}
	return false
}
