package connection

import (
	"context"
	"errors"
	"io"
	"sync/atomic"
	"time"

	"connectrpc.com/connect/v2"
)

// FinalSendError handles admission failures that close the request writer before
// Send can expose their status. Read that status once, with no replay and a short
// cleanup deadline. A successful/empty response cannot bless a failed send.
// Cancellation and non-EOF transport failures do not start another read.
func FinalSendError(ctx context.Context, cancel context.CancelFunc, sendErr error, receive func() error) error {
	if ctx.Err() != nil {
		return RebuildError(ctx.Err())
	}
	if !errors.Is(sendErr, io.EOF) {
		return RebuildError(sendErr)
	}
	var expired atomic.Bool
	timer := time.AfterFunc(time.Second, func() { expired.Store(true); cancel() })
	defer timer.Stop()
	final := receive()
	if expired.Load() {
		return connect.NewError(connect.CodeDeadlineExceeded, "downstream terminal status timed out")
	}
	if final != nil && !errors.Is(final, io.EOF) {
		return RebuildError(final)
	}
	// Do not preserve io.EOF as a cause: callers use errors.Is(EOF) to recognize
	// clean stream completion, which a failed request must never resemble.
	return connect.NewError(connect.CodeUnavailable, "downstream request closed before completion")
}
