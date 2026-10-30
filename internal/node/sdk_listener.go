package node

import (
	"errors"
	"net"
	"net/netip"
	"strconv"
	"sync"

	"connectrpc.com/connect/v2"
	"tailscale.com/tsnet"
)

const peerAcceptQueueLimit = 32

// The pinned SDK's listener queues connections without their peer identity.
// Capture our peer incarnation at its flow callback instead, before a queued
// connection can be mistaken for a later owner of the same IP address. This
// relies on the SDK's WireGuard flow and live peer-map observations; it is not
// a cryptographic socket identity or an atomic control-plane transaction.
type sdkPeerListener struct {
	node       *Node
	generation uint64
	port       uint16
	queue      chan *peerConn
	done       chan struct{}
	registered chan struct{}
	mu         sync.Mutex
	closed     bool
	unregister func()
	active     sync.WaitGroup
	once       sync.Once
}

func (n *Node) listenPeerTCP(server *tsnet.Server, generation uint64, port uint16) (net.Listener, error) {
	listener := &sdkPeerListener{node: n, generation: generation, port: port, queue: make(chan *peerConn, peerAcceptQueueLimit), done: make(chan struct{}), registered: make(chan struct{})}
	n.mu.Lock()
	if n.generation != generation || n.server != server || !n.authorizedLocked() {
		n.mu.Unlock()
		return nil, authorizationError(connect.CodeFailedPrecondition)
	}
	if n.peerListeners[port] != nil {
		n.mu.Unlock()
		return nil, errors.New("tailnet port already in use")
	}
	if n.peerListeners == nil {
		n.peerListeners = make(map[uint16]*sdkPeerListener)
	}
	n.peerListeners[port] = listener
	n.mu.Unlock()
	// The SDK invokes flow while holding its mutex. Never call into the SDK
	// under node/listener locks, including registration and unregistration.
	listener.unregister = server.RegisterFallbackTCPHandler(listener.flow)
	close(listener.registered)
	listener.mu.Lock()
	closed := listener.closed
	listener.mu.Unlock()
	if closed {
		return nil, net.ErrClosed
	}
	return listener, nil
}

func (l *sdkPeerListener) flow(src, dst netip.AddrPort) (func(net.Conn), bool) {
	if dst.Port() != l.port {
		return nil, false
	}
	l.mu.Lock()
	closed := l.closed
	l.mu.Unlock()
	if closed {
		return nil, true
	}
	l.node.mu.Lock()
	identity := l.node.peers[src.Addr()]
	valid := l.node.generation == l.generation && l.node.authorizedLocked() && !identity.key.IsZero() && !identity.expired
	local := false
	for _, address := range l.node.selfAddresses {
		if ip, err := netip.ParseAddr(address); err == nil && ip == dst.Addr() {
			local = true
			break
		}
	}
	l.node.mu.Unlock()
	if !valid || !local {
		return nil, true
	}
	return func(conn net.Conn) { l.enqueue(conn, src.Addr(), identity) }, true
}

func (l *sdkPeerListener) enqueue(conn net.Conn, address netip.Addr, identity peerIdentity) {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		_ = conn.Close()
		return
	}
	l.active.Add(1)
	l.mu.Unlock()
	defer l.active.Done()
	bound, err := l.node.bindPeerConn(conn, l.generation, address, identity)
	if err != nil {
		return
	}
	peer := bound.(*peerConn)
	l.mu.Lock()
	queued := false
	if !l.closed {
		select {
		case l.queue <- peer:
			queued = true
		default:
		}
	}
	l.mu.Unlock()
	if !queued {
		_ = peer.Close()
	}
}

func (l *sdkPeerListener) Accept() (net.Conn, error) {
	for {
		select {
		case <-l.done:
			return nil, net.ErrClosed
		case conn := <-l.queue:
			l.mu.Lock()
			closed := l.closed
			l.mu.Unlock()
			if closed {
				_ = conn.Close()
				return nil, net.ErrClosed
			}
			select {
			case <-conn.done:
				continue
			default:
				return conn, nil
			}
		}
	}
}

func (l *sdkPeerListener) Close() error {
	l.once.Do(func() {
		l.mu.Lock()
		l.closed = true
		close(l.done)
		l.mu.Unlock()
		<-l.registered
		l.unregister()
		l.active.Wait()
		for {
			select {
			case conn := <-l.queue:
				_ = conn.Close()
			default:
				l.node.mu.Lock()
				if l.node.peerListeners[l.port] == l {
					delete(l.node.peerListeners, l.port)
				}
				l.node.mu.Unlock()
				return
			}
		}
	})
	return nil
}

func (l *sdkPeerListener) Addr() net.Addr { return peerListenAddr(l.port) }

type peerListenAddr uint16

func (peerListenAddr) Network() string  { return "tcp" }
func (a peerListenAddr) String() string { return ":" + strconv.Itoa(int(a)) }
