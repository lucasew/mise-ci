package core

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

	s.Logger.Info("sending copy (clone)")
	run.CommandCh <- &pb.ServerMessage{
		Id: 1,
		Payload: &pb.ServerMessage_Copy{
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
