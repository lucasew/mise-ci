package stream

import (
	"context"
	"fmt"
	"log/slog"
)

// MessageStream defines a generic interface for sending and receiving messages
type MessageStream[TSend, TRecv any] interface {
	Send(msg TSend) error
	Recv() (TRecv, error)
}

// BidiConfig holds configuration for the bidirectional stream handler
type BidiConfig[TSend any] struct {
	Logger      *slog.Logger
	OnConnect   func() error
	OnClose     func()
	ShouldClose func(msg TSend) bool
}

// HandleBidiStream manages a generic bidirectional communication stream
func HandleBidiStream[TSend, TRecv any](
	ctx context.Context,
	stream MessageStream[TSend, TRecv],
	sendCh <-chan TSend,
	recvCh chan<- TRecv,
	cfg BidiConfig[TSend],
) error {
	if cfg.OnConnect != nil {
		if err := cfg.OnConnect(); err != nil {
			return fmt.Errorf("onConnect failed: %w", err)
		}
	}

	if cfg.OnClose != nil {
		defer cfg.OnClose()
	}

	// Used to signal errors or completion from goroutines
	errCh := make(chan error, 2)

	// Sender goroutine
	go func() {
		for {
			select {
			case <-ctx.Done():
				// Context cancelled, stop sending
				return
			case msg, ok := <-sendCh:
				if !ok {
					// Channel closed
					return
				}
				if err := stream.Send(msg); err != nil {
					errCh <- fmt.Errorf("failed to send message: %w", err)
					return
				}
				if cfg.ShouldClose != nil && cfg.ShouldClose(msg) {
					// Logic requested closure after this message
					errCh <- nil
					return
				}
			}
		}
	}()

	// Receiver goroutine
	go func() {
		for {
			msg, err := stream.Recv()
			if err != nil {
				errCh <- fmt.Errorf("failed to receive message: %w", err)
				return
			}
			select {
			case recvCh <- msg:
			case <-ctx.Done():
				return
			}
		}
	}()

	// Wait for the first error or completion
	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}
