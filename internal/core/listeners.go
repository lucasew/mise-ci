package core

import (
	"sync"
)

// ListenerManager manages a list of listeners with broadcast capabilities
type ListenerManager[T any] struct {
	mu        sync.RWMutex
	listeners []chan T
}

// NewListenerManager creates a new ListenerManager
func NewListenerManager[T any]() *ListenerManager[T] {
	return &ListenerManager[T]{
		listeners: make([]chan T, 0),
	}
}

// Subscribe adds a new listener and returns its channel
func (lm *ListenerManager[T]) Subscribe() chan T {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	ch := make(chan T, 100)
	lm.listeners = append(lm.listeners, ch)
	return ch
}

// Unsubscribe removes a listener
func (lm *ListenerManager[T]) Unsubscribe(ch chan T) {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	for i, listener := range lm.listeners {
		if listener == ch {
			// Close the channel
			close(ch)
			// Remove from slice
			lm.listeners = append(lm.listeners[:i], lm.listeners[i+1:]...)
			return
		}
	}
}

// Broadcast sends a value to all listeners
func (lm *ListenerManager[T]) Broadcast(value T) {
	lm.mu.RLock()
	defer lm.mu.RUnlock()

	for _, ch := range lm.listeners {
		select {
		case ch <- value:
		default:
			// If channel is full, skip to prevent blocking
		}
	}
}
