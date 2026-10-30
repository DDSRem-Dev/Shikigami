package node

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	"github.com/DDSRem-Dev/Shikigami/internal/server/storage"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	agentpb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	agentrpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent/agentconnect"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/net/netns"
	"tailscale.com/tailcfg"
	"tailscale.com/tstest/integration"
	"tailscale.com/tstest/integration/testcontrol"
	"tailscale.com/types/logger"
)

func localControl(t *testing.T, auth bool, authKey string) *testcontrol.Server {
	t.Helper()
	netns.SetEnabled(false)
	t.Cleanup(func() { netns.SetEnabled(true) })
	derpMap := integration.RunDERPAndSTUN(t, logger.Discard, "127.0.0.1")
	control := &testcontrol.Server{DERPMap: derpMap, RequireAuth: auth, RequireAuthKey: authKey, AllNodesSameUser: true, AllOnline: true, MagicDNSDomain: "test.shiki.ts.net", DNSConfig: &tailcfg.DNSConfig{Proxied: true}, Logf: logger.Discard}
	control.HTTPTestServer = httptest.NewServer(control)
	t.Cleanup(control.HTTPTestServer.Close)
	return control
}
func newTestNode(t *testing.T, ctx context.Context, control *testcontrol.Server, name string) *Node {
	t.Helper()
	node, err := Open(ctx, Options{StateDir: filepath.Join(t.TempDir(), "identity"), Hostname: name, ControlURL: control.HTTPTestServer.URL, allowHTTPForTest: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { node.Close() })
	return node
}
func awaitState(t *testing.T, node *Node, predicate func(*agentpb.NodeStatus) bool) *agentpb.NodeStatus {
	t.Helper()
	deadline := time.NewTimer(15 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		status := node.snapshot()
		if predicate(status) {
			return status
		}
		select {
		case <-deadline.C:
			t.Fatalf("node state timeout: state=%s reason=%s", status.State, status.ReasonCode)
		case <-tick.C:
		}
	}
}
func loginInteractive(t *testing.T, node *Node, control *testcontrol.Server) {
	t.Helper()
	_, err := node.beginLogin(&agentpb.BeginLoginRequest{Method: &agentpb.BeginLoginRequest_Interactive{Interactive: true}, RequestId: "login-" + node.options.Hostname})
	if err != nil {
		t.Fatal(err)
	}
	status := awaitState(t, node, func(s *agentpb.NodeStatus) bool { return s.LoginUrl != "" })
	if !control.CompleteAuth(status.LoginUrl) {
		t.Fatal("test control did not recognize SDK login URL")
	}
	awaitState(t, node, func(s *agentpb.NodeStatus) bool { return s.State == "online" })
}
func TestNodeControlBeforeLoginCancelAndPersistence(t *testing.T) {
	control := localControl(t, true, "")
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	node := newTestNode(t, ctx, control, "device-one")
	dir, err := os.MkdirTemp("/tmp", "shiki-node-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	listener, err := connection.ListenUnix(filepath.Join(dir, "node.sock"), uint32(os.Getuid()))
	if err != nil {
		t.Fatal(err)
	}
	server := connect.NewServer()
	agentrpc.RegisterNodeControlHandler(server, NewControl(node, uint32(os.Getuid()), "test"))
	hs := connection.NewHTTPServer(ctx, connection.NewRPCHandler(server))
	done := make(chan error, 1)
	go func() { done <- hs.Serve(listener) }()
	t.Cleanup(func() { hs.Close(); <-done })
	endpoint, err := connection.NewUnixClient(filepath.Join(dir, "node.sock"), uint32(os.Getuid()), "shiki-node")
	if err != nil {
		t.Fatal(err)
	}
	defer endpoint.Close()
	client := agentrpc.NewNodeControlClient(connection.NewRPCClient(endpoint))
	negotiated, err := client.Negotiate(ctx, &pb.ProtocolOffer{Major: 1, Capabilities: []string{"node.login.v1"}, RequiredCapabilities: []string{"node.login.v1"}})
	if err != nil {
		t.Fatal(err)
	}
	localCtx, info := connect.NewClientContext(ctx)
	info.RequestHeader().Set(NodeInstanceHeader, negotiated.InstanceId)
	if _, err = client.GetStatus(ctx, &pb.Empty{}); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("unnegotiated status: %v", err)
	}
	before, err := client.GetStatus(localCtx, &pb.Empty{})
	if err != nil || before.State == "online" {
		t.Fatal("local control unavailable before login")
	}
	request := &agentpb.BeginLoginRequest{Method: &agentpb.BeginLoginRequest_Interactive{Interactive: true}, RequestId: "initial-login"}
	first, err := client.BeginLogin(localCtx, request)
	if err != nil {
		t.Fatal(err)
	}
	repeat, err := client.BeginLogin(localCtx, request)
	if err != nil || repeat.LoginId != first.LoginId {
		t.Fatal("login request was not idempotent")
	}
	if _, err = client.BeginLogin(localCtx, &agentpb.BeginLoginRequest{Method: &agentpb.BeginLoginRequest_AuthKey{AuthKey: []byte("different")}, RequestId: request.RequestId}); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("mismatched login request accepted: %v", err)
	}
	status := awaitState(t, node, func(s *agentpb.NodeStatus) bool { return s.LoginUrl != "" })
	oldURL := status.LoginUrl
	stopped, err := client.CancelLogin(localCtx, &agentpb.CancelLoginRequest{LoginId: first.LoginId})
	if err != nil || stopped.LoginUrl != "" || stopped.ReasonCode != "LOGIN_CANCELED" {
		t.Fatal("login cancel failed")
	}
	// Completing the old control-plane URL must not resurrect the canceled local attempt.
	control.CompleteAuth(oldURL)
	if got := node.snapshot(); got.State == "online" || got.LoginUrl != "" {
		t.Fatal("canceled attempt became online")
	}
	loginInteractive(t, node, control)
	online := node.snapshot()
	if len(online.Addresses) == 0 {
		t.Fatal("online status did not report embedded addresses")
	}
	noopRequest := &agentpb.BeginLoginRequest{Method: &agentpb.BeginLoginRequest_Interactive{Interactive: true}, RequestId: "already-online"}
	noop, err := client.BeginLogin(localCtx, noopRequest)
	if err != nil || noop.State != "online" || noop.LoginId == "" {
		t.Fatal("online login request did not complete")
	}
	noopReplay, err := client.BeginLogin(localCtx, noopRequest)
	if err != nil || noopReplay.LoginId != noop.LoginId {
		t.Fatal("online login request lost its deduplicated result")
	}
	if canceled, err := client.CancelLogin(localCtx, &agentpb.CancelLoginRequest{LoginId: noop.LoginId}); err != nil || canceled.State != "online" {
		t.Fatal("canceling completed login affected the online node")
	}
	opts := node.options
	if _, err = Open(ctx, opts); !errors.Is(err, storage.ErrLocked) {
		t.Fatalf("duplicate node identity opened: %v", err)
	}
	if err = node.Close(); err != nil {
		t.Fatal(err)
	}
	recovered, err := Open(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer recovered.Close()
	after := awaitState(t, recovered, func(s *agentpb.NodeStatus) bool { return s.State == "online" })
	if len(after.Addresses) != len(online.Addresses) || after.Addresses[0] != online.Addresses[0] {
		t.Fatal("node identity/address changed on restart")
	}
	if recovered.InstanceID() == node.InstanceID() {
		t.Fatal("NodeControl runtime instance reused")
	}
	noopReplay, err = recovered.beginLogin(noopRequest)
	if err != nil || noopReplay.LoginId != noop.LoginId || noopReplay.State != "online" {
		t.Fatal("restart lost completed online login request")
	}
	if _, err = recovered.beginLogin(&agentpb.BeginLoginRequest{Method: &agentpb.BeginLoginRequest_AuthKey{AuthKey: []byte("different")}, RequestId: noopRequest.RequestId}); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatal("restart lost online request parameter deduplication")
	}
	// Cancellation of a historical completed request returns current status,
	// without replaying its old online result or stopping the active SDK.
	recovered.mu.Lock()
	recovered.watchCancel()
	recovered.generation++ // Reject any observation already queued by the watcher.
	recovered.status = &agentpb.NodeStatus{State: "expired", ReasonCode: "NODE_EXPIRED"}
	recovered.mu.Unlock()
	if canceled, err := recovered.cancelAttempt(noop.LoginId, "LOGIN_CANCELED"); err != nil || canceled.State != "expired" {
		t.Fatal("historical cancellation replayed stale authorization status")
	}
}

type nodeRPCProbe struct {
	rpc.UnimplementedShikiDaemonHandler
}

func (nodeRPCProbe) NegotiateProtocol(ctx context.Context, offer *pb.ProtocolOffer) (*pb.ProtocolAgreement, error) {
	info, _ := connect.CallInfoForServerContext(ctx)
	if info.RequestHeader().Get("Authorization") != "Bearer shiki_tailnet_test" {
		return nil, connect.NewError(connect.CodeUnauthenticated, "TOKEN_REQUIRED")
	}
	return &pb.ProtocolAgreement{Major: offer.Major}, nil
}
func (nodeRPCProbe) ReadFile(ctx context.Context, request *pb.ReadFileRequest, out rpc.ShikiDaemonReadFileServerStream) error {
	return out.Send(&pb.FileData{Payload: &pb.FileData_Chunk{Chunk: []byte("download")}})
}
func (nodeRPCProbe) WriteFile(ctx context.Context, in rpc.ShikiDaemonWriteFileServerStream) (*pb.FileInfo, error) {
	var size int64
	for {
		frame, err := in.Receive()
		if errors.Is(err, io.EOF) {
			return &pb.FileInfo{Size: size}, nil
		}
		if err != nil {
			return nil, err
		}
		size += int64(len(frame.GetChunk()))
	}
}
func (nodeRPCProbe) TerminalStream(ctx context.Context, stream rpc.ShikiDaemonTerminalStreamServerStream) error {
	_, err := stream.Receive()
	if err != nil {
		return err
	}
	if err = stream.Send(&pb.TerminalOutput{Payload: &pb.TerminalOutput_Data{Data: []byte("READY")}}); err != nil {
		return err
	}
	for {
		frame, err := stream.Receive()
		if errors.Is(err, io.EOF) {
			return stream.Send(&pb.TerminalOutput{Payload: &pb.TerminalOutput_Data{Data: []byte("after-half-close")}})
		}
		if err != nil {
			return err
		}
		if err = stream.Send(&pb.TerminalOutput{Payload: &pb.TerminalOutput_Data{Data: frame.GetData()}}); err != nil {
			return err
		}
	}
}

// connectedNodePair gives each independent acceptance case fresh identities,
// control plane, listeners and an unchanged bounded 45-second lifetime.
func connectedNodePair(t *testing.T) (context.Context, *Node, *Node, *testcontrol.Server, rpc.ShikiDaemonClient) {
	t.Helper()
	control := localControl(t, true, "")
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	t.Cleanup(cancel)
	left := newTestNode(t, ctx, control, "left")
	right := newTestNode(t, ctx, control, "right")
	loginInteractive(t, left, control)
	loginInteractive(t, right, control)
	listener, err := right.Listen(9527)
	if err != nil {
		t.Fatal(err)
	}
	server := connect.NewServer()
	rpc.RegisterShikiDaemonHandler(server, nodeRPCProbe{})
	hs := connection.NewHTTPServer(ctx, connection.NewRPCHandler(server))
	done := make(chan error, 1)
	go func() { done <- hs.Serve(listener) }()
	t.Cleanup(func() { hs.Close(); <-done })
	target := net.JoinHostPort(right.snapshot().Addresses[0], "9527")
	pool, err := left.NewRPCPool(target, "shiki_tailnet_test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	client := rpc.NewShikiDaemonClient(pool.Client)
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	for {
		agreement, err := client.NegotiateProtocol(ctx, &pb.ProtocolOffer{Major: 1})
		if err == nil {
			if agreement.Major != 1 {
				t.Fatal("protocol mismatch")
			}
			break
		}
		select {
		case <-deadline.C:
			t.Fatalf("peer connect failed: %v", err)
		case <-time.After(20 * time.Millisecond):
		}
	}
	return ctx, left, right, control, client
}

func TestTwoEmbeddedNodesAllConnectStreamShapes(t *testing.T) {
	ctx, left, _, _, client := connectedNodePair(t)
	read, err := client.ReadFile(ctx, &pb.ReadFileRequest{})
	if err != nil {
		t.Fatal(err)
	}
	defer read.Close()
	file, err := read.Receive()
	if err != nil || string(file.GetChunk()) != "download" {
		t.Fatal("server stream failed")
	}
	if _, err = read.Receive(); !errors.Is(err, io.EOF) {
		t.Fatal(err)
	}
	write, err := client.WriteFile(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = write.Send(&pb.WriteFileRequest{Payload: &pb.WriteFileRequest_Chunk{Chunk: []byte("upload")}}); err != nil {
		t.Fatal(err)
	}
	info, err := write.CloseAndReceive()
	if err != nil || info.Size != 6 {
		t.Fatal("client stream failed")
	}
	stream, err := client.TerminalStream(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if err = stream.Send(&pb.TerminalInput{Payload: &pb.TerminalInput_Open{Open: &pb.TerminalOpen{SessionId: "session"}}}); err != nil {
		t.Fatal(err)
	}
	ready, err := stream.Receive()
	if err != nil || string(ready.GetData()) != "READY" {
		t.Fatal("full-duplex READY was not received before CloseSend")
	}
	payload := []byte{0, 0xff, 27, 0xc0}
	if err = stream.Send(&pb.TerminalInput{Payload: &pb.TerminalInput_Data{Data: payload}}); err != nil {
		t.Fatal(err)
	}
	echo, err := stream.Receive()
	if err != nil || !bytes.Equal(echo.GetData(), payload) {
		t.Fatal("binary bytes changed")
	}
	if err = stream.CloseSend(); err != nil {
		t.Fatal(err)
	}
	last, err := stream.Receive()
	if err != nil || string(last.GetData()) != "after-half-close" {
		t.Fatal("half-close lost response")
	}
	if _, err = stream.Receive(); !errors.Is(err, io.EOF) {
		t.Fatal(err)
	}
	for _, target := range []string{"127.0.0.1:9527", "8.8.8.8:9527", "unrecognized.invalid:9527"} {
		if conn, err := left.Dial(ctx, "tcp", target); err == nil {
			conn.Close()
			t.Fatalf("unsafe target accepted: %s", target)
		}
	}
}
func TestTwoEmbeddedNodesPeerAddressForms(t *testing.T) {
	ctx, left, right, control, _ := connectedNodePair(t)
	assertPeerAddressForms(t, ctx, left, right, control)
}

func TestTwoEmbeddedNodesAuthorizationEpoch(t *testing.T) {
	ctx, left, _, _, client := connectedNodePair(t)
	assertRPCPoolAuthorizationLifetime(t, ctx, left, client)
}

func TestTwoEmbeddedNodesRemovedPeer(t *testing.T) {
	ctx, left, right, control, _ := connectedNodePair(t)
	assertRemovedPeerUsesOnlyNetstack(t, ctx, left, right, control)
}

func TestInvalidAuthKeyAndNoCredentialPersistence(t *testing.T) {
	control := localControl(t, false, "test-valid-key")
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	node := newTestNode(t, ctx, control, "auth-test")
	<-node.started
	if _, err := node.beginLogin(&agentpb.BeginLoginRequest{Method: &agentpb.BeginLoginRequest_AuthKey{AuthKey: []byte("test-invalid-key")}, RequestId: "invalid"}); err != nil {
		t.Fatal(err)
	}
	denied := awaitState(t, node, func(s *agentpb.NodeStatus) bool { return s.State == "denied" })
	if denied.ReasonCode != "INVALID_AUTH_KEY" {
		t.Fatal("explicit rejection not classified")
	}
	// Delay the replacement SDK so notifications already queued by the rejected
	// SDK arrive after the new attempt is published but before runLogin starts.
	// No sleeps or scheduler timing are needed to exercise this boundary.
	func() {
		node.opMu.Lock()
		defer node.opMu.Unlock()
		node.mu.Lock()
		staleGeneration := node.generation
		node.mu.Unlock()
		pending, err := node.beginLogin(&agentpb.BeginLoginRequest{Method: &agentpb.BeginLoginRequest_AuthKey{AuthKey: []byte("test-valid-key")}, RequestId: "valid"})
		if err != nil {
			t.Fatal(err)
		}
		if node.rejectAuthKey(staleGeneration) {
			t.Fatal("old SDK rejection finished the new login attempt")
		}
		node.applyStatus(staleGeneration, &ipnstate.Status{BackendState: "NeedsMachineAuth"})
		node.setGenerationFailure(staleGeneration, "NODE_STATUS_UNAVAILABLE")
		if status := node.snapshot(); status.State != "authenticating" || status.LoginId != pending.LoginId || status.ReasonCode != "" {
			t.Fatalf("old SDK notification changed pending login: state=%s reason=%s", status.State, status.ReasonCode)
		}
	}()
	awaitState(t, node, func(s *agentpb.NodeStatus) bool { return s.State == "online" })
	// A completed attempt's delayed worker must not stop an SDK now owned by
	// a later successful login, even when that worker finally gets opMu.
	node.mu.Lock()
	currentServer, currentGeneration := node.server, node.generation
	rejectedAttempt := node.requests["invalid"]
	node.mu.Unlock()
	node.runLogin(rejectedAttempt, []byte("test-invalid-key"))
	node.mu.Lock()
	unchanged := node.server == currentServer && node.generation == currentGeneration
	node.mu.Unlock()
	if !unchanged || node.snapshot().State != "online" {
		t.Fatal("completed login worker replaced the current authenticated SDK")
	}
	// Stop the SDK and join its workers before inspecting its files. A live
	// backend may replace or remove netmap-cache entries during filepath.Walk.
	// Reopen the committed store so the decoded check includes shutdown writes,
	// rather than only the live store's earlier in-memory snapshot.
	if err := node.Close(); err != nil {
		t.Fatal(err)
	}
	persisted, err := storage.Open(node.options.StateDir, storage.Options{DisableRecovery: true})
	if err != nil {
		t.Fatal(err)
	}
	defer persisted.Close()
	snapshot, err := persisted.View()
	if err != nil {
		t.Fatal(err)
	}
	for id := range snapshot.Buckets["tailscale"] {
		var value stateValue
		if err := snapshot.Get("tailscale", id, &value); err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(value.Value, []byte("test-valid-key")) || bytes.Contains(value.Value, []byte("test-invalid-key")) {
			t.Fatal("auth key persisted inside encoded SDK state")
		}
	}
	if err := filepath.Walk(node.options.StateDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.Contains(data, []byte("test-valid-key")) || bytes.Contains(data, []byte("test-invalid-key")) {
			t.Fatal("auth key persisted")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestNodeStateFailsClosedAndRootCancellationReleasesLock(t *testing.T) {
	control := localControl(t, true, "")
	ctx, cancel := context.WithCancel(context.Background())
	node := newTestNode(t, ctx, control, "cancel-root")
	<-node.started
	cancel()
	select {
	case <-node.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("root cancellation did not close SDK and release state lock")
	}
	if _, err := node.beginLogin(&agentpb.BeginLoginRequest{Method: &agentpb.BeginLoginRequest_Interactive{Interactive: true}, RequestId: "after-close"}); connect.CodeOf(err) != connect.CodeUnavailable {
		t.Fatal("closed node admitted login")
	}
	// A truncated persisted identity must not silently become a new node.
	path := filepath.Join(node.options.StateDir, "state.json")
	if err := os.WriteFile(path, []byte{}, 0600); err != nil {
		t.Fatal(err)
	}
	if reopened, err := Open(context.Background(), node.options); err == nil {
		reopened.Close()
		t.Fatal("truncated identity recreated")
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) != 0 {
		t.Fatal("corrupt identity evidence was overwritten")
	}
}

func TestInterruptedLoginRequestSurvivesRestartWithoutSecret(t *testing.T) {
	control := localControl(t, true, "")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	node := newTestNode(t, ctx, control, "pending-crash")
	request := &agentpb.BeginLoginRequest{Method: &agentpb.BeginLoginRequest_Interactive{Interactive: true}, RequestId: "pending-before-exit"}
	initial, err := node.beginLogin(request)
	if err != nil {
		t.Fatal(err)
	}
	awaitState(t, node, func(s *agentpb.NodeStatus) bool { return s.LoginUrl != "" })
	opts := node.options
	if err = node.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, err := Open(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	replay, err := restarted.beginLogin(request)
	if err != nil || replay.LoginId != initial.LoginId || replay.ReasonCode != "LOGIN_INTERRUPTED" || replay.LoginUrl != "" {
		t.Fatal("restart replayed the interrupted login or lost its result")
	}
	if _, err = restarted.beginLogin(&agentpb.BeginLoginRequest{Method: &agentpb.BeginLoginRequest_AuthKey{AuthKey: []byte("new-secret")}, RequestId: request.RequestId}); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatal("restart lost request parameter deduplication")
	}
}

func assertPeerAddressForms(t *testing.T, ctx context.Context, left, right *Node, control *testcontrol.Server) {
	t.Helper()
	right.mu.Lock()
	lc := right.local
	right.mu.Unlock()
	status, err := lc.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	left.mu.Lock()
	localClient := left.local
	left.mu.Unlock()
	localStatus, err := localClient.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// testcontrol retains pre-login registrations with the same hostname.
	// Preserve ambiguity rejection, then publish a unique authenticated name
	// for the current peer to test real DNS and short-name connections.
	if _, _, err := resolvePeer(localStatus, status.Self.DNSName); connect.CodeOf(err) == connect.CodeInvalidArgument {
		if conn, err := left.Dial(ctx, "tcp", net.JoinHostPort(status.Self.DNSName, "9527")); connect.CodeOf(err) != connect.CodeInvalidArgument {
			if conn != nil {
				conn.Close()
			}
			t.Fatalf("ambiguous peer name admitted: %v", err)
		}
	}
	remote := control.Node(status.Self.PublicKey)
	if remote == nil {
		t.Fatal("current control-plane peer is absent")
	}
	remote.Name = "right-current.test.shiki.ts.net."
	hostinfo := remote.Hostinfo.AsStruct()
	hostinfo.Hostname = "right-current"
	remote.Hostinfo = hostinfo.View()
	if !control.AddRawMapResponse(localStatus.Self.PublicKey, &tailcfg.MapResponse{PeersChanged: []*tailcfg.Node{remote}}) {
		t.Fatal("unique peer name was not delivered")
	}
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for {
		observed, err := localClient.Status(ctx)
		if err != nil {
			t.Fatal(err)
		}
		peer, _, err := resolvePeer(observed, remote.Name)
		if err == nil && peer.PublicKey == remote.Key {
			break
		}
		select {
		case <-deadline.C:
			t.Fatal("unique peer name did not reach the authenticated map")
		case <-time.After(20 * time.Millisecond):
		}
	}
	hosts := []string{remote.Name, hostinfo.Hostname}
	for _, ip := range status.TailscaleIPs {
		hosts = append(hosts, ip.String())
	}
	for _, host := range hosts {
		if host == "" {
			t.Fatal("missing peer address form")
		}
		pool, err := left.NewRPCPool(net.JoinHostPort(host, "9527"), "shiki_tailnet_test")
		if err != nil {
			t.Fatal(err)
		}
		_, err = rpc.NewShikiDaemonClient(pool.Client).NegotiateProtocol(ctx, &pb.ProtocolOffer{Major: 1})
		pool.Close()
		if err != nil {
			t.Fatalf("peer address %s rejected: %v", host, err)
		}
	}
}

func assertRemovedPeerUsesOnlyNetstack(t *testing.T, ctx context.Context, left, right *Node, control *testcontrol.Server) {
	t.Helper()
	left.mu.Lock()
	client := left.local
	server := left.server
	left.mu.Unlock()
	right.mu.Lock()
	remoteClient := right.local
	right.mu.Unlock()
	localStatus, err := client.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	remoteStatus, err := remoteClient.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	remote := control.Node(remoteStatus.Self.PublicKey)
	if remote == nil {
		t.Fatal("missing control-plane peer")
	}
	target := netip.AddrPortFrom(remoteStatus.TailscaleIPs[0], 9527)
	if !control.AddRawMapResponse(localStatus.Self.PublicKey, &tailcfg.MapResponse{PeersRemoved: []tailcfg.NodeID{remote.ID}}) {
		t.Fatal("peer removal was not delivered")
	}
	timeout := time.NewTimer(5 * time.Second)
	defer timeout.Stop()
	for {
		if _, ok := server.Sys().Engine.Get().PeerForIP(target.Addr()); !ok {
			break
		}
		select {
		case <-timeout.C:
			t.Fatal("peer route removal did not reach the SDK engine")
		case <-time.After(20 * time.Millisecond):
		}
	}
	// Simulate a peer disappearing after our authenticated peer-map resolution.
	// The only dial operation here is the typed gVisor subsystem method; no
	// generic SDK/SystemDial callback is part of this adapter's execution path.
	probeCtx, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
	defer cancel()
	conn, err := dialUserspaceTCP(probeCtx, server, target, remoteStatus.Self.PublicKey)
	if err == nil {
		conn.Close()
		t.Fatal("removed peer still accepted a userspace connection")
	}
	if _, err := left.Dial(ctx, "tcp", target.String()); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("removed peer remained routable: %v", err)
	}
}

func assertRPCPoolAuthorizationLifetime(t *testing.T, ctx context.Context, node *Node, client rpc.ShikiDaemonClient) {
	t.Helper()
	node.mu.Lock()
	lc := node.local
	generation := node.generation
	stopWatch := node.watchCancel
	originalLease := node.authorization
	node.mu.Unlock()
	observed, err := lc.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Isolate state observations from periodic SDK notifications for deterministic
	// expiry/revocation races while retaining the real HTTP/2 connection pool.
	stopWatch()
	transient := *observed
	transient.Health = []string{"control temporarily unreachable"}
	node.applyStatus(generation, &transient)
	node.mu.Lock()
	sameLease := node.authorization == originalLease
	node.mu.Unlock()
	if !sameLease || originalLease.Err() != nil {
		t.Fatal("control outage canceled still-running peer transport")
	}
	expired := *observed
	self := *observed.Self
	self.Expired = true
	expired.Self = &self
	for name, invalid := range map[string]*ipnstate.Status{
		"expired":     &expired,
		"denied":      {BackendState: "NeedsMachineAuth"},
		"needs_login": {BackendState: "NeedsLogin"},
	} {
		stream, err := client.TerminalStream(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err = stream.Send(&pb.TerminalInput{Payload: &pb.TerminalInput_Open{Open: &pb.TerminalOpen{SessionId: name}}}); err != nil {
			t.Fatal(err)
		}
		if _, err = stream.Receive(); err != nil {
			t.Fatal(err)
		}
		node.mu.Lock()
		oldLease := node.authorization
		node.mu.Unlock()
		node.applyStatus(generation, invalid)
		if _, err = client.NegotiateProtocol(ctx, &pb.ProtocolOffer{Major: 1}); connect.CodeOf(err) != connect.CodeFailedPrecondition {
			t.Fatalf("%s: reused HTTP2 connection admitted new RPC: %v", name, err)
		}
		if _, err = stream.Receive(); connect.CodeOf(err) != connect.CodeCanceled {
			t.Fatalf("%s: old stream survived invalidation: %v", name, err)
		}
		_ = stream.Close()
		node.applyStatus(generation, observed)
		node.mu.Lock()
		newLease := node.authorization
		node.mu.Unlock()
		if newLease == nil || newLease == oldLease || oldLease.Err() == nil {
			t.Fatal("online transition reused invalid authorization lifetime")
		}
		// HTTP/2 may first observe the retired socket's close. Retry only the
		// authenticated protocol probe; never replay business writes or input.
		deadline := time.Now().Add(3 * time.Second)
		for {
			_, err = client.NegotiateProtocol(ctx, &pb.ProtocolOffer{Major: 1})
			if err == nil {
				break
			}
			if connect.CodeOf(err) != connect.CodeUnavailable || time.Now().After(deadline) {
				t.Fatalf("%s: fresh authorization could not negotiate: %v", name, err)
			}
			select {
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			case <-time.After(20 * time.Millisecond):
			}
		}
	}
}
