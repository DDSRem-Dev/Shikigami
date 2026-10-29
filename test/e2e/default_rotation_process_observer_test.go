//go:build e2e

package e2e

import (
	"context"
	"time"
)

type rotationProcessObservation struct {
	LastPresent, FirstAbsent time.Time
	MaximumSamplingGap       time.Duration
	Error                    string
}
type rotationProcessObserver struct {
	done   chan struct{}
	result rotationProcessObservation
	cancel context.CancelFunc
}

// Expiry-window sampling is independent of slower RPC/status probes. A missing
// observation after the conservative earliest expiry is inconclusive, not a
// pass; the harness never converts sampling resolution into an exact timestamp.
func startRotationProcessObserver(parent context.Context, identity rotationProcessIdentity) *rotationProcessObserver {
	return startRotationProcessObserverWithProbe(parent, identity, rotationObservedAlive)
}
func startRotationProcessObserverWithProbe(parent context.Context, identity rotationProcessIdentity, probe func(context.Context, rotationProcessIdentity) (bool, error)) *rotationProcessObserver {
	ctx, cancel := context.WithCancel(parent)
	o := &rotationProcessObserver{done: make(chan struct{}), cancel: cancel}
	go func() {
		defer close(o.done)
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()
		var previous time.Time
		for {
			began := time.Now()
			if !previous.IsZero() {
				o.result.MaximumSamplingGap = max(o.result.MaximumSamplingGap, began.Sub(previous))
			}
			previous = began
			alive, err := probe(ctx, identity)
			if err != nil {
				if ctx.Err() == nil {
					o.result.Error = err.Error()
				}
				return
			}
			if !alive {
				if ctx.Err() == nil {
					o.result.FirstAbsent = time.Now()
				}
				return
			}
			o.result.LastPresent = began
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return o
}
func (o *rotationProcessObserver) close() { o.cancel(); <-o.done }
