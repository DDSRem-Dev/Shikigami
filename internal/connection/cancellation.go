package connection

import (
	"context"
	"io"
	"sync"

	"connectrpc.com/connect/v2"
)

// A canceled HTTP/2 request can leave its request-body io.Pipe writer blocked
// while another goroutine waits for a response. Explicitly closing both stream
// halves wakes those operations; canceling only its context is insufficient for
// every duplex proxy topology in the pinned Connect/Go versions.
func closeOnCancellation(next connect.ClientFunc) connect.ClientFunc {
	return func(ctx context.Context, spec connect.Spec) (connect.ClientStream, error) {
		transportCtx, cancel := context.WithCancel(ctx)
		stream, err := next(transportCtx, spec)
		if err != nil {
			cancel()
			return nil, err
		}
		// Client/bidi bodies use a pipe: initialize headers before cancellation
		// can close that pipe, then allow Close concurrently with blocked Send.
		// Unary/server-stream requests attach their body during Send and need
		// serialization instead; their HTTP request is interrupted by ctx.
		clientStreaming := spec.StreamType&connect.StreamTypeClient != 0
		if clientStreaming {
			if err := stream.SendHeaders(); err != nil {
				cancel()
				_ = stream.Close()
				return nil, err
			}
		}
		wrapped := &cancelStream{ClientStream: stream, ctx: ctx, cancel: cancel, clientStreaming: clientStreaming, singleResponse: spec.StreamType == connect.StreamTypeClient}
		wrapped.stop = context.AfterFunc(ctx, wrapped.close)
		return wrapped, nil
	}
}

type cancelStream struct {
	connect.ClientStream
	ctx             context.Context
	once            sync.Once
	stop            func() bool
	err             error
	clientStreaming bool
	// A pure client stream consumes its response and terminal status in one
	// Receive. Bidi/server streams must keep their lifetime until EOF/error.
	singleResponse bool
	cancel         context.CancelFunc
	sendMu         sync.Mutex
	closed         bool
}

func (s *cancelStream) close() {
	s.once.Do(func() {
		s.cancel()
		if !s.clientStreaming {
			s.sendMu.Lock()
			defer s.sendMu.Unlock()
			s.closed = true
		}
		s.err = s.ClientStream.Close()
	})
}

func (s *cancelStream) sendOperation(fn func() error) error {
	if !s.clientStreaming {
		s.sendMu.Lock()
		defer s.sendMu.Unlock()
		if s.closed {
			if cause := s.ctx.Err(); cause != nil {
				return RebuildError(cause)
			}
			return io.EOF
		}
	}
	return fn()
}

func (s *cancelStream) Send(message any) error {
	return s.sendOperation(func() error { return s.ClientStream.Send(message) })
}
func (s *cancelStream) SendHeaders() error {
	return s.sendOperation(s.ClientStream.SendHeaders)
}
func (s *cancelStream) CloseSend() error {
	return s.sendOperation(s.ClientStream.CloseSend)
}
func (s *cancelStream) Close() error { s.stop(); s.close(); return s.err }
func (s *cancelStream) Receive(message any) error {
	err := s.ClientStream.Receive(message)
	if err != nil || s.singleResponse {
		_ = s.Close()
	}
	if cause := s.ctx.Err(); cause != nil {
		return RebuildError(cause)
	}
	return err
}
