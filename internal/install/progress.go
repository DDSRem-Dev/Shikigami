package install

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"
)

type progressOutputKey struct{}

// WithProgress enables human-readable progress on a separate terminal/stderr
// stream. Structured command results remain unchanged. No command output,
// arguments, credentials or error strings are sent to this stream.
func WithProgress(ctx context.Context, output io.Writer) context.Context {
	if output == nil {
		return ctx
	}
	return context.WithValue(ctx, progressOutputKey{}, output)
}

// ProgressStep reports a noninteractive operation using a fixed, nonsecret
// description. Callers must never wrap a password prompt or other user input.
// Its heartbeat is joined before returning, including on failure/cancellation.
func ProgressStep(ctx context.Context, description string, action func() error) error {
	output, _ := ctx.Value(progressOutputKey{}).(io.Writer)
	if output == nil {
		return action()
	}
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	return progressStep(ctx, output, description, ticker.C, action)
}

func progressStep(ctx context.Context, output io.Writer, description string, ticks <-chan time.Time, action func() error) error {
	started := time.Now()
	fmt.Fprintf(output, "[shiki] %s…\n", description)
	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			case <-ctx.Done():
				return
			case <-ticks:
				if ctx.Err() != nil {
					return
				}
				fmt.Fprintf(output, "[shiki] %s — still working (%s elapsed). No input needed; Ctrl+C cancels.\n", description, time.Since(started).Round(time.Second))
			}
		}
	}()
	var once sync.Once
	finish := func() { once.Do(func() { close(stop); <-done }) }
	// Also join on panic, so a caller recovering cannot inherit a reporter.
	defer finish()
	err := action()
	finish()
	status := "done"
	if err != nil {
		status = "failed"
	}
	fmt.Fprintf(output, "[shiki] %s: %s (%s).\n", description, status, time.Since(started).Round(time.Second))
	return err
}
