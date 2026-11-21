package node

import (
	"context"
	"errors"
	"io"
	"net"
	"net/netip"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"tailscale.com/client/local"
	"tailscale.com/client/tailscale/apitype"
	"tailscale.com/tailcfg"
	"tailscale.com/types/key"
	"tailscale.com/wgengine"
)

func TestWhoIsAdmissionExactIdentityAndNoProxyPort(t *testing.T) {
	for _, text := range []string{"100.64.0.2", "fd7a:115c:a1e0::2"} {
		for _, change := range []string{"unchanged", "removed", "replaced", "expired", "self", "subnet", "wrong address", "nil response", "nil node", "api error"} {
			t.Run(text+"/"+change, func(t *testing.T) {
				target := netip.MustParseAddr(text)
				expected := key.NewNode().Public()
				response := &apitype.WhoIsResponse{Node: &tailcfg.Node{Key: expected, Addresses: []netip.Prefix{netip.PrefixFrom(target, target.BitLen())}}}
				var lookupErr error
				self4, self6 := netip.MustParseAddr("100.64.0.1"), netip.MustParseAddr("fd7a:115c:a1e0::1")
				calls := 0
				lookup := func(ctx context.Context, address string) (*apitype.WhoIsResponse, error) {
					calls++
					if address != target.String() {
						t.Fatalf("WhoIs must receive the exact bare IP, never a proxy port: %q", address)
					}
					deadline, ok := ctx.Deadline()
					if !ok || time.Until(deadline) > 5*time.Second {
						t.Fatal("unbounded WhoIs request")
					}
					return response, lookupErr
				}
				admission := peerAdmission{target: target, expected: expected, lookup: func(ip netip.Addr) (wgengine.PeerForIP, bool, error) {
					return lookupExactPeer(t.Context(), lookup, ip, self4, self6)
				}}
				if err := admission.check(); err != nil {
					t.Fatal(err)
				}
				switch change {
				case "removed":
					lookupErr = local.ErrPeerNotFound
				case "replaced":
					response.Node.Key = key.NewNode().Public()
				case "expired":
					response.Node.Expired = true
				case "self":
					if target.Is4() {
						self4 = target
					} else {
						self6 = target
					}
				case "subnet":
					response.Node.Addresses = []netip.Prefix{netip.PrefixFrom(target, target.BitLen()-1)}
				case "wrong address":
					response.Node.Addresses = []netip.Prefix{netip.PrefixFrom(target.Next(), target.BitLen())}
				case "nil response":
					response = nil
				case "nil node":
					response.Node = nil
				case "api error":
					lookupErr = errors.New("private-response-body-must-not-escape")
				}
				conn, remote := net.Pipe()
				defer conn.Close()
				defer remote.Close()
				accepted, err := admission.accept(conn)
				if change == "unchanged" {
					if err != nil || accepted != conn || calls != 2 {
						t.Fatalf("fresh exact peer rejected: %v", err)
					}
					return
				}
				want := connect.CodePermissionDenied
				if change == "api error" {
					want = connect.CodeUnavailable
				}
				if accepted != nil || connect.CodeOf(err) != want {
					t.Fatalf("changed peer accepted: %v %v", accepted, err)
				}
				if strings.Contains(err.Error(), "private-response-body") {
					t.Fatal("private lookup response escaped")
				}
				if change == "self" && calls != 1 {
					t.Fatal("self address reached WhoIs")
				}
				_ = remote.SetReadDeadline(time.Now().Add(time.Second))
				if _, err := remote.Read(make([]byte, 1)); err != io.EOF {
					t.Fatalf("rejected successful handshake not closed: %v", err)
				}
			})
		}
	}
}

func TestWhoIsAdmissionCancellationAndEarlierDeadline(t *testing.T) {
	target := netip.MustParseAddr("100.64.0.2")
	expected := key.NewNode().Public()
	for _, phase := range []string{"before", "during", "after", "deadline"} {
		t.Run(phase, func(t *testing.T) {
			timeout := 5 * time.Second
			if phase == "deadline" {
				timeout = 30 * time.Millisecond
			}
			ctx, cancel := context.WithTimeout(t.Context(), timeout)
			defer cancel()
			parentDeadline, _ := ctx.Deadline()
			if phase == "before" {
				cancel()
			}
			calls := 0
			lookup := func(call context.Context, _ string) (*apitype.WhoIsResponse, error) {
				calls++
				deadline, _ := call.Deadline()
				if deadline.After(parentDeadline) {
					t.Fatal("WhoIs exceeded caller deadline")
				}
				if phase == "during" || phase == "after" {
					cancel()
				}
				if phase != "after" {
					<-call.Done()
					return nil, call.Err()
				}
				return &apitype.WhoIsResponse{Node: &tailcfg.Node{Key: expected, Addresses: []netip.Prefix{netip.PrefixFrom(target, 32)}}}, nil
			}
			admission := peerAdmission{target: target, expected: expected, lookup: func(ip netip.Addr) (wgengine.PeerForIP, bool, error) {
				return lookupExactPeer(ctx, lookup, ip, netip.Addr{}, netip.Addr{})
			}}
			conn, remote := net.Pipe()
			defer conn.Close()
			defer remote.Close()
			accepted, err := admission.accept(conn)
			want := context.Canceled
			if phase == "deadline" {
				want = context.DeadlineExceeded
			}
			if accepted != nil || !errors.Is(err, want) {
				t.Fatalf("context failure lost: %v", err)
			}
			if phase == "before" && calls != 0 {
				t.Fatal("canceled lookup was started")
			}
			_ = remote.SetReadDeadline(time.Now().Add(time.Second))
			if _, err := remote.Read(make([]byte, 1)); err != io.EOF {
				t.Fatal("canceled handshake connection leaked", err)
			}
		})
	}
}
