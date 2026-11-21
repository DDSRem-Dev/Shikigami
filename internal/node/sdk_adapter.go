package node

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"time"

	"connectrpc.com/connect/v2"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"tailscale.com/client/local"
	"tailscale.com/client/tailscale/apitype"
	"tailscale.com/net/tsaddr"
	"tailscale.com/tsnet"
	"tailscale.com/types/key"
	"tailscale.com/wgengine"
)

// pinnedNetstack is the public subsystem API of tailscale.com v1.104.2. Sys()
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
	client, err := server.LocalClient()
	if err != nil {
		return nil, connect.NewError(connect.CodeUnavailable, "NODE_STATUS_UNAVAILABLE")
	}
	admission := peerAdmission{target: target.Addr(), expected: expected, lookup: func(ip netip.Addr) (wgengine.PeerForIP, bool, error) {
		self4, self6 := server.TailscaleIPs()
		return lookupExactPeer(ctx, client.WhoIs, ip, self4, self6)
	}}
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
	lookup   func(netip.Addr) (wgengine.PeerForIP, bool, error)
}

func (a peerAdmission) check() error {
	peer, ok, err := a.lookup(a.target)
	if err != nil {
		return err
	}
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

// WhoIs with a bare IP uses the SDK's live node address index. Port zero cannot
// enter the proxy-mapper fallback. Require the original exact address prefix;
// a subnet/service route must never become an application peer identity.
func lookupExactPeer(parent context.Context, whois func(context.Context, string) (*apitype.WhoIsResponse, error), target, self4, self6 netip.Addr) (wgengine.PeerForIP, bool, error) {
	var absent wgengine.PeerForIP
	if err := parent.Err(); err != nil {
		return absent, false, err
	}
	if !tsaddr.IsTailscaleIP(target) || target.Is4In6() || target == self4 || target == self6 {
		return absent, false, nil
	}
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	response, err := whois(ctx, target.String())
	if cause := ctx.Err(); cause != nil {
		return absent, false, cause
	}
	if errors.Is(err, local.ErrPeerNotFound) {
		return absent, false, nil
	}
	if err != nil {
		return absent, false, connect.NewError(connect.CodeUnavailable, "NODE_STATUS_UNAVAILABLE")
	}
	if response == nil || response.Node == nil {
		return absent, false, nil
	}
	for _, prefix := range response.Node.Addresses {
		if prefix.IsSingleIP() && prefix.Addr() == target {
			return wgengine.PeerForIP{Node: response.Node.View(), Route: prefix}, true, nil
		}
	}
	return absent, false, nil
}
