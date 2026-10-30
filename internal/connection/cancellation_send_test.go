package connection

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
)

func TestClosedSingleRequestStreamPreservesCancellation(t *testing.T) {
	for _, kind := range []struct {
		name string
		kind connect.StreamType
	}{
		{"unary", connect.StreamTypeUnary},
		{"server-stream", connect.StreamTypeServer},
	} {
		for _, state := range []string{"canceled", "deadline", "explicit-close"} {
			t.Run(kind.name+"/"+state, func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				want := connect.CodeCanceled
				if state == "deadline" {
					var deadlineCancel context.CancelFunc
					ctx, deadlineCancel = context.WithDeadline(ctx, time.Now().Add(-time.Second))
					defer deadlineCancel()
					want = connect.CodeDeadlineExceeded
				} else if state == "canceled" {
					cancel()
				}
				probe := &cancellationProbe{entered: make(chan struct{}), closed: make(chan struct{})}
				stream, err := closeOnCancellation(func(ctx context.Context, _ connect.Spec) (connect.ClientStream, error) {
					probe.ctx = ctx
					return probe, nil
				})(ctx, connect.Spec{StreamType: kind.kind})
				if err != nil {
					t.Fatal(err)
				}
				// Join close deterministically: a pre-canceled context can run its
				// AfterFunc before CallUnary reaches its first Send. No timing or
				// scheduler assumption is needed to exercise that ordering here.
				if err := stream.Close(); err != nil {
					t.Fatal(err)
				}
				for _, operation := range []struct {
					name string
					call func() error
				}{
					{"Send", func() error { return stream.Send(nil) }},
					{"SendHeaders", stream.SendHeaders},
					{"CloseSend", stream.CloseSend},
				} {
					err := operation.call()
					if state == "explicit-close" {
						if !errors.Is(err, io.EOF) {
							t.Errorf("%s after explicit close: want EOF, got %v", operation.name, err)
						}
					} else if connect.CodeOf(err) != want || errors.Is(err, io.EOF) {
						t.Errorf("%s after %s: want %v without EOF, got %v", operation.name, state, want, err)
					}
				}
				if probe.closes.Load() != 1 {
					t.Fatal("stream closed repeatedly")
				}
			})
		}
	}
}
