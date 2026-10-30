package agent

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/node"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
)

func TestLocalNodeAuthorizationWaitDoesNotPoisonRemoteRecovery(t *testing.T) {
	// Exercise the actual node RPC admission path, before any socket or remote
	// negotiation exists, as on asynchronous agent startup.
	unready := &node.Node{}
	nodePool, err := unready.NewRPCPool("100.64.0.1:9527", "shiki_route_fixture")
	if err != nil {
		t.Fatal(err)
	}
	defer nodePool.Close()
	_, blocked := rpc.NewShikiDaemonClient(nodePool.Client).NegotiateProtocol(t.Context(), remoteProtocolOffer())
	if connect.CodeOf(blocked) != connect.CodeFailedPrecondition {
		t.Fatalf("node admission: %v", blocked)
	}
	var calls int
	good := routeEndpoint(t, func(ctx context.Context) (*pb.ProtocolAgreement, error) {
		calls++
		return routeAgreement(ctx)
	})
	r := &Resources{}
	server := &apb.ServerConfig{TailscaleAddress: "100.64.0.1:9527", DirectTlsAddress: good.DirectTlsAddress, DaemonId: "daemon_route"}
	_, routes, err := establishRemote(t.Context(), server, func(route string) (*connect.Client, func(), error) {
		if route != "tsnet" {
			t.Fatal("local authorization wait bypassed through TLS")
		}
		return nodePool.Client, func() {}, nil
	})
	if err == nil || !reflect.DeepEqual(routes, []string{"tsnet"}) || calls != 0 {
		t.Fatal("unready node contacted remote or used fallback", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	pool := &remotePool{ctx: ctx, cancel: cancel}
	pool.failed(blocked)
	if !pool.bad.Load() || pool.failure.Load() == nil {
		t.Fatal("old node authorization failure did not retire the old route")
	}
	r.failRemote(pool, err)
	if pool.failure.Load() != nil {
		t.Fatal("retired admission error would prevent a fresh recovery probe")
	}
	if pool.state != "auth_required" || pool.reason != "NODE_AUTH_REQUIRED" || pool.episode.stopped || pool.episode.exhausted || pool.episode.attempts != 0 {
		t.Fatalf("local wait became terminal remote failure: state=%s reason=%s episode=%+v", pool.state, pool.reason, pool.episode)
	}
	// Local authorization may require user interaction longer than the network
	// retry budget. Rechecking it neither registers a node nor spends that budget.
	for range 10 {
		pool.episode.failed(time.Now(), blocked)
	}
	if pool.episode.attempts != 0 || pool.episode.stopped || !pool.maintenanceDue(time.Now().Add(2*time.Second)) {
		t.Fatal("node wait exhausted or permanently stopped recovery")
	}
	// Model the now-authorized route with a real HTTP/2 endpoint. The same
	// route selector must perform authenticated negotiation and pinned-ID check.
	candidate, routes, err := establishRemote(t.Context(), server, func(string) (*connect.Client, func(), error) {
		return r.newTrustedRoute(good, "shiki_route_fixture", "direct-tls")
	})
	if err != nil || candidate == nil {
		t.Fatal("ready route did not recover", err)
	}
	defer candidate.close()
	if calls != 1 || candidate.daemonID != server.DaemonId || candidate.agreement == nil || !reflect.DeepEqual(routes, []string{"tsnet"}) {
		t.Fatal("recovery omitted credential/protocol/identity verification")
	}
}

func TestRemoteNodeAuthTextStillStopsRecoveryAndFallback(t *testing.T) {
	// A remote peer cannot impersonate the local node's authorization signal.
	remote := routeEndpoint(t, func(context.Context) (*pb.ProtocolAgreement, error) {
		return nil, connect.NewError(connect.CodeFailedPrecondition, "NODE_AUTH_REQUIRED")
	})
	r := &Resources{}
	_, routes, err := establishRemote(t.Context(), &apb.ServerConfig{TailscaleAddress: "node:9527", DirectTlsAddress: remote.DirectTlsAddress}, func(route string) (*connect.Client, func(), error) {
		if route != "tsnet" {
			t.Fatal("remote rejection used fallback")
		}
		return r.newTrustedRoute(remote, "shiki_route_fixture", "direct-tls")
	})
	var ce *connect.Error
	if !errors.As(err, &ce) || !ce.IsRemote() || !reflect.DeepEqual(routes, []string{"tsnet"}) {
		t.Fatal("missing real remote rejection", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	pool := &remotePool{ctx: ctx, cancel: cancel}
	r.failRemote(pool, err)
	if pool.state != "incompatible" || !pool.episode.stopped || pool.maintenanceDue(time.Now().Add(time.Hour)) {
		t.Fatal("remote rejection became recoverable")
	}
}
