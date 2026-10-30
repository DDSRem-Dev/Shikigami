package node

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"tailscale.com/client/local"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/tsnet"
	"tailscale.com/types/key"
)

func peerRegistryFixture(t *testing.T) (*Node, *countedAuthorizationContext, netip.Addr, *ipnstate.Status) {
	t.Helper()
	node, lease := newCountedAuthorization(t)
	node.generation = 1
	node.authorizationCancel = lease.cancel
	node.status.Addresses = []string{"100.64.0.1"}
	node.selfAddresses = []string{"100.64.0.1"}
	address := netip.MustParseAddr("100.64.0.2")
	identity := key.NewNode().Public()
	status := &ipnstate.Status{BackendState: "Running", Peer: map[key.NodePublic]*ipnstate.PeerStatus{
		identity: {PublicKey: identity, InNetworkMap: true, TailscaleIPs: []netip.Addr{address}},
	}}
	node.applyPeerStatus(1, status)
	return node, lease, address, status
}

func assertRegistryEmpty(t *testing.T, node *Node) {
	t.Helper()
	node.mu.Lock()
	defer node.mu.Unlock()
	if len(node.peerConnections) != 0 {
		t.Fatalf("retained %d peer connections", len(node.peerConnections))
	}
}

func assertPipeClosed(t *testing.T, conn net.Conn) {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := conn.Read(make([]byte, 1)); err != io.EOF {
		t.Fatalf("retired connection remains open: %v", err)
	}
}

func TestPeerLifetimeIdentityAndCleanup(t *testing.T) {
	for _, change := range []string{"removed", "replaced", "expired", "historical-engine-entry", "unchanged", "old-generation"} {
		t.Run(change, func(t *testing.T) {
			node, lease, address, status := peerRegistryFixture(t)
			identity := node.peers[address]
			conn, remote := net.Pipe()
			defer remote.Close()
			bound, err := node.bindPeerConn(conn, 1, address, identity)
			if err != nil {
				t.Fatal(err)
			}
			defer bound.Close()
			lease.awaitCount(t, 1)
			generation := uint64(1)
			preserved := false
			switch change {
			case "removed":
				status.Peer = nil
			case "replaced":
				status.Peer[identity.key].PublicKey = key.NewNode().Public()
			case "expired":
				status.Peer[identity.key].Expired = true
			case "historical-engine-entry":
				status.Peer[identity.key].InNetworkMap = false
			case "unchanged":
				preserved = true
				status.Health = []string{"control temporarily unreachable"}
			case "old-generation":
				preserved = true
				generation = 0
				status.Peer = nil
			}
			node.applyPeerStatus(generation, status)
			if preserved {
				select {
				case <-bound.(*peerConn).done:
					t.Fatal("unrelated or stale observation revoked a peer")
				default:
				}
			} else {
				assertPipeClosed(t, remote)
				assertRegistryEmpty(t, node)
				lease.awaitCount(t, 0)
			}
			_ = bound.Close()
			assertRegistryEmpty(t, node)
			lease.awaitCount(t, 0)
		})
	}
}

func TestPeerRegistrationRejectsRetiredIncarnation(t *testing.T) {
	node, lease, address, status := peerRegistryFixture(t)
	old := node.peers[address]
	node.applyPeerStatus(1, &ipnstate.Status{})
	node.applyPeerStatus(1, status)
	conn, remote := net.Pipe()
	defer remote.Close()
	if _, err := node.bindPeerConn(conn, 1, address, old); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("old handshake joined re-added identity: %v", err)
	}
	assertPipeClosed(t, remote)
	assertRegistryEmpty(t, node)
	lease.awaitCount(t, 0)
}

func TestPeerRegistryShutdownClosesAndReleasesSynchronously(t *testing.T) {
	node, lease, address, _ := peerRegistryFixture(t)
	var remotes []net.Conn
	for range 16 {
		conn, remote := net.Pipe()
		defer remote.Close()
		remotes = append(remotes, remote)
		if _, err := node.bindPeerConn(conn, 1, address, node.peers[address]); err != nil {
			t.Fatal(err)
		}
	}
	node.stopLocked()
	assertRegistryEmpty(t, node)
	lease.awaitCount(t, 0)
	for _, remote := range remotes {
		assertPipeClosed(t, remote)
	}
}

func TestSDKListenerAdmissionQueueAndLateFlow(t *testing.T) {
	node, lease, address, status := peerRegistryFixture(t)
	node.server = &tsnet.Server{}
	listener, err := node.listenPeerTCP(node.server, 1, 9527)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	l := listener.(*sdkPeerListener)
	src := netip.AddrPortFrom(address, 12345)
	dst := netip.MustParseAddrPort("100.64.0.1:9527")
	for _, target := range []netip.AddrPort{netip.MustParseAddrPort("127.0.0.1:9527"), netip.MustParseAddrPort("100.64.0.99:9527")} {
		if handler, intercept := l.flow(src, target); handler != nil || !intercept {
			t.Fatal("listener accepted a non-self destination")
		}
	}
	if handler, intercept := l.flow(src, netip.MustParseAddrPort("100.64.0.1:9528")); handler != nil || intercept {
		t.Fatal("listener claimed an unrelated port")
	}
	stale, _ := l.flow(src, dst)
	node.applyPeerStatus(1, &ipnstate.Status{})
	node.applyPeerStatus(1, status)
	conn, remote := net.Pipe()
	defer remote.Close()
	stale(conn)
	assertPipeClosed(t, remote)
	assertRegistryEmpty(t, node)
	var remotes []net.Conn
	for range peerAcceptQueueLimit + 1 {
		handler, intercept := l.flow(src, dst)
		if handler == nil || !intercept {
			t.Fatal("authorized flow rejected")
		}
		conn, remote := net.Pipe()
		defer remote.Close()
		remotes = append(remotes, remote)
		handler(conn)
	}
	if len(l.queue) != peerAcceptQueueLimit {
		t.Fatal("listener queue capacity changed")
	}
	assertPipeClosed(t, remotes[len(remotes)-1])
	late, _ := l.flow(src, dst)
	var closing sync.WaitGroup
	for range 8 {
		closing.Go(func() { _ = listener.Close() })
	}
	closing.Wait()
	assertRegistryEmpty(t, node)
	lease.awaitCount(t, 0)
	for _, remote := range remotes {
		assertPipeClosed(t, remote)
	}
	conn, remote = net.Pipe()
	defer remote.Close()
	late(conn)
	assertPipeClosed(t, remote)
	if _, err := listener.Accept(); err != net.ErrClosed {
		t.Fatalf("closed listener accepted: %v", err)
	}
	if node.peerListeners[9527] != nil {
		t.Fatal("closed listener retained its port")
	}
}

func TestStatusPermitWaitIsCancelable(t *testing.T) {
	node := &Node{statusPermit: make(chan struct{}, 1)}
	node.statusPermit <- struct{}{}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := node.queryStatus(ctx, nil, 0); err != context.Canceled {
		t.Fatalf("status permit ignored cancellation: %v", err)
	}
}

type peerStatusTransport func(*http.Request) (*http.Response, error)

func (f peerStatusTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestPeerObservationFailurePreservesOtherPeer(t *testing.T) {
	node, lease, address, status := peerRegistryFixture(t)
	secondAddress := address.Next()
	secondKey := key.NewNode().Public()
	status.Peer[secondKey] = &ipnstate.PeerStatus{PublicKey: secondKey, InNetworkMap: true, TailscaleIPs: []netip.Addr{secondAddress}}
	node.applyPeerStatus(1, status)
	var connections []*peerConn
	var remotes []net.Conn
	for _, address := range []netip.Addr{address, secondAddress} {
		conn, remote := net.Pipe()
		defer remote.Close()
		bound, err := node.bindPeerConn(conn, 1, address, node.peers[address])
		if err != nil {
			t.Fatal(err)
		}
		defer bound.Close()
		connections = append(connections, bound.(*peerConn))
		remotes = append(remotes, remote)
	}
	lc := &local.Client{OmitAuth: true, Transport: peerStatusTransport(func(request *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusServiceUnavailable, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("unavailable")), Request: request}, nil
	})}
	if _, err := node.queryStatus(t.Context(), lc, 1); err == nil {
		t.Fatal("status observation unexpectedly succeeded")
	}
	for _, conn := range connections {
		select {
		case <-conn.done:
			t.Fatal("status observation failure revoked a working peer")
		default:
		}
	}
	lease.awaitCount(t, 2)
	delete(status.Peer, connections[0].identity.key)
	node.applyPeerStatus(1, status)
	assertPipeClosed(t, remotes[0])
	select {
	case <-connections[1].done:
		t.Fatal("one removed peer canceled an unrelated peer")
	default:
	}
	lease.awaitCount(t, 1)
	_ = connections[1].Close()
	lease.awaitCount(t, 0)
	assertRegistryEmpty(t, node)
}

func TestStatusObservationHasOwnDeadlineAndReleasesPermit(t *testing.T) {
	node := &Node{}
	caller, cancel := context.WithCancel(t.Context())
	defer cancel()
	lc := &local.Client{OmitAuth: true, Transport: peerStatusTransport(func(request *http.Request) (*http.Response, error) {
		<-request.Context().Done()
		return nil, request.Context().Err()
	})}
	done := make(chan error, 1)
	go func() { _, err := node.queryStatus(caller, lc, 0); done <- err }()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) || caller.Err() != nil {
			t.Fatalf("observation did not enforce its own deadline: %v", err)
		}
	case <-time.After(6 * time.Second):
		cancel()
		<-done
		t.Fatal("deadline-free caller monopolized the status observation permit")
	}
	if len(node.statusPermit) != 0 {
		t.Fatal("timed-out observation retained its permit")
	}
}
