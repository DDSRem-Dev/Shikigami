package connection

import (
	"context"
	"math/rand/v2"
	"time"
)

// RetryConnect retries connection establishment only: one initial attempt plus
// at most five reconnects. Each attempt must include authentication and protocol
// negotiation and honor its context. Callers must never pass a business write.
func RetryConnect(ctx context.Context, attempt func(context.Context) error) error {
	return retryConnect(ctx, attempt, RetryDelay)
}

// RetryDelay is shared by synchronous establishment and the agent's scheduled
// reconnect episodes. n is the zero-based reconnect index, capped before shift.
func RetryDelay(n int) time.Duration {
	base := min(time.Second*time.Duration(1<<min(max(n, 0), 5)), 30*time.Second)
	return min(time.Duration(float64(base)*(0.8+rand.Float64()*0.4)), 30*time.Second)
}

func retryConnect(ctx context.Context, attempt func(context.Context) error, delay func(int) time.Duration) error {
	for n := 0; ; n++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		probe, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := attempt(probe)
		cancel()
		if err == nil || n >= 5 || !IsRetryableNetworkError(err) {
			return err
		}
		timer := time.NewTimer(delay(n))
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
