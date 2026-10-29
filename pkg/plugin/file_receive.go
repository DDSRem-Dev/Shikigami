package plugin

import (
	"context"
	"errors"
	"io"
	"sync"

	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
)

type fileReceiveResult struct {
	message *pb.WriteFileRequest
	err     error
}

// fileReceiver owns one transport reader. Cancellation is terminal: no new
// Recv can start after a canceled Receive. Returning from the gRPC handler
// cancels its transport context and releases an in-flight Recv; close does not
// claim to have joined that transport operation before the handler returns.
type fileReceiver struct {
	mu       sync.Mutex
	recv     func() (*pb.WriteFileRequest, error)
	requests chan chan fileReceiveResult
	stop     chan struct{}
	terminal error
	started  bool
	closed   bool
}

func newFileReceiver(recv func() (*pb.WriteFileRequest, error)) *fileReceiver {
	return &fileReceiver{recv: recv, requests: make(chan chan fileReceiveResult), stop: make(chan struct{})}
}

func (r *fileReceiver) run() {
	for {
		select {
		case <-r.stop:
			return
		case result := <-r.requests:
			message, err := r.recv()
			result <- fileReceiveResult{message, err}
		}
	}
}

func (r *fileReceiver) closeLocked() {
	if !r.closed {
		r.closed = true
		close(r.stop)
	}
}

func (r *fileReceiver) close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closeLocked()
}

func (r *fileReceiver) failure() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if errors.Is(r.terminal, io.EOF) {
		return nil
	}
	return r.terminal
}

func (r *fileReceiver) receive(ctx context.Context) (*pb.WriteFileRequest, error) {
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
	result := make(chan fileReceiveResult, 1)
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
