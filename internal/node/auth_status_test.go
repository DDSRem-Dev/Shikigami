package node

import (
	"net/netip"
	"testing"
	"time"

	agentpb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/types/key"
)

func TestSDKNeedsLoginWithElapsedSelfKeyExpiry(t *testing.T) {
	n, observed := authorizedStatusFixture(t)
	lease := n.authorization
	observed.BackendState = "NeedsLogin"
	deadline := time.Now().Add(-time.Minute)
	observed.Self.KeyExpiry = &deadline
	// Official SDK observation: Online remains true and Expired remains false
	// after its own key-expiry state machine has transitioned to NeedsLogin.
	n.applyStatus(0, observed)
	status := n.snapshot()
	if status.State != "expired" || status.ReasonCode != "NODE_KEY_EXPIRED" {
		t.Fatalf("SDK key expiry misclassified: state=%s reason=%s", status.State, status.ReasonCode)
	}
	if lease.Err() == nil || n.DataPlaneAuthorized() {
		t.Fatal("expired identity retained authorization")
	}
}

func TestSelfKeyExpiryRequiresSDKStateAndConservativeDeadline(t *testing.T) {
	now := time.Date(2026, time.October, 31, 1, 0, 0, 0, time.UTC)
	past, future, zero := now.Add(-time.Minute), now.Add(time.Minute), time.Time{}
	boundary := now.Add(-time.Second)
	pastBoundary := boundary.Add(-time.Nanosecond)
	for _, tc := range []struct {
		name, backend                 string
		deadline                      *time.Time
		noSelf, noKey, explicit, want bool
	}{
		{name: "SDK elapsed deadline", backend: "NeedsLogin", deadline: &past, want: true},
		{name: "Running past deadline", backend: "Running", deadline: &past},
		{name: "Stopped past deadline", backend: "Stopped", deadline: &past},
		{name: "approval past deadline", backend: "NeedsMachineAuth", deadline: &past},
		{name: "future deadline or backward clock", backend: "NeedsLogin", deadline: &future},
		{name: "no deadline", backend: "NeedsLogin"},
		{name: "zero deadline", backend: "NeedsLogin", deadline: &zero},
		{name: "no self", backend: "NeedsLogin", deadline: &past, noSelf: true},
		{name: "no self key", backend: "NeedsLogin", deadline: &past, noKey: true},
		{name: "rounded conservative boundary", backend: "NeedsLogin", deadline: &boundary},
		{name: "past conservative boundary", backend: "NeedsLogin", deadline: &pastBoundary, want: true},
		{name: "explicit overrides future", backend: "Starting", deadline: &future, explicit: true, want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			observed := &ipnstate.Status{BackendState: tc.backend, Self: &ipnstate.PeerStatus{PublicKey: key.NewNode().Public(), KeyExpiry: tc.deadline, Expired: tc.explicit}}
			if tc.noKey {
				observed.Self.PublicKey = key.NodePublic{}
			}
			if tc.noSelf {
				observed.Self = nil
			}
			if got := nodeKeyExpired(observed, now); got != tc.want {
				t.Fatalf("expired=%t, want %t", got, tc.want)
			}
		})
	}
}

func TestExpiredNodeRetainsActiveReauthenticationURL(t *testing.T) {
	n, observed := authorizedStatusFixture(t)
	n.attempt = &loginAttempt{id: "reauth", interactive: true, loginStarted: true, expires: time.Now().Add(time.Minute), done: make(chan struct{})}
	deadline := time.Now().Add(-time.Minute)
	observed.BackendState = "NeedsLogin"
	observed.Self.KeyExpiry = &deadline
	observed.AuthURL = "https://control.invalid/login/reauth"
	n.applyStatus(0, observed)
	status := n.snapshot()
	if status.State != "expired" || status.ReasonCode != "NODE_KEY_EXPIRED" || status.LoginUrl != observed.AuthURL || status.LoginExpiresAt == 0 || n.attempt.finished || n.DataPlaneAuthorized() {
		t.Fatal("expired reauthentication lost its URL, completed early or admitted business")
	}
}

func TestExplicitSelfExpiryAcrossBackendStates(t *testing.T) {
	// The pinned SDK can retain Self while changing Running to NeedsLogin.
	// Its explicit expiry signal must survive that transition and revoke the
	// old authorization lifetime regardless of the backend's coarse state.
	for _, backend := range []string{"Running", "NeedsLogin", "NeedsMachineAuth", "Starting", "Stopped", "NoState"} {
		t.Run(backend, func(t *testing.T) {
			n, observed := authorizedStatusFixture(t)
			lease := n.authorization
			observed.BackendState = backend
			observed.Self.Expired = true
			n.applyStatus(0, observed)
			status := n.snapshot()
			if status.State != "expired" || status.ReasonCode != "NODE_KEY_EXPIRED" {
				t.Fatalf("explicit expiry lost: state=%s reason=%s", status.State, status.ReasonCode)
			}
			if lease.Err() == nil || n.DataPlaneAuthorized() || len(status.Addresses) != 0 {
				t.Fatal("expired identity retained business authorization")
			}
		})
	}
}

func TestPendingAuthURLRetiresExistingAuthorization(t *testing.T) {
	n, observed := authorizedStatusFixture(t)
	lease := n.authorization
	// Control loss by itself preserves usable peer authorization.
	observed.Self.Online = false
	n.applyStatus(0, observed)
	if lease.Err() != nil || !n.DataPlaneAuthorized() {
		t.Fatal("control outage revoked usable identity")
	}
	observed.AuthURL = "https://control.invalid/login/test"
	n.applyStatus(0, observed)
	status := n.snapshot()
	if status.State != "needs_login" || status.ReasonCode != "NODE_AUTH_REQUIRED" {
		t.Fatalf("pending URL classification: state=%s reason=%s", status.State, status.ReasonCode)
	}
	if lease.Err() == nil || n.DataPlaneAuthorized() || len(n.selfAddresses) != 0 {
		t.Fatal("pending login retained previous business authorization")
	}
	// Removing the URL while still offline cannot grant fresh authorization.
	observed.AuthURL = ""
	n.applyStatus(0, observed)
	if n.DataPlaneAuthorized() {
		t.Fatal("cached identity revived retired authorization")
	}
	observed.Self.Online = true
	n.applyStatus(0, observed)
	if !n.DataPlaneAuthorized() || n.authorization == lease || lease.Err() == nil {
		t.Fatal("fresh login did not establish a distinct authorization lifetime")
	}
}

func authorizedStatusFixture(t *testing.T) (*Node, *ipnstate.Status) {
	t.Helper()
	n := &Node{ctx: t.Context(), status: &agentpb.NodeStatus{State: "uninitialized"}}
	observed := &ipnstate.Status{
		BackendState: "Running",
		Self:         &ipnstate.PeerStatus{PublicKey: key.NewNode().Public(), Online: true},
		TailscaleIPs: []netip.Addr{netip.MustParseAddr("100.64.0.1")},
	}
	n.applyStatus(0, observed)
	if !n.DataPlaneAuthorized() {
		t.Fatal("fresh SDK identity was not authorized")
	}
	return n, observed
}
