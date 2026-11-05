package server

import (
	"context"
	"testing"

	"connectrpc.com/connect/v2"
)

// Model the legal nil -> DeadlineExceeded transition between observations.
// This context never reports Canceled, so admission must not invent that cause.
type admissionDeadlineTransition struct {
	context.Context
	expired bool
}

func (c *admissionDeadlineTransition) Err() error {
	if !c.expired {
		c.expired = true
		return nil
	}
	return context.DeadlineExceeded
}

func TestAdmissionDeadlineTransitionNeverBecomesCancellation(t *testing.T) {
	for _, prefix := range []string{"FILE", "PORT"} {
		t.Run(prefix, func(t *testing.T) {
			ctx := &admissionDeadlineTransition{Context: t.Context()}
			// Either side of the transition is valid; Canceled is not.
			if err := admissionContextError(ctx, prefix); err != nil && connect.CodeOf(err) != connect.CodeDeadlineExceeded {
				t.Fatalf("deadline transition invented cancellation: %v", err)
			}
			if err := admissionContextError(ctx, prefix); connect.CodeOf(err) != connect.CodeDeadlineExceeded {
				t.Fatalf("observed deadline lost its cause: %v", err)
			}
		})
	}
}
