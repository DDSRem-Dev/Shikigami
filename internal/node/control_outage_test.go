package node

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	"github.com/DDSRem-Dev/Shikigami/internal/server/storage"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
	"tailscale.com/client/local"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/types/key"
)

func TestCachedRunningCannotAuthorizeOrCompleteLogin(t *testing.T) {
	n := &Node{ctx: t.Context(), status: &apb.NodeStatus{State: "uninitialized"}}
	observed := &ipnstate.Status{BackendState: "Running", Self: &ipnstate.PeerStatus{PublicKey: key.NewNode().Public()}, TailscaleIPs: []netip.Addr{netip.MustParseAddr("100.64.0.1")}}
	n.applyStatus(0, observed)
	if n.snapshot().State != "offline" || n.snapshot().ReasonCode != "NODE_CONTROL_UNAVAILABLE" || n.authorization != nil || len(n.snapshot().Addresses) != 0 {
		t.Fatal("cached Running state advertised live control authorization")
	}
	// A URL from a pending SDK registration must remain visible over an old map.
	n.attempt = &loginAttempt{id: "pending", interactive: true, expires: time.Now().Add(time.Minute), done: make(chan struct{})}
	observed.AuthURL = "https://control.invalid/login/test"
	n.applyStatus(0, observed)
	if n.attempt.finished || n.snapshot().LoginUrl != observed.AuthURL || n.authorization != nil {
		t.Fatal("cached Running completed or concealed the pending login")
	}
}

// cutControlListener interrupts the real SDK's encrypted control connection,
// including hijacked connections, while leaving DERP and peer traffic intact.
type cutControlListener struct {
	net.Listener
	mu      sync.Mutex
	blocked bool
	conns   map[*cutControlConn]struct{}
}
type cutControlConn struct {
	net.Conn
	owner *cutControlListener
	once  sync.Once
}

func (c *cutControlConn) Close() error {
	err := c.Conn.Close()
	c.once.Do(func() { c.owner.mu.Lock(); delete(c.owner.conns, c); c.owner.mu.Unlock() })
	return err
}
func (l *cutControlListener) Accept() (net.Conn, error) {
	for {
		c, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		l.mu.Lock()
		if l.blocked {
			l.mu.Unlock()
			c.Close()
			continue
		}
		wrapped := &cutControlConn{Conn: c, owner: l}
		l.conns[wrapped] = struct{}{}
		l.mu.Unlock()
		return wrapped, nil
	}
}
func (l *cutControlListener) cut(block bool) {
	l.mu.Lock()
	l.blocked = block
	var conns []*cutControlConn
	if block {
		for c := range l.conns {
			conns = append(conns, c)
		}
	}
	l.mu.Unlock()
	for _, c := range conns {
		c.Close()
	}
}

func TestSDKControlOutagePreservesPeerAuthorization(t *testing.T) {
	control := localControl(t, true, "")
	control.HTTPTestServer.Close()
	httpServer := httptest.NewUnstartedServer(control)
	gate := &cutControlListener{Listener: httpServer.Listener, conns: map[*cutControlConn]struct{}{}}
	httpServer.Listener = gate
	control.HTTPTestServer = httpServer
	httpServer.Start()
	t.Cleanup(httpServer.Close)
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	left := newTestNode(t, ctx, control, "outage-client")
	right := newTestNode(t, ctx, control, "outage-server")
	loginInteractive(t, left, control)
	loginInteractive(t, right, control)
	listener, err := right.Listen(9527)
	if err != nil {
		t.Fatal(err)
	}
	server := connect.NewServer()
	rpc.RegisterShikiDaemonHandler(server, &authorizationUploadHandler{})
	hs := connection.NewHTTPServer(ctx, connection.NewRPCHandler(server))
	done := make(chan error, 1)
	go func() { done <- hs.Serve(listener) }()
	defer func() { hs.Close(); <-done }()
	address := net.JoinHostPort(right.snapshot().Addresses[0], "9527")
	pool, err := left.NewRPCPool(address, "shiki_test")
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	client := rpc.NewShikiDaemonClient(pool.Client)
	stream, err := client.TerminalStream(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	exchange := func() {
		t.Helper()
		if err := stream.Send(&pb.TerminalInput{Payload: &pb.TerminalInput_Data{Data: []byte("still-live")}}); err != nil {
			t.Fatal(err)
		}
		out, err := stream.Receive()
		if err != nil || string(out.GetData()) != "still-live" {
			t.Fatal("control outage disrupted peer stream", err)
		}
	}
	exchange()
	left.mu.Lock()
	lease := left.authorization
	left.mu.Unlock()
	gate.cut(true)
	// First establish actual SDK loss of its control poll, independently of our status adapter.
	deadline := time.Now().Add(10 * time.Second)
	for {
		status, err := left.local.Status(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if status.Self != nil && !status.Self.Online {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("SDK did not observe control outage")
		}
		time.Sleep(20 * time.Millisecond)
	}
	awaitState(t, left, func(s *apb.NodeStatus) bool {
		return s.State == "offline" && s.ReasonCode == "NODE_CONTROL_UNAVAILABLE"
	})
	awaitState(t, right, func(s *apb.NodeStatus) bool { return s.State == "offline" })
	left.mu.Lock()
	preserved := left.authorization == lease && lease.Err() == nil
	left.mu.Unlock()
	if !preserved || !left.DataPlaneAuthorized() || !right.DataPlaneAuthorized() {
		t.Fatal("control loss revoked an established authorization lease")
	}
	exchange()
	// Both HTTP/2 reuse and a fresh TCP flow remain available over the last authorized peer map.
	second, err := client.TerminalStream(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = second.Send(&pb.TerminalInput{Payload: &pb.TerminalInput_Data{Data: []byte("new-rpc")}}); err != nil {
		t.Fatal(err)
	}
	if out, err := second.Receive(); err != nil || string(out.GetData()) != "new-rpc" {
		t.Fatal("new RPC rejected during outage", err)
	}
	second.Close()
	conn, err := left.Dial(ctx, "tcp", address)
	if err != nil {
		t.Fatal("new peer flow rejected during outage", err)
	}
	conn.Close()
	extra, err := right.Listen(9528)
	if err != nil {
		t.Fatal("listener rejected during outage", err)
	}
	extra.Close()
	gate.cut(false)
	awaitState(t, left, func(s *apb.NodeStatus) bool { return s.State == "online" })
	awaitState(t, right, func(s *apb.NodeStatus) bool { return s.State == "online" })
	exchange()
	left.mu.Lock()
	same := left.authorization == lease
	left.mu.Unlock()
	if !same {
		t.Fatal("control recovery unnecessarily replaced the peer authorization lease")
	}
}

func TestLoginCompletionRequiresCurrentRegistration(t *testing.T) {
	for _, mode := range []string{"not-started", "same-key", "new-key", "existing-url"} {
		t.Run(mode, func(t *testing.T) {
			store, err := storage.Open(filepath.Join(t.TempDir(), "state"), storage.Options{})
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			oldKey := key.NewNode().Public()
			attempt := &loginAttempt{id: "registration", requestID: "registration", interactive: true, loginStarted: mode != "not-started", previousKey: oldKey, expires: time.Now().Add(time.Minute), done: make(chan struct{})}
			n := &Node{ctx: t.Context(), state: store, status: &apb.NodeStatus{State: "authenticating"}, attempt: attempt}
			observed := &ipnstate.Status{BackendState: "Running", Self: &ipnstate.PeerStatus{PublicKey: oldKey, Online: true}, TailscaleIPs: []netip.Addr{netip.MustParseAddr("100.64.0.1")}}
			if mode == "new-key" {
				observed.Self.PublicKey = key.NewNode().Public()
			}
			if mode == "existing-url" {
				observed.AuthURL = "https://control.invalid/login/existing"
				n.applyStatus(0, observed)
				if attempt.finished || n.snapshot().LoginUrl == "" {
					t.Fatal("existing URL prematurely completed")
				}
				observed.AuthURL = ""
			}
			n.applyStatus(0, observed)
			want := mode == "new-key" || mode == "existing-url"
			if attempt.finished != want || (n.authorization != nil) != want {
				t.Fatalf("registration completed=%v authorized=%v, want %v", attempt.finished, n.authorization != nil, want)
			}
		})
	}
}

func TestSDKPendingInteractiveURLCompletesWithoutAnotherKeyRotation(t *testing.T) {
	control := localControl(t, true, "")
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	n := newTestNode(t, ctx, control, "reuse-login-url")
	request := &apb.BeginLoginRequest{Method: &apb.BeginLoginRequest_Interactive{Interactive: true}, RequestId: "reuse-login-url"}
	first, err := n.beginLogin(request)
	if err != nil {
		t.Fatal(err)
	}
	pending := awaitState(t, n, func(s *apb.NodeStatus) bool { return s.LoginUrl != "" })
	keys := map[key.NodePublic]bool{}
	for _, entry := range control.AllNodes() {
		keys[entry.Key] = true
	}
	// The SDK explicitly reuses a valid pending URL, with no further key rotation.
	if err := n.local.StartLoginInteractive(ctx); err != nil {
		t.Fatal(err)
	}
	observed, err := n.local.StatusWithoutPeers(ctx)
	if err != nil || observed.AuthURL != pending.LoginUrl {
		t.Fatal("SDK did not retain pending URL", err)
	}
	for _, entry := range control.AllNodes() {
		if !keys[entry.Key] {
			t.Fatal("reopening a URL unexpectedly rotated its node key")
		}
	}
	if !control.CompleteAuth(pending.LoginUrl) {
		t.Fatal("URL was not accepted")
	}
	online := awaitState(t, n, func(s *apb.NodeStatus) bool { return s.State == "online" })
	observed, err = n.local.StatusWithoutPeers(ctx)
	if err != nil || !keys[observed.Self.PublicKey] {
		t.Fatal("URL completion used a different registration key", err)
	}
	if online.LoginId != first.LoginId {
		t.Fatal("URL completion lost the active attempt")
	}
	replay, err := n.beginLogin(request)
	if err != nil || replay.State != "online" || replay.LoginId != first.LoginId {
		t.Fatal("completed URL attempt lost deduplication")
	}
}

func TestSDKExplicitLoginAfterControlOutage(t *testing.T) {
	control := localControl(t, true, "")
	control.HTTPTestServer.Close()
	httpServer := httptest.NewUnstartedServer(control)
	gate := &cutControlListener{Listener: httpServer.Listener, conns: map[*cutControlConn]struct{}{}}
	httpServer.Listener = gate
	control.HTTPTestServer = httpServer
	httpServer.Start()
	t.Cleanup(httpServer.Close)
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	n := newTestNode(t, ctx, control, "explicit-recovery")
	loginInteractive(t, n, control)
	before, err := n.local.StatusWithoutPeers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	previous := control.Node(before.Self.PublicKey)
	gate.cut(true)
	awaitState(t, n, func(s *apb.NodeStatus) bool { return s.State == "offline" })
	request := &apb.BeginLoginRequest{Method: &apb.BeginLoginRequest_Interactive{Interactive: true}, RequestId: "recover-control"}
	pending, err := n.beginLogin(request)
	if err != nil || pending.State != "authenticating" {
		t.Fatal("explicit offline login was ignored", err)
	}
	repeated, err := n.beginLogin(request)
	if err != nil || repeated.LoginId != pending.LoginId {
		t.Fatal("pending recovery was not deduplicated")
	}
	gate.cut(false)
	url := awaitState(t, n, func(s *apb.NodeStatus) bool { return s.LoginUrl != "" })
	if !control.CompleteAuth(url.LoginUrl) {
		t.Fatal("recovery URL not recognized")
	}
	awaitState(t, n, func(s *apb.NodeStatus) bool { return s.State == "online" })
	after, err := n.local.StatusWithoutPeers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	current := control.Node(after.Self.PublicKey)
	if current == nil || previous == nil || current.Machine != previous.Machine {
		t.Fatal("explicit login discarded persisted machine identity")
	}
	if before.Self.PublicKey == after.Self.PublicKey {
		t.Fatal("fresh interactive registration did not rotate the node key")
	}
}

func TestCancelLoginDoesNotMistakeCachedRunningForCompletion(t *testing.T) {
	store, err := storage.Open(filepath.Join(t.TempDir(), "state"), storage.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	attempt := &loginAttempt{id: "cached-pending", requestID: "cached-pending", interactive: true, loginStarted: true, expires: time.Now().Add(time.Minute), done: make(chan struct{})}
	cached := &ipnstate.Status{BackendState: "Running", Self: &ipnstate.PeerStatus{PublicKey: key.NewNode().Public()}, TailscaleIPs: []netip.Addr{netip.MustParseAddr("100.64.0.1")}}
	encoded, err := json.Marshal(cached)
	if err != nil {
		t.Fatal(err)
	}
	lc := &local.Client{OmitAuth: true, Transport: peerStatusTransport(func(request *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(encoded)), Request: request}, nil
	})}
	n := &Node{ctx: t.Context(), state: store, attempt: attempt, local: lc, status: &apb.NodeStatus{State: "authenticating"}}
	canceled, err := n.cancelAttempt(attempt.id, "LOGIN_CANCELED")
	if err != nil || !attempt.finished || canceled.ReasonCode != "LOGIN_CANCELED" || canceled.LoginUrl != "" || n.local != nil {
		t.Fatal("cached Running prevented cancellation", err)
	}
	if n.DataPlaneAuthorized() {
		t.Fatal("canceled pending cache acquired a lease")
	}
}

func TestCancellationCommitPreservesConcurrentCompletion(t *testing.T) {
	store, err := storage.Open(filepath.Join(t.TempDir(), "state"), storage.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	attempt := &loginAttempt{id: "completed-during-cancel", requestID: "completed-during-cancel", interactive: true, loginStarted: true, expires: time.Now().Add(time.Minute), done: make(chan struct{})}
	n := &Node{ctx: t.Context(), state: store, attempt: attempt, status: &apb.NodeStatus{State: "authenticating"}}
	// CancelLogin has already observed pending and completed its status I/O.
	// Publish the final online observation before its final commit boundary.
	observed := &ipnstate.Status{BackendState: "Running", Self: &ipnstate.PeerStatus{PublicKey: key.NewNode().Public(), Online: true}, TailscaleIPs: []netip.Addr{netip.MustParseAddr("100.64.0.1")}}
	n.applyStatus(0, observed)
	if !attempt.finished || !n.DataPlaneAuthorized() {
		t.Fatal("completion precondition failed")
	}
	got, err, stop := n.commitCancellation(attempt, "LOGIN_CANCELED")
	if err != nil || stop || got.State != "online" || attempt.result.State != "online" || !n.DataPlaneAuthorized() {
		t.Fatal("late cancellation overwrote successful registration", err)
	}
	got.State = "mutated"
	if n.snapshot().State != "online" {
		t.Fatal("cancellation returned aliased status")
	}
}

func TestExistingURLIsAdoptedWithoutStartingAnotherRegistration(t *testing.T) {
	store, err := storage.Open(filepath.Join(t.TempDir(), "state"), storage.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	oldKey := key.NewNode().Public()
	observed := &ipnstate.Status{BackendState: "Running", AuthURL: "https://control.invalid/login/existing", Self: &ipnstate.PeerStatus{PublicKey: oldKey, Online: false}, TailscaleIPs: []netip.Addr{netip.MustParseAddr("100.64.0.1")}}
	encoded, err := json.Marshal(observed)
	if err != nil {
		t.Fatal(err)
	}
	posts := 0
	lc := &local.Client{OmitAuth: true, Transport: peerStatusTransport(func(request *http.Request) (*http.Response, error) {
		body, code := encoded, http.StatusOK
		if request.Method == http.MethodPost {
			posts++
			body = nil
			code = http.StatusNoContent
		}
		return &http.Response{StatusCode: code, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(body)), Request: request}, nil
	})}
	attempt := &loginAttempt{id: "adopt", requestID: "adopt", interactive: true, expires: time.Now().Add(time.Minute), done: make(chan struct{})}
	n := &Node{ctx: t.Context(), state: store, attempt: attempt, status: &apb.NodeStatus{State: "authenticating"}}
	if err := n.requestLogin(t.Context(), lc, attempt, nil); err != nil {
		t.Fatal(err)
	}
	if posts != 0 {
		t.Fatal("existing URL was restarted; completion racing that call can start a second registration")
	}
	observed.AuthURL = ""
	observed.Self.Online = true
	n.applyStatus(0, observed)
	if !attempt.finished || n.snapshot().State != "online" || attempt.previousKey != oldKey {
		t.Fatal("adopted URL did not finish with its original key")
	}
}

func TestNewLoginDoesNotInheritEarlierURLObservation(t *testing.T) {
	store, err := storage.Open(filepath.Join(t.TempDir(), "state"), storage.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	oldKey := key.NewNode().Public()
	observed := &ipnstate.Status{BackendState: "Running", Self: &ipnstate.PeerStatus{PublicKey: oldKey, Online: true}, TailscaleIPs: []netip.Addr{netip.MustParseAddr("100.64.0.1")}}
	encoded, err := json.Marshal(observed)
	if err != nil {
		t.Fatal(err)
	}
	lc := &local.Client{OmitAuth: true, Transport: peerStatusTransport(func(request *http.Request) (*http.Response, error) {
		code, body := http.StatusOK, encoded
		if request.Method == http.MethodPost {
			code, body = http.StatusNoContent, nil
		}
		return &http.Response{StatusCode: code, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(body)), Request: request}, nil
	})}
	attempt := &loginAttempt{id: "new-flow", requestID: "new-flow", interactive: true, sawAuthURL: true, previousKey: oldKey, expires: time.Now().Add(time.Minute), done: make(chan struct{})}
	n := &Node{ctx: t.Context(), state: store, attempt: attempt, status: &apb.NodeStatus{State: "authenticating"}}
	if err := n.requestLogin(t.Context(), lc, attempt, nil); err != nil {
		t.Fatal(err)
	}
	n.applyStatus(0, observed)
	if attempt.finished || attempt.sawAuthURL || n.DataPlaneAuthorized() {
		t.Fatal("an earlier URL observation completed a newly started flow")
	}
}

func TestLoginStartupPermitWaitIsCancelable(t *testing.T) {
	n := &Node{statusPermit: make(chan struct{}, 1)}
	n.statusPermit <- struct{}{}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := n.requestLogin(ctx, nil, nil, nil); !errors.Is(err, context.Canceled) {
		t.Fatal("login startup ignored cancellation while awaiting status publication", err)
	}
	if len(n.statusPermit) != 1 {
		t.Fatal("canceling login stole another observation's permit")
	}
}
