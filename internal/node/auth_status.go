package node

import (
	"time"

	"tailscale.com/ipn/ipnstate"
)

// nodeKeyExpired reconciles the pinned SDK's two self-expiry signals. Its
// internal keyExpired state moves the backend to NeedsLogin, but Status copies
// Self.Expired only from the control node: that flag can remain false after the
// SDK has enforced its own KeyExpiry. Require both the SDK state transition and
// its known identity deadline; a timestamp or lost control poll alone is not
// evidence of expiry. Status rounds KeyExpiry to whole seconds, so wait an extra
// second before interpreting it. A backward clock step conservatively delays
// this classification; it cannot restore the authorization retired by NeedsLogin.
func nodeKeyExpired(observed *ipnstate.Status, now time.Time) bool {
	self := observed.Self
	if self == nil {
		return false
	}
	if self.Expired {
		return true
	}
	return observed.BackendState == "NeedsLogin" && !self.PublicKey.IsZero() &&
		self.KeyExpiry != nil && !self.KeyExpiry.IsZero() && now.After(self.KeyExpiry.Add(time.Second))
}
