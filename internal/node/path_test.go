package node

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"

	"tailscale.com/client/local"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/types/key"
)

// Exercise the real LocalAPI client and status decoding: a configured DERP
// region or an old direct address alone does not prove the current route.
func TestConnectionPathRequiresActiveAuthorizedPeer(t *testing.T) {
	for _, tc := range []struct {
		name, backend, target, want string
		peer                        ipnstate.PeerStatus
		selfExpired, unavailable    bool
	}{
		{name: "idle home DERP", peer: ipnstate.PeerStatus{Relay: "test"}},
		{name: "idle old direct", peer: ipnstate.PeerStatus{CurAddr: "127.0.0.1:3456", Relay: "test"}},
		{name: "active direct", peer: ipnstate.PeerStatus{Active: true, CurAddr: "127.0.0.1:3456", Relay: "test"}, want: "tailscale-direct"},
		{name: "active DERP", peer: ipnstate.PeerStatus{Active: true, Relay: "test"}, want: "tailscale-relay"},
		{name: "active peer relay", peer: ipnstate.PeerStatus{Active: true, PeerRelay: "127.0.0.1:3456:vni:1"}, want: "tailscale-relay"},
		{name: "active no observed route", peer: ipnstate.PeerStatus{Active: true}},
		{name: "expired peer", peer: ipnstate.PeerStatus{Active: true, Expired: true, Relay: "test"}},
		{name: "expired self", selfExpired: true, peer: ipnstate.PeerStatus{Active: true, Relay: "test"}},
		{name: "backend logged out", backend: "NeedsLogin", peer: ipnstate.PeerStatus{Active: true, Relay: "test"}},
		{name: "unknown target", target: "absent:9527", peer: ipnstate.PeerStatus{Active: true, Relay: "test"}},
		{name: "malformed target", target: "no-port", peer: ipnstate.PeerStatus{Active: true, Relay: "test"}},
		{name: "status unavailable", unavailable: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.want == "" {
				tc.want = "unknown"
			}
			if tc.backend == "" {
				tc.backend = "Running"
			}
			if tc.target == "" {
				tc.target = "peer:9527"
			}
			tc.peer.HostName = "peer"
			tc.peer.TailscaleIPs = []netip.Addr{netip.MustParseAddr("100.64.0.2")}
			status := &ipnstate.Status{BackendState: tc.backend, Self: &ipnstate.PeerStatus{Expired: tc.selfExpired}, Peer: map[key.NodePublic]*ipnstate.PeerStatus{key.NewNode().Public(): &tc.peer}}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/localapi/v0/status" {
					http.NotFound(w, r)
					return
				}
				if tc.unavailable {
					http.Error(w, "unavailable", http.StatusServiceUnavailable)
					return
				}
				if err := json.NewEncoder(w).Encode(status); err != nil {
					t.Error(err)
				}
			}))
			defer server.Close()
			transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "tcp", server.Listener.Addr().String())
			}}
			defer transport.CloseIdleConnections()
			node := &Node{local: &local.Client{OmitAuth: true, Transport: transport}}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if got := node.ConnectionPath(ctx, tc.target); got != tc.want {
				t.Fatalf("ConnectionPath = %q, want %q", got, tc.want)
			}
		})
	}
}
