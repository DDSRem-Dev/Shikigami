package agent

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"
)

// Delay the public AfterFunc hook while Done/Err already expose cancellation.
// This forces handler cleanup to stop the callback before it can set a deadline.
type deferredFrontendCancellation struct {
	context.Context
	mu       sync.Mutex
	done     chan struct{}
	err      error
	callback func()
	stopped  bool
}

func (c *deferredFrontendCancellation) Done() <-chan struct{} { return c.done }
func (c *deferredFrontendCancellation) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}
func (c *deferredFrontendCancellation) AfterFunc(callback func()) func() bool {
	c.mu.Lock()
	c.callback = callback
	c.mu.Unlock()
	return func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.stopped = true
		return true
	}
}
func (c *deferredFrontendCancellation) cancelWithoutCallback() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.err = context.Canceled
	close(c.done)
}

type frontendDeadlineWriter struct {
	deadlines []time.Time
}

func (*frontendDeadlineWriter) Header() http.Header         { return http.Header{} }
func (*frontendDeadlineWriter) WriteHeader(int)             {}
func (*frontendDeadlineWriter) Write(p []byte) (int, error) { return len(p), nil }
func (w *frontendDeadlineWriter) SetWriteDeadline(deadline time.Time) error {
	w.deadlines = append(w.deadlines, deadline)
	return nil
}

func TestFrontendCancellationStoppedCallbackStillBoundsFinalStatus(t *testing.T) {
	ctx := &deferredFrontendCancellation{Context: context.Background(), done: make(chan struct{})}
	writer := &frontendDeadlineWriter{}
	output := &frontendOutput{controller: http.NewResponseController(writer)}
	stop := bindFrontendCancellation(ctx, output)
	if ctx.callback == nil {
		t.Fatal("fixture did not register the delayed cancellation callback")
	}
	ctx.cancelWithoutCallback()
	if len(writer.deadlines) != 0 {
		t.Fatal("fixture ran callback instead of delaying it")
	}
	before := time.Now()
	stop()
	if !ctx.stopped || len(writer.deadlines) != 1 {
		t.Fatal("stopped cancellation callback left final status unbounded")
	}
	if writer.deadlines[0].Before(before) || writer.deadlines[0].After(time.Now().Add(frontendFinalStatusTimeout)) {
		t.Fatal("final status deadline is outside the cancellation budget")
	}
	output.cancel() // A concurrently started callback must not extend the budget.
	if len(writer.deadlines) != 1 {
		t.Fatal("duplicate cancellation extended the write deadline")
	}
	output.finish()
	output.cancel()
	if len(writer.deadlines) != 1 {
		t.Fatal("late callback touched a finished HTTP response")
	}
}

func TestFrontendNormalCompletionDoesNotAbortFinalStatus(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	writer := &frontendDeadlineWriter{}
	output := &frontendOutput{controller: http.NewResponseController(writer)}
	stop := bindFrontendCancellation(ctx, output)
	stop()
	cancel() // Match Registry.done, which runs after interceptor cleanup.
	output.finish()
	if len(writer.deadlines) != 0 {
		t.Fatal("normal completion aborted its final status")
	}
}
