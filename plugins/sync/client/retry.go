package client

import (
	"connectrpc.com/connect/v2"
	"context"
	"time"
)

// Retry whole reconciliation, never a captured write. Every retry rereads the
// current revisions, so an uncertain commit is reconciled against the baseline.
func (t *task) resetRetry() { t.failures = 0; t.retryAt = time.Time{}; t.retryBlocked = false }
func (t *task) recordRetry(ctx context.Context, err error) {
	if err == nil {
		t.resetRetry()
		return
	}
	switch connect.CodeOf(err) {
	case connect.CodeUnavailable, connect.CodeDeadlineExceeded:
		if t.failures >= 5 {
			t.retryBlocked = true
			t.data.Reason = "REMOTE_RETRY_EXHAUSTED"
			if t.persist() == nil {
				t.publish(ctx)
			}
			return
		}
		t.retryAt = time.Now().Add(retryDelay(t.failures))
		t.failures++
	case connect.CodeUnauthenticated, connect.CodePermissionDenied:
		t.retryBlocked = true
	default:
		t.resetRetry()
	}
}
