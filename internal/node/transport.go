package node

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/net/tsaddr"
)

// Dial accepts known tailnet peers only and resolves against the authenticated
// peer map, never ordinary DNS. The pinned SDK adapter dials directly through
// userspace WireGuard and cannot fall back to the machine's system network.
func (n *Node) Dial(ctx context.Context, network, address string) (net.Conn, error) {
	if network != "tcp" {
		return nil, errors.New("only tailnet TCP is supported")
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, errors.New("tailnet endpoint requires host:port")
	}
	number, err := strconv.ParseUint(port, 10, 16)
	if err != nil || number == 0 {
		return nil, errors.New("invalid tailnet endpoint port")
	}
	n.mu.Lock()
	server, lc := n.server, n.local
	generation := n.generation
	lease := n.authorization
	n.mu.Unlock()
	if server == nil || lc == nil || lease == nil || lease.Err() != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, "NODE_AUTH_REQUIRED")
	}
	status, err := n.queryStatus(ctx, lc, generation)
	if err != nil {
		if cause := ctx.Err(); cause != nil {
			return nil, connection.RebuildError(cause)
		}
		return nil, connect.NewError(connect.CodeUnavailable, "NODE_STATUS_UNAVAILABLE")
	}
	if status.BackendState != "Running" || (status.Self != nil && status.Self.Expired) {
		return nil, connect.NewError(connect.CodeFailedPrecondition, "NODE_AUTH_REQUIRED")
	}
	peer, ip, err := resolvePeer(status, host)
	if err != nil {
		return nil, err
	}
	if peer.Expired {
		return nil, connect.NewError(connect.CodePermissionDenied, "PEER_KEY_EXPIRED")
	}
	n.mu.Lock()
	identity := n.peers[ip]
	n.mu.Unlock()
	if identity.key != peer.PublicKey {
		return nil, connect.NewError(connect.CodePermissionDenied, "UNKNOWN_TAILNET_PEER")
	}
	conn, err := dialUserspaceTCP(ctx, server, netip.AddrPortFrom(ip, uint16(number)), peer.PublicKey)
	if err != nil {
		return nil, err
	}
	n.mu.Lock()
	current := n.generation == generation && n.server == server && n.authorizedLocked() && n.authorization == lease
	n.mu.Unlock()
	if !current || lease.Err() != nil {
		_ = conn.Close()
		return nil, connect.NewError(connect.CodeFailedPrecondition, "NODE_AUTH_REQUIRED")
	}
	return n.bindPeerConn(conn, generation, ip, identity)
}

func resolvePeer(status *ipnstate.Status, host string) (*ipnstate.PeerStatus, netip.Addr, error) {
	normalized := strings.TrimSuffix(strings.ToLower(host), ".")
	literal, parseErr := netip.ParseAddr(normalized)
	if parseErr == nil && (!tsaddr.IsTailscaleIP(literal) || literal.Is4In6()) {
		return nil, netip.Addr{}, connect.NewError(connect.CodePermissionDenied, "NON_TAILNET_TARGET")
	}
	var match *ipnstate.PeerStatus
	var address netip.Addr
	for _, peer := range status.Peer {
		matches := false
		for _, ip := range peer.TailscaleIPs {
			if parseErr == nil && literal == ip {
				matches = true
				address = ip
			}
		}
		dnsName := strings.TrimSuffix(strings.ToLower(peer.DNSName), ".")
		if parseErr != nil && (normalized == dnsName || normalized == strings.ToLower(peer.HostName) || normalized == strings.SplitN(dnsName, ".", 2)[0]) {
			matches = true
		}
		if !matches {
			continue
		}
		if match != nil && match != peer {
			return nil, netip.Addr{}, connect.NewError(connect.CodeInvalidArgument, "AMBIGUOUS_TAILNET_TARGET")
		}
		match = peer
		if !address.IsValid() {
			for _, ip := range peer.TailscaleIPs {
				if tsaddr.IsTailscaleIP(ip) {
					address = ip
					break
				}
			}
		}
	}
	if match == nil || !address.IsValid() {
		return nil, netip.Addr{}, connect.NewError(connect.CodePermissionDenied, "UNKNOWN_TAILNET_PEER")
	}
	return match, address, nil
}
func (n *Node) Listen(port uint16) (net.Listener, error) {
	if port == 0 {
		return nil, errors.New("tailnet listener requires a fixed port")
	}
	n.mu.Lock()
	server, authorized := n.server, n.authorizedLocked()
	generation := n.generation
	n.mu.Unlock()
	if server == nil || !authorized {
		return nil, connect.NewError(connect.CodeFailedPrecondition, "NODE_AUTH_REQUIRED")
	}
	return n.listenPeerTCP(server, generation, port)
}

// RPCPool owns a fixed peer's HTTP/2 connections. It never follows redirects or
// consults environment proxies. Close idle connections after canceling streams.
type RPCPool struct {
	Client    *connect.Client
	transport *http.Transport
}

func (p *RPCPool) Close() { p.transport.CloseIdleConnections() }
func (n *Node) NewRPCPool(address, token string) (*RPCPool, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil || host == "" || strings.ContainsAny(host, "/@?#\\\t\r\n ") {
		return nil, errors.New("invalid tailnet endpoint")
	}
	if number, err := strconv.ParseUint(port, 10, 16); err != nil || number == 0 {
		return nil, errors.New("invalid tailnet port")
	}
	bearer, err := connection.BearerToken(token)
	if err != nil {
		return nil, err
	}
	protocols := new(http.Protocols)
	protocols.SetUnencryptedHTTP2(true)
	transport := &http.Transport{Protocols: protocols, Proxy: nil, IdleConnTimeout: 90 * time.Second, DialContext: func(ctx context.Context, network, target string) (net.Conn, error) {
		if network != "tcp" || target != address {
			return nil, errors.New("unexpected tailnet destination")
		}
		return n.Dial(ctx, network, address)
	}}
	origin := "http://" + address
	client := &http.Client{Transport: peerOrigin{transport: transport, authority: address}, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("RPC redirects are disabled") }}
	// Use the same bounded-message and cancellation adapter as TLS/UDS clients.
	endpoint := &connection.Endpoint{HTTPClient: client, BaseURL: origin}
	return &RPCPool{Client: connection.NewRPCClient(endpoint, bearer, n.authorizeRPC), transport: transport}, nil
}

type peerOrigin struct {
	transport http.RoundTripper
	authority string
}

func (p peerOrigin) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.Scheme != "http" || request.URL.Host != p.authority || request.URL.User != nil || request.Host != p.authority {
		return nil, errors.New("RPC target differs from trusted peer")
	}
	return p.transport.RoundTrip(request)
}
func (n *Node) ConnectionPath(ctx context.Context, address string) string {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return "unknown"
	}
	n.mu.Lock()
	lc := n.local
	n.mu.Unlock()
	if lc == nil {
		return "unknown"
	}
	status, err := lc.Status(ctx)
	if err != nil || status.BackendState != "Running" || (status.Self != nil && status.Self.Expired) {
		return "unknown"
	}
	peer, _, err := resolvePeer(status, host)
	if err != nil || peer.Expired || !peer.Active {
		return "unknown"
	}
	// Relay is the configured home DERP region even for idle peers. Match the
	// pinned SDK's status semantics: only an active peer has an observed path.
	if peer.CurAddr != "" {
		return "tailscale-direct"
	}
	if peer.Relay != "" || peer.PeerRelay != "" {
		return "tailscale-relay"
	}
	return "unknown"
}
