package node

import (
	"io"
	"net"
	"net/netip"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"tailscale.com/tailcfg"
	"tailscale.com/types/key"
	"tailscale.com/wgengine"
)

// Model a handshake that already succeeded using an old data-plane route.
// A current peer-map removal, key replacement or expiry must still close it.
func TestPeerAdmissionRejectsStaleSuccessfulHandshake(t *testing.T) {
	for _, address := range []string{"100.64.0.2", "fd7a:115c:a1e0::2"} {
		for _, change := range []string{"removed", "replaced", "expired", "self", "subnet", "wrong address", "invalid node"} {
			t.Run(address+"/"+change, func(t *testing.T) {
				target := netip.MustParseAddr(address)
				expected := key.NewNode().Public()
				node := &tailcfg.Node{Key: expected}
				peer := wgengine.PeerForIP{Node: node.View(), Route: netip.PrefixFrom(target, target.BitLen())}
				present := true
				admission := peerAdmission{target: target, expected: expected, lookup: func(ip netip.Addr) (wgengine.PeerForIP, bool) {
					if ip != target {
						t.Fatalf("lookup target changed: %v", ip)
					}
					return peer, present
				}}
				if err := admission.check(); err != nil {
					t.Fatal(err)
				}
				switch change {
				case "removed":
					present = false
				case "replaced":
					peer.Node = (&tailcfg.Node{Key: key.NewNode().Public()}).View()
				case "expired":
					peer.Node = (&tailcfg.Node{Key: expected, Expired: true}).View()
				case "self":
					peer.IsSelf = true
				case "subnet":
					peer.Route = netip.PrefixFrom(target, target.BitLen()-1)
				case "wrong address":
					peer.Route = netip.PrefixFrom(target.Next(), target.BitLen())
				case "invalid node":
					peer.Node = tailcfg.NodeView{}
				}
				if err := admission.check(); connect.CodeOf(err) != connect.CodePermissionDenied {
					t.Fatalf("stale peer admitted before dialing: %v", err)
				}
				conn, remote := net.Pipe()
				defer remote.Close()
				defer conn.Close()
				got, err := admission.accept(conn)
				if got != nil || connect.CodeOf(err) != connect.CodePermissionDenied {
					t.Fatalf("stale handshake admitted: %v, %v", got, err)
				}
				_ = remote.SetReadDeadline(time.Now().Add(time.Second))
				if _, err := remote.Read(make([]byte, 1)); err != io.EOF {
					t.Fatalf("stale connection was not closed: %v", err)
				}
			})
		}
	}
}
