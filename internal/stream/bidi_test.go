package stream

import (
	"context"
	"testing"
	"time"
)

type MockStream[TSend, TRecv any] struct {
	sendFunc func(msg TSend) error
	recvFunc func() (TRecv, error)
}

func (m *MockStream[TSend, TRecv]) Send(msg TSend) error {
	if m.sendFunc != nil {
		return m.sendFunc(msg)
	}
	return nil
}

func (m *MockStream[TSend, TRecv]) Recv() (TRecv, error) {
	if m.recvFunc != nil {
		return m.recvFunc()
	}
	var zero TRecv
	return zero, nil
}

func TestHandleBidiStream_BlockingSend(t *testing.T) {
	// this test verifies that the send to recvCh is blocking

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	recvCh := make(chan string) // unbuffered
	sendCh := make(chan string)

	msgSent := make(chan struct{})

	stream := &MockStream[string, string]{
		recvFunc: func() (string, error) {
			select {
			case <-msgSent:
				// after sending one message, block until context done
				<-ctx.Done()
				return "", ctx.Err()
			default:
				close(msgSent)
				return "test-message", nil
			}
		},
	}

	go func() {
		_ = HandleBidiStream(ctx, stream, sendCh, recvCh, BidiConfig[string]{})
	}()

	// Wait enough time for the receiver goroutine to try to send to recvCh
	// In the buggy version, it will hit default case and drop the message.
	// In the fixed version, it will block.
	time.Sleep(100 * time.Millisecond)

	// Now try to receive
	select {
	case msg := <-recvCh:
		if msg != "test-message" {
			t.Errorf("expected 'test-message', got %v", msg)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timed out waiting for message - it was likely dropped due to non-blocking send")
	}
}
