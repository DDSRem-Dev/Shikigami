package rotation_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
)

type streamCounts struct {
	active, started, canceled atomic.Int64
	mu                        sync.Mutex
	ends                      []*streamEnd
}

type streamEnd struct {
	Kind                                      string
	Started, Deadline, Returned, Canceled     time.Time
	ContextAtReturn, ContextError, ReturnCode string
	CancellationResult                        bool
}

func (s *streamCounts) track(ctx context.Context, kind string) func(error) {
	s.started.Add(1)
	s.active.Add(1)
	deadline, _ := ctx.Deadline()
	end := &streamEnd{Kind: kind, Started: time.Now(), Deadline: deadline}
	s.mu.Lock()
	s.ends = append(s.ends, end)
	s.mu.Unlock()
	// Observe actual context cancellation independently of handler return. The
	// interceptor's deferred cleanup may run after our inner handler's defer.
	context.AfterFunc(ctx, func() {
		s.mu.Lock()
		end.Canceled = time.Now()
		end.ContextError = ctx.Err().Error()
		s.mu.Unlock()
		s.canceled.Add(1)
	})
	return func(err error) {
		s.mu.Lock()
		end.Returned = time.Now()
		if ctx.Err() != nil {
			end.ContextAtReturn = ctx.Err().Error()
		}
		if err != nil {
			end.ReturnCode = connect.CodeOf(err).String()
		}
		end.CancellationResult = errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || connect.CodeOf(err) == connect.CodeCanceled || connect.CodeOf(err) == connect.CodeDeadlineExceeded
		s.mu.Unlock()
		s.active.Add(-1)
	}
}

func (s *streamCounts) terminalStates() []streamEnd {
	s.mu.Lock()
	defer s.mu.Unlock()
	states := make([]streamEnd, len(s.ends))
	for i, end := range s.ends {
		states[i] = *end
	}
	return states
}

func (s *streamCounts) expiryEvidence(notBefore time.Time) error {
	if s.started.Load() == 0 || s.active.Load() != 0 || s.canceled.Load() != s.started.Load() {
		return errors.New("callbacks have not all returned and actually canceled")
	}
	for _, end := range s.terminalStates() {
		if !end.Started.Before(notBefore) || end.Returned.Before(notBefore) || end.Canceled.Before(notBefore) {
			return fmt.Errorf("%s did not remain live across expiry", end.Kind)
		}
		// A normal stream completion followed by request cleanup is not expiry
		// evidence, even if its context is later canceled by an outer defer.
		if end.ContextAtReturn == "" && !end.CancellationResult {
			return fmt.Errorf("%s returned without a cancellation result", end.Kind)
		}
	}
	return nil
}

func TestStreamObserverRecordsCancellationAfterReturn(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var counts streamCounts
	finish := counts.track(ctx, "events")
	notBefore := time.Now()
	finish(context.Canceled)
	if counts.active.Load() != 0 || counts.canceled.Load() != 0 {
		t.Fatal("return must not invent context cancellation")
	}
	cancel()
	waitCtx, stop := context.WithTimeout(t.Context(), time.Second)
	defer stop()
	wait(t, waitCtx, "actual cancellation after handler return", func() bool { return counts.canceled.Load() == 1 })
	if err := counts.expiryEvidence(notBefore); err != nil {
		t.Fatal(err)
	}
}

func TestStreamObserverRejectsEarlyReturnAndOrdinaryRequestCleanup(t *testing.T) {
	for _, mode := range []string{"early canceled return", "ordinary return"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var counts streamCounts
			finish := counts.track(ctx, "files")
			notBefore := time.Now()
			if mode == "early canceled return" {
				finish(context.Canceled)
				notBefore = time.Now().Add(time.Nanosecond)
			} else {
				finish(nil)
			}
			cancel()
			waitCtx, stop := context.WithTimeout(t.Context(), time.Second)
			defer stop()
			wait(t, waitCtx, "request cleanup cancellation", func() bool { return counts.canceled.Load() == 1 })
			if err := counts.expiryEvidence(notBefore); err == nil {
				t.Fatal("handler return followed by request cleanup falsely proved expiry")
			}
		})
	}
}
