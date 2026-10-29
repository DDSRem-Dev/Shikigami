package client

import (
	"math"
	"time"
)

// fileTransferTimeout allows 30 seconds for setup/commit plus one second per
// started 64 KiB. Thus the default 10 MiB limit has a 190 second budget, allowing
// slow links without letting a silent peer hold the serial sync loop forever.
// Callers reject negative sizes. Integer division before rounding and saturated
// duration arithmetic keep every nonnegative int64 size finite and monotonic.
// WithTimeout also preserves any earlier deadline on the caller's context.
func fileTransferTimeout(size int64) time.Duration {
	const base = 30 * time.Second
	const chunk = int64(64 << 10)
	units := size / chunk
	if size%chunk != 0 {
		units++
	}
	if units > int64((time.Duration(math.MaxInt64)-base)/time.Second) {
		return time.Duration(math.MaxInt64)
	}
	return base + time.Duration(units)*time.Second
}
