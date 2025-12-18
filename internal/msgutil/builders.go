package msgutil

import (
	pb "github.com/lucasew/mise-ci/internal/proto"
)

// NewRunCommand creates a Run command message
func NewRunCommand(id uint64, env map[string]string, cmd string, args ...string) *pb.ServerMessage {
	return &pb.ServerMessage{
		Id: id,
		Payload: &pb.ServerMessage_Run{
			Run: &pb.Run{
				Cmd:  cmd,
				Args: args,
				Env:  env,
			},
		},
	}
}

// NewCopyToWorker creates a copy to worker command message
func NewCopyToWorker(id uint64, source, dest string, data []byte) *pb.ServerMessage {
	return &pb.ServerMessage{
		Id: id,
		Payload: &pb.ServerMessage_Copy{
			Copy: &pb.Copy{
				Direction: pb.Copy_TO_WORKER,
				Source:    source,
				Dest:      dest,
				Data:      data,
			},
		},
	}
}

// NewCopyFromWorker creates a copy from worker command message
func NewCopyFromWorker(id uint64, source, dest string) *pb.ServerMessage {
	return &pb.ServerMessage{
		Id: id,
		Payload: &pb.ServerMessage_Copy{
			Copy: &pb.Copy{
				Direction: pb.Copy_FROM_WORKER,
				Source:    source,
				Dest:      dest,
			},
		},
	}
}

// NewCloseCommand creates a Close command message
func NewCloseCommand(id uint64) *pb.ServerMessage {
	return &pb.ServerMessage{
		Id: id,
		Payload: &pb.ServerMessage_Close{
			Close: &pb.Close{},
		},
	}
}
