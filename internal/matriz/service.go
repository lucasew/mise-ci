package matriz

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"mise-ci/internal/config"
	"mise-ci/internal/forge"
	pb "mise-ci/internal/proto"
	"mise-ci/internal/runner"
)

type Service struct {
	Core   *Core
	Forge  forge.Forge
	Runner runner.Runner
	Config *config.Config
	Logger *slog.Logger
}

func NewService(core *Core, f forge.Forge, r runner.Runner, cfg *config.Config, logger *slog.Logger) *Service {
	return &Service{
		Core:   core,
		Forge:  f,
		Runner: r,
		Config: cfg,
		Logger: logger,
	}
}

func (s *Service) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	event, err := s.Forge.ParseWebhook(r)
	if err != nil {
		s.Logger.Error("parse webhook", "error", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if event == nil {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ignored"))
		return
	}

	s.Logger.Info("received webhook", "repo", event.Repo, "sha", event.SHA)

	go s.StartRun(event)

	w.WriteHeader(http.StatusAccepted)
	w.Write([]byte("accepted"))
}

func (s *Service) StartRun(event *forge.WebhookEvent) {
	ctx := context.Background()
	runID := fmt.Sprintf("%s-%d", event.SHA[:8], time.Now().Unix())

	s.Logger.Info("starting run", "run_id", runID)

	// Update status
	status := forge.Status{
		State:       forge.StatePending,
		Context:     "mise-ci",
		Description: "Run started",
		TargetURL:   "", // TODO: Link to logs
	}
	if err := s.Forge.UpdateStatus(ctx, event.Repo, event.SHA, status); err != nil {
		s.Logger.Error("update status pending", "error", err)
	}

	run := s.Core.CreateRun(runID)
	token, err := s.Core.GenerateToken(runID)
	if err != nil {
		s.Logger.Error("generate token", "error", err)
		return
	}

	// Dispatch
	callback := s.Config.Server.PublicURL
	if callback == "" {
		callback = s.Config.Server.GRPCAddr
	}

	params := runner.RunParams{
		CallbackURL: callback,
		Token:       token,
		Image:       s.Config.Runner.DefaultImage,
	}

	// If GRPCAddr starts with :, prepend hostname or use configured external URL
	// For now assuming GRPCAddr is reachable by worker (e.g. within same network)
	// Ideally config should have PublicURL

	jobID, err := s.Runner.Dispatch(ctx, params)
	if err != nil {
		s.Logger.Error("dispatch job", "error", err)
		status.State = forge.StateError
		status.Description = "Failed to dispatch job"
		s.Forge.UpdateStatus(ctx, event.Repo, event.SHA, status)
		return
	}

	s.Logger.Info("job dispatched", "job_id", jobID)

	// Orchestrate
	success := s.Orchestrate(ctx, run, event)

	// Final status
	status.State = forge.StateSuccess
	status.Description = "Build passed"
	if !success {
		status.State = forge.StateFailure
		status.Description = "Build failed"
	}

	if err := s.Forge.UpdateStatus(ctx, event.Repo, event.SHA, status); err != nil {
		s.Logger.Error("update status final", "error", err)
	}

	// Cleanup?
}

func (s *Service) Orchestrate(ctx context.Context, run *Run, event *forge.WebhookEvent) bool {
	// 1. Get credentials
	creds, err := s.Forge.CloneCredentials(ctx, event.Repo)
	if err != nil {
		s.Logger.Error("get credentials", "error", err)
		return false
	}

	// 2. Send Copy (Clone)
	s.Logger.Info("sending copy (clone)")
	run.CommandCh <- &pb.MatrizMessage{
		Id: 1,
		Payload: &pb.MatrizMessage_Copy{
			Copy: &pb.Copy{
				Direction: pb.Copy_TO_WORKER,
				Source:    event.Clone,
				Dest:      ".",
				Creds: &pb.Credentials{
					Auth: &pb.Credentials_Token{
						Token: creds.Token,
					},
				},
			},
		},
	}

	if !s.waitForDone(run, "clone") {
		return false
	}

	// 3. Run mise trust
	if !s.runCommand(run, 2, "mise", "trust") {
		return false
	}

	// 4. Run mise install
	if !s.runCommand(run, 3, "mise", "install") {
		return false
	}

	// 5. Run mise run ci
	if !s.runCommand(run, 4, "mise", "run", "ci") {
		return false
	}

	// 6. Close
	run.CommandCh <- &pb.MatrizMessage{
		Id: 5,
		Payload: &pb.MatrizMessage_Close{
			Close: &pb.Close{},
		},
	}

	return true
}

func (s *Service) runCommand(run *Run, id uint64, cmd string, args ...string) bool {
	s.Logger.Info("sending run", "cmd", cmd, "args", args)
	run.CommandCh <- &pb.MatrizMessage{
		Id: id,
		Payload: &pb.MatrizMessage_Run{
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
			// Log output?
			// For now just dropping it or printing to stdout
			// Real impl would save logs
			// fmt.Print(string(payload.Output.Data))
		}
	}
	return false
}
