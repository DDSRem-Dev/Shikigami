package connection

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
)

func TestTerminalSendErrorBoundedAndNeverPromotesFailedSend(t *testing.T) {
	t.Run("unresponsive-final-status", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		start := time.Now()
		err := FinalSendError(ctx, cancel, io.EOF, func() error { <-ctx.Done(); return ctx.Err() })
		if connect.CodeOf(err) != connect.CodeDeadlineExceeded || time.Since(start) > 3*time.Second {
			t.Fatal("terminal status wait did not obey independent deadline", err, time.Since(start))
		}
	})
	for _, final := range []error{nil, io.EOF} {
		ctx, cancel := context.WithCancel(t.Context())
		err := FinalSendError(ctx, cancel, io.EOF, func() error { return final })
		cancel()
		if connect.CodeOf(err) != connect.CodeUnavailable || errors.Is(err, io.EOF) {
			t.Fatal("failed send promoted to successful completion", err)
		}
	}
	for _, canceled := range []bool{false, true} {
		ctx, cancel := context.WithCancel(t.Context())
		sendErr := errors.New("hard transport failure")
		if canceled {
			cancel()
			sendErr = io.EOF
		}
		err := FinalSendError(ctx, cancel, sendErr, func() error { t.Error("unexpected response read after cancellation/hard failure"); return nil })
		cancel()
		if err == nil || canceled && connect.CodeOf(err) != connect.CodeCanceled {
			t.Fatal("failed transport/cancellation lost", err)
		}
	}
}
