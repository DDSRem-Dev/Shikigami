//go:build e2e

package e2e

import (
	"bufio"
	"context"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// upgradeProxy forwards opaque TLS bytes. The original client still verifies
// the daemon certificate; this fixture neither terminates TLS nor interprets
// RPC messages. Throttling makes a real partial file observable before pausing.
type upgradeProxy struct {
	listener net.Listener
	target   string
	slow     atomic.Bool
	mu       sync.Mutex
	gate     chan struct{}
	paused   bool
	peers    map[net.Conn]bool
	wg       sync.WaitGroup
	ctx      context.Context
	cancel   context.CancelFunc
}

func newUpgradeProxy(t *testing.T, parent context.Context, target string) *upgradeProxy {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(parent)
	p := &upgradeProxy{listener: listener, target: target, gate: make(chan struct{}), peers: map[net.Conn]bool{}, ctx: ctx, cancel: cancel}
	close(p.gate)
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		for {
			client, err := listener.Accept()
			if err != nil {
				return
			}
			p.wg.Add(1)
			go func() { defer p.wg.Done(); p.serve(client) }()
		}
	}()
	t.Cleanup(func() {
		cancel()
		listener.Close()
		p.pause(false)
		p.mu.Lock()
		for peer := range p.peers {
			peer.Close()
		}
		p.mu.Unlock()
		p.wg.Wait()
	})
	return p
}

func (p *upgradeProxy) pause(paused bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if paused && !p.paused {
		p.gate = make(chan struct{})
	} else if !paused && p.paused {
		close(p.gate)
	}
	p.paused = paused
}

func (p *upgradeProxy) serve(client net.Conn) {
	defer client.Close()
	upstream, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(p.ctx, "tcp4", p.target)
	if err != nil {
		return
	}
	defer upstream.Close()
	p.mu.Lock()
	p.peers[client], p.peers[upstream] = true, true
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		delete(p.peers, client)
		delete(p.peers, upstream)
		p.mu.Unlock()
	}()
	ctx, cancel := context.WithCancel(p.ctx)
	defer cancel()
	stop := context.AfterFunc(ctx, func() { client.Close(); upstream.Close() })
	defer stop()
	done := make(chan struct{})
	go func() {
		defer close(done)
		p.copy(ctx, upstream, client)
		cancel()
		client.Close()
		upstream.Close()
	}()
	p.copy(ctx, client, upstream)
	cancel()
	client.Close()
	upstream.Close()
	<-done
}

func (p *upgradeProxy) copy(ctx context.Context, dst, src net.Conn) {
	buffer := make([]byte, 32<<10)
	for {
		n, err := src.Read(buffer)
		if n > 0 {
			p.mu.Lock()
			gate := p.gate
			p.mu.Unlock()
			select {
			case <-ctx.Done():
				return
			case <-gate:
			}
			if p.slow.Load() {
				select {
				case <-ctx.Done():
					return
				case <-time.After(25 * time.Millisecond):
				}
			}
			for remaining := buffer[:n]; len(remaining) != 0; {
				written, writeErr := dst.Write(remaining)
				if writeErr != nil || written == 0 {
					return
				}
				remaining = remaining[written:]
			}
		}
		if err != nil {
			return
		}
	}
}

type upgradeEcho struct {
	listener net.Listener
	mu       sync.Mutex
	seen     map[string]int
	closed   map[string]int
	peers    map[net.Conn]bool
	closing  bool
	wg       sync.WaitGroup
}

func newUpgradeEcho(t *testing.T) *upgradeEcho {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	e := &upgradeEcho{listener: listener, seen: map[string]int{}, closed: map[string]int{}, peers: map[net.Conn]bool{}}
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		for {
			peer, err := listener.Accept()
			if err != nil {
				return
			}
			e.mu.Lock()
			if e.closing {
				e.mu.Unlock()
				peer.Close()
				return
			}
			e.peers[peer] = true
			e.mu.Unlock()
			e.wg.Add(1)
			go func() {
				defer e.wg.Done()
				defer peer.Close()
				defer func() { e.mu.Lock(); delete(e.peers, peer); e.mu.Unlock() }()
				reader := bufio.NewReader(peer)
				line, err := reader.ReadString('\n')
				if err != nil {
					return
				}
				marker := strings.TrimSuffix(line, "\n")
				e.mu.Lock()
				e.seen[marker]++
				e.mu.Unlock()
				defer func() { e.mu.Lock(); e.closed[marker]++; e.mu.Unlock() }()
				if strings.HasPrefix(line, "hold:") {
					// A real received request deliberately has no application reply.
					// The old TCP session must close when its host is upgraded.
					_, _ = io.Copy(io.Discard, reader)
					return
				}
				_, _ = peer.Write([]byte("echo:" + line))
			}()
		}
	}()
	t.Cleanup(func() {
		listener.Close()
		e.mu.Lock()
		e.closing = true
		for peer := range e.peers {
			peer.Close()
		}
		e.mu.Unlock()
		e.wg.Wait()
	})
	return e
}

func (e *upgradeEcho) count(marker string) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.seen[marker]
}

func (e *upgradeEcho) closedCount(marker string) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.closed[marker]
}
