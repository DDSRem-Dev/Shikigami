package node

import (
	"context"
	"net"
	"net/netip"

	"connectrpc.com/connect/v2"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"tailscale.com/tsnet"
	"tailscale.com/types/key"
	"tailscale.com/wgengine"
)

// pinnedNetstack is the public subsystem API of tailscale.com v1.98.10. Sys()
// and its subsystem methods are explicitly unstable upstream: keep this adapter
// isolated and rerun the real-node tests whenever updating the SDK.
//
// Calling netstack directly is essential. Server.Dial -> UserDial can fall back
// to the system network when a peer route disappears. This interface returns
// only userspace TCP connections and has no route to UserDial/SystemDial.
type pinnedNetstack interface {
	DialContextTCPWithBind(context.Context, netip.Addr, netip.AddrPort) (*gonet.TCPConn, error)
}

func dialUserspaceTCP(ctx context.Context, server *tsnet.Server, target netip.AddrPort, expected key.NodePublic) (net.Conn, error) {
	if server == nil || server.Sys() == nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, "NODE_NETSTACK_UNAVAILABLE")
	}
	implementation, ok := server.Sys().Netstack.GetOK()
	if !ok {
		return nil, connect.NewError(connect.CodeFailedPrecondition, "NODE_NETSTACK_UNAVAILABLE")
	}
	stack, ok := implementation.(pinnedNetstack)
	if !ok {
		return nil, connect.NewError(connect.CodeFailedPrecondition, "NODE_NETSTACK_INCOMPATIBLE")
	}
	engine, ok := server.Sys().Engine.GetOK()
	if !ok {
		return nil, connect.NewError(connect.CodeFailedPrecondition, "NODE_NETSTACK_UNAVAILABLE")
	}
	admission := peerAdmission{target: target.Addr(), expected: expected, lookup: engine.PeerForIP}
	if err := admission.check(); err != nil {
		return nil, err
	}
	ip4, ip6 := server.TailscaleIPs()
	source := ip4
	if target.Addr().Is6() {
		source = ip6
	}
	if !source.IsValid() {
		return nil, connect.NewError(connect.CodeFailedPrecondition, "NODE_ADDRESS_UNAVAILABLE")
	}
	conn, err := stack.DialContextTCPWithBind(ctx, source, target)
	if err != nil {
		if denied := admission.check(); denied != nil {
			return nil, denied
		}
		return nil, err
	}
	return admission.accept(conn)
}

// SDK peer-map publication and data-plane route removal are separate steps.
// Bind both admission observations to the identity resolved by Node.Dial, so a
// stale userspace handshake cannot admit an already removed/replaced peer.
// These checks are not an atomic lease against arbitrary later peer changes.
type peerAdmission struct {
	target   netip.Addr
	expected key.NodePublic
	lookup   func(netip.Addr) (wgengine.PeerForIP, bool)
}

func (a peerAdmission) check() error {
	peer, ok := a.lookup(a.target)
	if !ok || !peer.Node.Valid() || peer.IsSelf || a.expected.IsZero() || peer.Node.Key() != a.expected || !peer.Route.IsSingleIP() || peer.Route.Addr() != a.target {
		return connect.NewError(connect.CodePermissionDenied, "UNKNOWN_TAILNET_PEER")
	}
	if peer.Node.Expired() {
		return connect.NewError(connect.CodePermissionDenied, "PEER_KEY_EXPIRED")
	}
	return nil
}

func (a peerAdmission) accept(conn net.Conn) (net.Conn, error) {
	if err := a.check(); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}
