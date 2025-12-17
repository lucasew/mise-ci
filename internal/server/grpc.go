package server

import (
	"io"
	"log/slog"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/lucasew/mise-ci/internal/core"
	pb "github.com/lucasew/mise-ci/internal/proto"
)

type GrpcServer struct {
	pb.UnimplementedServerServer
	core   *core.Core
	logger *slog.Logger
}

func NewGrpcServer(core *core.Core, logger *slog.Logger) *GrpcServer {
	return &GrpcServer{
		core:   core,
		logger: logger,
	}
}

func (s *GrpcServer) Connect(stream pb.Server_ConnectServer) error {
	// 1. Auth
	md, ok := metadata.FromIncomingContext(stream.Context())
	if !ok {
		return status.Error(codes.Unauthenticated, "missing metadata")
	}
	auth := md["authorization"]
	if len(auth) == 0 {
		return status.Error(codes.Unauthenticated, "missing authorization header")
	}

	token := strings.TrimPrefix(auth[0], "Bearer ")
	runID, err := s.core.ValidateToken(token)
	if err != nil {
		return status.Errorf(codes.Unauthenticated, "invalid token: %v", err)
	}

	run, ok := s.core.GetRun(runID)
	if !ok {
		return status.Errorf(codes.NotFound, "run %s not found", runID)
	}

	s.logger.Info("worker connected", "run_id", runID)

	// 2. Handshake - receive RunnerInfo
	msg, err := stream.Recv()
	if err != nil {
		return err
	}
	if info, ok := msg.Payload.(*pb.WorkerMessage_RunnerInfo); ok {
		s.logger.Info("received runner info", "hostname", info.RunnerInfo.Hostname)
	} else {
		return status.Error(codes.InvalidArgument, "expected RunnerInfo as first message")
	}

	// 3. Loop
	errCh := make(chan error, 1)

	// Sender: reads from run.CommandCh and sends to stream
	go func() {
		for cmd := range run.CommandCh {
			if err := stream.Send(cmd); err != nil {
				s.logger.Error("failed to send command", "error", err)
				errCh <- err
				return
			}
			if _, ok := cmd.Payload.(*pb.ServerMessage_Close); ok {
				return
			}
		}
	}()

	// Receiver: reads from stream and sends to run.ResultCh
	go func() {
		for {
			msg, err := stream.Recv()
			if err == io.EOF {
				errCh <- nil
				return
			}
			if err != nil {
				s.logger.Error("failed to receive message", "error", err)
				errCh <- err
				return
			}
			select {
			case run.ResultCh <- msg:
			case <-stream.Context().Done():
				return
			}
		}
	}()

	return <-errCh
}
