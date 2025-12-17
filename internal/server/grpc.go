package server

import (
	"errors"
	"io"
	"log/slog"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/lucasew/mise-ci/internal/core"
	pb "github.com/lucasew/mise-ci/internal/proto"
	"github.com/lucasew/mise-ci/internal/stream"
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

// grpcStreamAdapter adapts pb.Server_ConnectServer to MessageStream interface
type grpcStreamAdapter struct {
	stream pb.Server_ConnectServer
}

func (a *grpcStreamAdapter) Send(msg *pb.ServerMessage) error {
	return a.stream.Send(msg)
}

func (a *grpcStreamAdapter) Recv() (*pb.WorkerMessage, error) {
	return a.stream.Recv()
}

func (s *GrpcServer) Connect(serverStream pb.Server_ConnectServer) error {
	// 1. Auth
	md, ok := metadata.FromIncomingContext(serverStream.Context())
	if !ok {
		return status.Error(codes.Unauthenticated, "missing metadata")
	}
	auth := md["authorization"]
	if len(auth) == 0 {
		return status.Error(codes.Unauthenticated, "missing authorization header")
	}

	token := strings.TrimPrefix(auth[0], "Bearer ")
	runID, tokenType, err := s.core.ValidateToken(token)
	if err != nil {
		return status.Errorf(codes.Unauthenticated, "invalid token: %v", err)
	}

	if tokenType != core.TokenTypeWorker {
		return status.Errorf(codes.Unauthenticated, "invalid token type: %s", tokenType)
	}

	run, ok := s.core.GetRun(runID)
	if !ok {
		return status.Errorf(codes.NotFound, "run %s not found", runID)
	}

	s.logger.Info("worker connected", "run_id", runID)

	adapter := &grpcStreamAdapter{stream: serverStream}

	err = stream.HandleBidiStream(
		serverStream.Context(),
		adapter,
		run.CommandCh,
		run.ResultCh,
		stream.BidiConfig[*pb.ServerMessage]{
			Logger: s.logger,
			OnConnect: func() error {
				// 2. Handshake - receive RunnerInfo
				msg, err := adapter.Recv()
				if err != nil {
					return err
				}
				if info, ok := msg.Payload.(*pb.WorkerMessage_RunnerInfo); ok {
					s.logger.Info("received runner info", "hostname", info.RunnerInfo.Hostname)
					return nil
				}
				return status.Error(codes.InvalidArgument, "expected RunnerInfo as first message")
			},
			ShouldClose: func(msg *pb.ServerMessage) bool {
				_, ok := msg.Payload.(*pb.ServerMessage_Close)
				return ok
			},
		},
	)

	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}
