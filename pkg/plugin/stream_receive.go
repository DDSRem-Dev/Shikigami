package plugin

import (
	"context"
	"errors"
	"io"
	"sync"
)

type messageReceiveResult[T any] struct {
	message *T
	err     error
}

// messageReceiver owns one transport reader. Cancellation is terminal: no new
// Recv can start after a canceled Receive. Returning from the gRPC handler
// cancels its transport context and releases an in-flight Recv; close does not
// claim to have joined that transport operation before the handler returns.
type messageReceiver[T any] struct {
	mu       sync.Mutex
	recv     func() (*T, error)
	requests chan chan messageReceiveResult[T]
	stop     chan struct{}
	terminal error
	started  bool
	closed   bool
}

func newMessageReceiver[T any](recv func() (*T, error)) *messageReceiver[T] {
	return &messageReceiver[T]{recv: recv, requests: make(chan chan messageReceiveResult[T]), stop: make(chan struct{})}
}

func (r *messageReceiver[T]) run() {
	for {
		select {
		case <-r.stop:
			return
		case result := <-r.requests:
			message, err := r.recv()
			result <- messageReceiveResult[T]{message, err}
		}
	}
}

func (r *messageReceiver[T]) closeLocked() {
	if !r.closed {
		r.closed = true
		close(r.stop)
	}
}

func (r *messageReceiver[T]) close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closeLocked()
}

func (r *messageReceiver[T]) failure() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if errors.Is(r.terminal, io.EOF) {
		return nil
	}
	return r.terminal
}

func (r *messageReceiver[T]) receive(ctx context.Context) (*T, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.terminal != nil {
		return nil, r.terminal
	}
	if err := ctx.Err(); err != nil {
		r.terminal = err
		r.closeLocked()
		return nil, err
	}
	if r.closed {
		return nil, context.Canceled
	}
	if !r.started {
		r.started = true
		go r.run()
	}
	result := make(chan messageReceiveResult[T], 1)
	select {
	case r.requests <- result:
	case <-ctx.Done():
		r.terminal = ctx.Err()
		r.closeLocked()
		return nil, r.terminal
	}
	select {
	case v := <-result:
		if err := ctx.Err(); err != nil {
			r.terminal = err
			r.closeLocked()
			return nil, err
		}
		if v.err != nil {
			r.terminal = v.err
			r.closeLocked()
		}
		return v.message, v.err
	case <-ctx.Done():
		r.terminal = ctx.Err()
		r.closeLocked()
		return nil, r.terminal
	}
}
