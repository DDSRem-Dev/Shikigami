package server

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	"golang.org/x/net/http2"
)

type observedNodeTransport struct {
	mu         sync.Mutex
	state      string
	authorized bool
	listener   net.Listener
	listens    int
}

func (n *observedNodeTransport) Status() *apb.NodeStatus {
	n.mu.Lock()
	defer n.mu.Unlock()
	return &apb.NodeStatus{State: n.state}
}
func (n *observedNodeTransport) DataPlaneAuthorized() bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.authorized
}
func (n *observedNodeTransport) Listen(uint16) (net.Listener, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.listens++
	return n.listener, nil
}
func (n *observedNodeTransport) set(state string, authorized bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.state = state
	n.authorized = authorized
}

// The SDK transition itself is covered by internal/node's real control outage
// test. This test verifies the host does not cancel HTTP/2 above that transport.
func TestDaemonRetainsHTTP2StreamDuringControlOutage(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	ctx, cancel := context.WithCancel(context.Background())
	runtime := &Runtime{ctx: ctx, cancel: cancel, ready: make(chan struct{})}
	transport := &observedNodeTransport{state: "online", authorized: true, listener: listener}
	pulse := make(chan struct{})
	handlerDone := make(chan struct{})
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(handlerDone)
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-pulse:
				if _, err := w.Write([]byte("x")); err != nil {
					return
				}
				w.(http.Flusher).Flush()
			}
		}
	})
	runtime.wg.Add(1)
	go runtime.serveNodeTransport(transport, handler)
	t.Cleanup(func() { cancel(); runtime.wg.Wait() })
	select {
	case <-runtime.ready:
	case <-time.After(3 * time.Second):
		t.Fatal("host did not start")
	}
	clientTransport := &http2.Transport{AllowHTTP: true, DialTLSContext: func(ctx context.Context, network, address string, _ *tls.Config) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, listener.Addr().String())
	}}
	defer clientTransport.CloseIdleConnections()
	requestCtx, requestCancel := context.WithTimeout(ctx, 8*time.Second)
	defer requestCancel()
	req, _ := http.NewRequestWithContext(requestCtx, http.MethodGet, "http://node.test/stream", nil)
	resp, err := (&http.Client{Transport: clientTransport}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	receive := func() {
		t.Helper()
		done := make(chan error, 1)
		go func() { var b [1]byte; _, err := io.ReadFull(resp.Body, b[:]); done <- err }()
		select {
		case pulse <- struct{}{}:
		case <-handlerDone:
			t.Fatal("host canceled a retained peer stream")
		case <-time.After(time.Second):
			t.Fatal("handler did not receive pulse")
		}
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(time.Second):
			t.Fatal("fresh response byte missing")
		}
	}
	receive()
	transport.set("offline", true)
	deadline := time.Now().Add(2 * time.Second)
	for runtime.Info().TailscaleState != "offline" && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if info := runtime.Info(); info.TailscaleState != "offline" || !info.RemoteReady {
		t.Fatalf("control outage incorrectly retired server: %+v", info)
	}
	receive() // A new byte after the host has observed offline, not buffered output.
	transport.mu.Lock()
	listens := transport.listens
	transport.mu.Unlock()
	if listens != 1 {
		t.Fatalf("control outage restarted the listener %d times", listens)
	}
	transport.set("denied", false)
	select {
	case <-handlerDone:
	case <-time.After(2 * time.Second):
		t.Fatal("explicit authorization loss retained HTTP stream")
	}
}
