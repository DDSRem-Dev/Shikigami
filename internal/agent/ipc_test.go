package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	agentpb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	agentrpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent/agentconnect"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
)

type daemonProbe struct {
	rpc.UnimplementedShikiDaemonHandler
	mu     sync.Mutex
	active map[string]chan struct{}
	count  int
}

func (d *daemonProbe) GetSystemInfo(ctx context.Context, _ *pb.Empty) (*pb.SystemInfo, error) {
	info, _ := connect.CallInfoForServerContext(ctx)
	if info.RequestHeader().Get("Authorization") != "Bearer shiki_stored" || info.RequestHeader().Get(InstanceHeader) != "" || info.RequestHeader().Get(FrontendHeader) != "" || info.RequestHeader().Get(ServerHeader) != "" || info.RequestHeader().Get("X-Secret") != "" {
		return nil, connect.NewError(connect.CodePermissionDenied, "HEADERS_LEAKED")
	}
	return &pb.SystemInfo{DaemonId: "daemon-a"}, nil
}
func (d *daemonProbe) GetSession(_ context.Context, request *pb.GetSessionRequest) (*pb.Session, error) {
	code := connect.CodeAborted
	switch request.Id {
	case "denied":
		code = connect.CodePermissionDenied
	case "unauthenticated":
		code = connect.CodeUnauthenticated
	case "exhausted":
		code = connect.CodeResourceExhausted
	}
	return nil, connect.NewError(code, "secret-server-path-and-token")
}
func (d *daemonProbe) TakeControl(context.Context, *pb.TakeControlRequest) (*pb.Empty, error) {
	return &pb.Empty{}, nil
}
func (d *daemonProbe) TerminalStream(ctx context.Context, stream rpc.ShikiDaemonTerminalStreamServerStream) error {
	open, err := stream.Receive()
	if err != nil {
		return err
	}
	d.mu.Lock()
	d.count++
	id := fmt.Sprintf("attachment-%d", d.count)
	done := make(chan struct{})
	d.active[id] = done
	d.mu.Unlock()
	defer close(done)
	session := open.GetOpen().GetSessionId()
	if err = stream.Send(&pb.TerminalOutput{SessionId: session, AttachmentId: id, Payload: &pb.TerminalOutput_Event{Event: &pb.SessionEvent{Type: pb.SessionEvent_READY}}}); err != nil {
		return err
	}
	for {
		input, err := stream.Receive()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if err = stream.Send(&pb.TerminalOutput{SessionId: session, AttachmentId: id, Payload: &pb.TerminalOutput_Data{Data: input.GetData()}}); err != nil {
			return err
		}
	}
}
func (d *daemonProbe) waitClosed(t *testing.T, id string) {
	t.Helper()
	d.mu.Lock()
	done := d.active[id]
	d.mu.Unlock()
	if done == nil {
		t.Fatal("missing remote attachment")
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("remote terminal was not canceled")
	}
}
func serveUnix(t *testing.T, handler http.Handler, root context.Context) *connection.Endpoint {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "shiki-ipc-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	socket := filepath.Join(dir, "agent.sock")
	listener, err := connection.ListenUnix(socket, uint32(os.Getuid()))
	if err != nil {
		t.Fatal(err)
	}
	server := connection.NewHTTPServer(root, handler)
	finished := make(chan error, 1)
	go func() { finished <- server.Serve(listener) }()
	t.Cleanup(func() { server.Close(); <-finished })
	endpoint, err := connection.NewUnixClient(socket, uint32(os.Getuid()), "shiki-agent")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(endpoint.Close)
	return endpoint
}
func setupIPC(t *testing.T) (*Service, *connect.Client, *daemonProbe, context.Context) {
	t.Helper()
	root, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	probe := &daemonProbe{active: map[string]chan struct{}{}}
	remoteServer := connect.NewServer()
	rpc.RegisterShikiDaemonHandler(remoteServer, probe)
	remote := serveUnix(t, connection.NewRPCHandler(remoteServer), root)
	token, err := connection.BearerToken("shiki_stored")
	if err != nil {
		t.Fatal(err)
	}
	remoteClient := connection.NewRPCClient(remote, token)
	service := NewService(root, uint32(os.Getuid()), "dev", func(ctx context.Context, id string) (*connect.Client, error) {
		if id != "srv-a" {
			return nil, connect.NewError(connect.CodeNotFound, "SERVER_NOT_FOUND")
		}
		return remoteClient, nil
	})
	local := serveUnix(t, service.Handler(service.RPCServer()), root)
	return service, connection.NewRPCClient(local), probe, root
}
func handshake(t *testing.T, client agentrpc.AgentClient) *agentpb.HandshakeResponse {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result, err := client.Handshake(ctx, &agentpb.HandshakeRequest{Protocol: &pb.ProtocolOffer{Major: 1, Capabilities: []string{"agent.events.v1"}}})
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func callContext(parent context.Context, handshake *agentpb.HandshakeResponse, server string) context.Context {
	ctx, info := connect.NewClientContext(parent)
	info.RequestHeader().Set(InstanceHeader, handshake.InstanceId)
	info.RequestHeader().Set(FrontendHeader, handshake.FrontendId)
	if server != "" {
		info.RequestHeader().Set(ServerHeader, server)
	}
	return ctx
}
func TestIPCHandshakeRoutingAndHeaders(t *testing.T) {
	_, raw, _, _ := setupIPC(t)
	client := agentrpc.NewAgentClient(raw)
	remote := rpc.NewShikiDaemonClient(raw)
	frontend := handshake(t, client)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := client.GetStatus(ctx, &pb.Empty{}); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("missing handshake: %v", err)
	}
	stale := &agentpb.HandshakeResponse{InstanceId: "previous-instance", FrontendId: frontend.FrontendId}
	if _, err := client.GetStatus(callContext(ctx, stale, ""), &pb.Empty{}); connect.CodeOf(err) != connect.CodeFailedPrecondition || !strings.Contains(err.Error(), "AGENT_RESTARTED") {
		t.Fatalf("old instance: %v", err)
	}
	route := callContext(ctx, frontend, "srv-a")
	info, _ := connect.CallInfoForClientContext(route)
	info.RequestHeader().Set("X-Secret", "must-not-forward")
	result, err := remote.GetSystemInfo(route, &pb.Empty{})
	if err != nil || result.GetDaemonId() != "daemon-a" {
		t.Fatalf("trusted routing/header stripping: %v %v", result, err)
	}
	for name, server := range map[string]string{"missing": "", "unknown": "missing"} {
		t.Run(name, func(t *testing.T) {
			_, err := remote.GetSystemInfo(callContext(ctx, frontend, server), &pb.Empty{})
			if err == nil {
				t.Fatal("route accepted")
			}
		})
	}
	if _, err := remote.ListFiles(callContext(ctx, frontend, "srv-a"), &pb.ListFilesRequest{}); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("file proxy admitted: %v", err)
	}
	if _, err := remote.NegotiateProtocol(callContext(ctx, frontend, "srv-a"), &pb.ProtocolOffer{Major: 1}); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("negotiation proxy admitted: %v", err)
	}
	for id, code := range map[string]connect.Code{"session": connect.CodeAborted, "denied": connect.CodePermissionDenied, "unauthenticated": connect.CodeUnauthenticated, "exhausted": connect.CodeResourceExhausted} {
		if _, err := remote.GetSession(callContext(ctx, frontend, "srv-a"), &pb.GetSessionRequest{Id: id}); connect.CodeOf(err) != code || strings.Contains(err.Error(), "secret") {
			t.Fatalf("unsafe downstream error %s: %v", id, err)
		}
	}
	bearer := callContext(ctx, frontend, "srv-a")
	info, _ = connect.CallInfoForClientContext(bearer)
	info.RequestHeader().Set("Authorization", "Bearer shiki_frontend")
	if _, err := remote.GetSystemInfo(bearer, &pb.Empty{}); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("frontend token accepted: %v", err)
	}
	duplicate := callContext(ctx, frontend, "srv-a")
	info, _ = connect.CallInfoForClientContext(duplicate)
	info.RequestHeader().Add(ServerHeader, "srv-b")
	if _, err := remote.GetSystemInfo(duplicate, &pb.Empty{}); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("duplicate route accepted: %v", err)
	}
	if _, err := client.ListMappings(callContext(ctx, frontend, "srv-a"), &agentpb.ListMappingsRequest{ServerId: "srv-b"}); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("conflicting body/header route: %v", err)
	}
}
func TestFrontendsOwnIndependentTerminalStreams(t *testing.T) {
	service, raw, probe, root := setupIPC(t)
	client := agentrpc.NewAgentClient(raw)
	remote := rpc.NewShikiDaemonClient(raw)
	first, second := handshake(t, client), handshake(t, client)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	a, err := remote.TerminalStream(callContext(ctx, first, "srv-a"))
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := remote.TerminalStream(callContext(ctx, second, "srv-a"))
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	open := &pb.TerminalInput{Payload: &pb.TerminalInput_Open{Open: &pb.TerminalOpen{SessionId: "sess-shared"}}}
	if err = a.Send(open); err != nil {
		t.Fatal(err)
	}
	if err = b.Send(open); err != nil {
		t.Fatal(err)
	}
	readyA, err := a.Receive()
	if err != nil {
		t.Fatal(err)
	}
	readyB, err := b.Receive()
	if err != nil {
		t.Fatal(err)
	}
	if readyA.AttachmentId == readyB.AttachmentId || service.Registry.TerminalCount() != 2 {
		t.Fatal("attachments were shared")
	}
	takeover := &pb.TakeControlRequest{SessionId: "sess-shared", AttachmentId: readyA.AttachmentId}
	if _, err = remote.TakeControl(callContext(ctx, second, "srv-a"), takeover); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("cross-frontend takeover: %v", err)
	}
	if _, err = remote.TakeControl(callContext(ctx, first, "srv-a"), takeover); err != nil {
		t.Fatalf("own takeover: %v", err)
	}
	if _, err = client.ReleaseFrontend(callContext(ctx, first, ""), &pb.Empty{}); err != nil {
		t.Fatal(err)
	}
	if _, err = a.Receive(); err == nil {
		t.Fatal("released terminal remained open")
	}
	probe.waitClosed(t, readyA.AttachmentId)
	if root.Err() != nil {
		t.Fatal("frontend release canceled background root")
	}
	if err = b.Send(&pb.TerminalInput{Payload: &pb.TerminalInput_Data{Data: []byte{0, 0xff, 27, 'x'}}}); err != nil {
		t.Fatal(err)
	}
	output, err := b.Receive()
	if err != nil || string(output.GetData()) != string([]byte{0, 0xff, 27, 'x'}) {
		t.Fatalf("second frontend interrupted or bytes changed: %v %v", output, err)
	}
	if _, err = client.GetStatus(callContext(ctx, first, ""), &pb.Empty{}); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("released frontend reused: %v", err)
	}
	if err = b.CloseSend(); err != nil {
		t.Fatal(err)
	}
	if _, err = b.Receive(); !errors.Is(err, io.EOF) {
		t.Fatalf("terminal close status: %v", err)
	}
	probe.waitClosed(t, readyB.AttachmentId)
}
func TestEventStreamAndOverflow(t *testing.T) {
	service, raw, _, _ := setupIPC(t)
	client := agentrpc.NewAgentClient(raw)
	frontend := handshake(t, client)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stream, err := client.WatchEvents(callContext(ctx, frontend, ""), &pb.Empty{})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	first, err := stream.Receive()
	if err != nil || first.Type != "rescan_required" {
		t.Fatalf("initial hint: %v %v", first, err)
	}
	service.Events.Publish(&agentpb.AgentEvent{Type: "mapping", ResourceId: "mapping-a"})
	next, err := stream.Receive()
	if err != nil || next.Sequence <= first.Sequence || next.InstanceId != frontend.InstanceId || next.ResourceId != "mapping-a" {
		t.Fatalf("event: %v %v", next, err)
	}
	subscriber, stop := service.Events.subscribe()
	defer stop()
	for i := 0; i < EventQueueSize; i++ {
		service.Events.Publish(&agentpb.AgentEvent{Type: "mapping"})
	}
	if _, err = subscriber.receive(ctx); connect.CodeOf(err) != connect.CodeResourceExhausted {
		t.Fatalf("overflow silently dropped events: %v", err)
	}
	if _, err = client.ReleaseFrontend(callContext(ctx, frontend, ""), &pb.Empty{}); err != nil {
		t.Fatal(err)
	}
	for {
		_, err = stream.Receive()
		if err != nil {
			break
		}
	}
	if connect.CodeOf(err) != connect.CodeCanceled {
		t.Fatalf("release subscription: %v", err)
	}
}

func TestTerminalRejectsRepeatedOpen(t *testing.T) {
	_, raw, probe, _ := setupIPC(t)
	frontend := handshake(t, agentrpc.NewAgentClient(raw))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	remote := rpc.NewShikiDaemonClient(raw)
	stream, err := remote.TerminalStream(callContext(ctx, frontend, "srv-a"))
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	open := &pb.TerminalInput{Payload: &pb.TerminalInput_Open{Open: &pb.TerminalOpen{SessionId: "session-a"}}}
	if err = stream.Send(open); err != nil {
		t.Fatal(err)
	}
	ready, err := stream.Receive()
	if err != nil {
		t.Fatal(err)
	}
	if err = stream.Send(open); err != nil {
		t.Fatal(err)
	}
	if _, err = stream.Receive(); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("repeated open: %v", err)
	}
	probe.waitClosed(t, ready.AttachmentId)
}
func TestRegistryQuotaAndIdleCollection(t *testing.T) {
	root, cancel := context.WithCancel(context.Background())
	defer cancel()
	registry := NewRegistry(root)
	ids := make([]string, 0, maxFrontends)
	for range maxFrontends {
		id, err := registry.Register()
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if _, err := registry.Register(); connect.CodeOf(err) != connect.CodeResourceExhausted {
		t.Fatalf("unbounded frontend registry: %v", err)
	}
	active, done, err := registry.bind(root, registry.InstanceID(), ids[0], true)
	if err != nil {
		t.Fatal(err)
	}
	defer done()
	registry.mu.Lock()
	for _, frontend := range registry.frontends {
		frontend.lastCall = time.Now().Add(-11 * time.Minute)
	}
	registry.mu.Unlock()
	if _, err = registry.Register(); err != nil {
		t.Fatalf("idle registrations not collected: %v", err)
	}
	if active.Err() != nil {
		t.Fatal("active frontend collected")
	}
	if _, _, err = registry.bind(root, registry.InstanceID(), ids[1], true); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("idle frontend survived: %v", err)
	}
	registry.Release(ids[0])
	select {
	case <-active.Done():
	case <-time.After(time.Second):
		t.Fatal("release did not cancel frontend context")
	}
	if root.Err() != nil {
		t.Fatal("background agent was stopped")
	}
}

func TestIPCRequiresVerifiedPeerUID(t *testing.T) {
	root, cancel := context.WithCancel(context.Background())
	defer cancel()
	service := NewService(root, uint32(os.Getuid()+1), "dev", nil)
	endpoint := serveUnix(t, service.Handler(service.RPCServer()), root)
	client := agentrpc.NewAgentClient(connection.NewRPCClient(endpoint))
	ctx, cancelCall := context.WithTimeout(root, time.Second)
	defer cancelCall()
	ctx, info := connect.NewClientContext(ctx)
	info.RequestHeader().Set("Shiki-Peer-Uid", fmt.Sprint(os.Getuid()+1))
	if _, err := client.Handshake(ctx, &agentpb.HandshakeRequest{Protocol: &pb.ProtocolOffer{Major: 1}}); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("self-reported UID accepted: %v", err)
	}
}
