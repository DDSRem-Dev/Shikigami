package agent

import (
	"context"
	"errors"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	"github.com/DDSRem-Dev/Shikigami/internal/node"
)

func localNodeAuthorizationRequired(err error) bool {
	var remote *connect.Error
	return errors.Is(err, node.ErrAuthorizationRequired) && !(errors.As(err, &remote) && remote.IsRemote())
}

type remoteFailure struct{ err error }

// One episode contains the initial connection plus at most five reconnects.
// It is runtime state shared by foreground RPCs, plugins and maintenance, so a
// periodic worker cannot create a new budget. An explicit server/credential
// update retires the pool and starts a new episode.
type reconnectEpisode struct {
	attempts  int
	retryAt   time.Time
	lastError error
	stopped   bool
	exhausted bool
}

func (e *reconnectEpisode) allowed(now time.Time) error {
	if e.exhausted {
		return rpcError(connect.CodeUnavailable, "RECONNECT_EXHAUSTED")
	}
	if e.stopped {
		return e.lastError
	}
	if now.Before(e.retryAt) {
		return rpcError(connect.CodeUnavailable, "RECONNECT_COOLDOWN")
	}
	return nil
}

func (e *reconnectEpisode) failed(now time.Time, err error) {
	e.lastError = err
	if localNodeAuthorizationRequired(err) {
		// Waiting for our own node is not a failed remote protocol or a network
		// reconnect. Recheck local admission without consuming the shared budget;
		// no registration, credential change or route fallback is initiated.
		e.retryAt = now.Add(time.Second)
		return
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		// A caller abandoning its request is neither a remote trust failure nor
		// permission to burn another caller's entire reconnect budget.
		e.retryAt = now.Add(time.Second)
		return
	}
	if !connection.IsRetryableNetworkError(err) {
		e.stopped = true
		return
	}
	e.attempts++
	if e.attempts >= 6 {
		e.exhausted = true
		return
	}
	e.retryAt = now.Add(connection.RetryDelay(e.attempts - 1))
}

func (p *remotePool) maintenanceDue(now time.Time) bool {
	if p.client != nil {
		return p.bad.Load() || !now.Before(p.nextCheck)
	}
	return p.episode.allowed(now) == nil
}

func (r *Resources) maintain() {
	defer close(r.done)
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-r.ctx.Done():
			return
		case <-ticker.C:
		case <-r.wake:
		}
		snapshot, err := r.store.View()
		if err != nil {
			continue
		}
		for id := range snapshot.Buckets["servers"] {
			if r.ctx.Err() != nil {
				return
			}
			r.mu.Lock()
			p := r.pools[id]
			due := p == nil || p.maintenanceDue(time.Now())
			var suspend error
			if p != nil {
				if p.needsSuspend {
					suspend = p.episode.lastError
					p.needsSuspend = false
				}
			}
			r.mu.Unlock()
			if suspend != nil {
				r.suspendForwards(r.ctx, id, suspend)
			}
			if !due {
				continue
			}
			// Admission and the due check are atomic with reserving the pool's
			// probe. A foreground call may have refreshed it since the scan.
			if client, _, err := r.resolveSnapshot(r.ctx, id, true); err == nil {
				if client != nil {
					r.reconcileForwards(r.ctx, id)
					r.reconcileAutomaticForwards(r.ctx, id)
				}
			} else {
				r.suspendForwards(r.ctx, id, err)
			}
		}
	}
}
