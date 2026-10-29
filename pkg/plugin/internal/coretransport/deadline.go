package coretransport

import (
	"context"
	"time"

	"connectrpc.com/connect/v2"
)

const unaryTimeout = 5 * time.Second

// CoreAPI calls made by background workers do not pass through the frontend's
// RPC budgets. Bound short unary operations here, leaving file and long-lived
// streams to their caller's operation-specific context. WithTimeout preserves
// any earlier parent deadline.
func unaryDeadline(next connect.ClientFunc) connect.ClientFunc {
	return func(ctx context.Context, spec connect.Spec) (connect.ClientStream, error) {
		if spec.StreamType != connect.StreamTypeUnary {
			return next(ctx, spec)
		}
		ctx, cancel := context.WithTimeout(ctx, unaryTimeout)
		stream, err := next(ctx, spec)
		if err != nil {
			cancel()
			return nil, err
		}
		return &unaryDeadlineStream{ClientStream: stream, cancel: cancel}, nil
	}
}

type unaryDeadlineStream struct {
	connect.ClientStream
	cancel context.CancelFunc
}

func (s *unaryDeadlineStream) Send(message any) error {
	err := s.ClientStream.Send(message)
	if err != nil {
		s.cancel()
	}
	return err
}
func (s *unaryDeadlineStream) SendHeaders() error {
	err := s.ClientStream.SendHeaders()
	if err != nil {
		s.cancel()
	}
	return err
}
func (s *unaryDeadlineStream) CloseSend() error {
	err := s.ClientStream.CloseSend()
	if err != nil {
		s.cancel()
	}
	return err
}
func (s *unaryDeadlineStream) Receive(message any) error {
	// Connect's unary Receive reads the full response and terminal EOF before
	// returning. Keep the timer until then, and never replace a remote error
	// with the cancellation used only to release our own timer.
	err := s.ClientStream.Receive(message)
	s.cancel()
	return err
}
func (s *unaryDeadlineStream) Close() error {
	defer s.cancel()
	return s.ClientStream.Close()
}
