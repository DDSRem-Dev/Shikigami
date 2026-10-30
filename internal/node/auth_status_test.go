package node

import (
	"net/netip"
	"testing"

	agentpb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/types/key"
)

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
