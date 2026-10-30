package node

import (
	"context"
	"net"
	"net/netip"
	"sync"

	"connectrpc.com/connect/v2"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/net/tsaddr"
	"tailscale.com/types/key"
)

type peerIdentity struct {
	key     key.NodePublic
	expired bool
	epoch   uint64
}

type peerMap map[netip.Addr]peerIdentity

// Only authoritative SDK peer-map observations retire a peer lifetime. Health changes and
// control-plane outages retain the last known map and working peer channels.
// LocalAPI Status includes the live peer map; the pinned SDK's initial NetMap
// notification can contain stale peers after optimized delta updates.
func (n *Node) applyPeerStatus(generation uint64, observed *ipnstate.Status) {
	peers := make(peerMap)
	for _, peer := range observed.Peer {
		if peer == nil || !peer.InNetworkMap || peer.PublicKey.IsZero() {
			continue
		}
		identity := peerIdentity{key: peer.PublicKey, expired: peer.Expired}
		for _, address := range peer.TailscaleIPs {
			if !tsaddr.IsTailscaleIP(address) || address.Is4In6() {
				continue
			}
			if _, duplicate := peers[address]; duplicate {
				// Ambiguous identities never authorize an existing or new socket.
				peers[address] = peerIdentity{}
			} else {
				peers[address] = identity
			}
		}
	}
	n.mu.Lock()
	if generation != n.generation {
		n.mu.Unlock()
		return
	}
	n.peerRevision++
	for address, peer := range peers {
		previous := n.peers[address]
		peer.epoch = n.peerRevision
		if !peer.key.IsZero() && peer.key == previous.key && !peer.expired && !previous.expired {
			peer.epoch = previous.epoch
		}
		peers[address] = peer
	}
	n.peers = peers
	var retired []*peerConn
	for conn := range n.peerConnections {
		peer := peers[conn.address]
		if conn.generation != generation || peer.key != conn.identity.key || peer.epoch != conn.identity.epoch || peer.expired {
			retired = append(retired, conn)
		}
	}
	n.mu.Unlock()
	for _, conn := range retired {
		_ = conn.Close()
	}
}

// Registration and map publication share the node mutex: a removal either
// rejects this registration or sees and closes the registered socket. Binding
// the immutable key prevents an address reassignment from reviving old streams.
func (n *Node) bindPeerConn(conn net.Conn, generation uint64, address netip.Addr, identity peerIdentity) (net.Conn, error) {
	n.mu.Lock()
	var err error
	lease := n.authorization
	peer := n.peers[address]
	switch {
	case generation != n.generation || !n.authorizedLocked():
		err = connect.NewError(connect.CodeFailedPrecondition, "NODE_AUTH_REQUIRED")
	case identity.key.IsZero() || peer.key != identity.key || peer.epoch != identity.epoch:
		err = connect.NewError(connect.CodePermissionDenied, "UNKNOWN_TAILNET_PEER")
	case peer.expired:
		err = connect.NewError(connect.CodePermissionDenied, "PEER_KEY_EXPIRED")
	}
	if err != nil {
		n.mu.Unlock()
		_ = conn.Close()
		return nil, err
	}
	bound := &peerConn{Conn: conn, node: n, generation: generation, address: address, identity: identity, done: make(chan struct{})}
	if n.peerConnections == nil {
		n.peerConnections = make(map[*peerConn]struct{})
	}
	n.peerConnections[bound] = struct{}{}
	bound.stop = context.AfterFunc(lease, func() { _ = bound.Close() })
	n.mu.Unlock()
	return bound, nil
}

type peerConn struct {
	net.Conn
	node       *Node
	generation uint64
	address    netip.Addr
	identity   peerIdentity
	stop       func() bool
	done       chan struct{}
	once       sync.Once
	err        error
}

func (c *peerConn) Close() error {
	c.once.Do(func() {
		c.node.mu.Lock()
		delete(c.node.peerConnections, c)
		stop := c.stop
		c.node.mu.Unlock()
		stop()
		c.err = c.Conn.Close()
		close(c.done)
	})
	return c.err
}
