package client

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	"github.com/DDSRem-Dev/Shikigami/pkg/config"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	arpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent/agentconnect"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
)

type frontendProbe struct {
	arpc.UnimplementedAgentHandler
	mu          sync.Mutex
	next        int
	released    map[string]bool
	omitControl atomic.Bool
	terminal    func(context.Context, rpc.ShikiDaemonTerminalStreamServerStream) error
}

func (p *frontendProbe) Handshake(context.Context, *apb.HandshakeRequest) (*apb.HandshakeResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.next++
	capabilities := []string{"agent.control.v1", "agent.events.v1"}
	if p.omitControl.Load() {
		capabilities = nil
	}
	return &apb.HandshakeResponse{InstanceId: "instance", FrontendId: fmt.Sprint(p.next), Protocol: &pb.ProtocolAgreement{Major: 1, Capabilities: capabilities}}, nil
}
func (p *frontendProbe) headers(ctx context.Context, server string) (string, error) {
	info, _ := connect.CallInfoForServerContext(ctx)
	h := info.RequestHeader()
	if h.Get(instanceHeader) != "instance" || h.Get(frontendHeader) == "" || h.Get(serverHeader) != server || h.Get("Authorization") != "" || h.Get("X-Secret") != "" {
		return "", connect.NewError(connect.CodePermissionDenied, "headers leaked or missing")
	}
	return h.Get(frontendHeader), nil
}
func (p *frontendProbe) GetStatus(ctx context.Context, _ *pb.Empty) (*apb.AgentStatus, error) {
	id, e := p.headers(ctx, "")
	if e != nil {
		return nil, e
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.released[id] {
		return nil, connect.NewError(connect.CodeFailedPrecondition, "frontend released")
	}
	return &apb.AgentStatus{InstanceId: "instance", State: "ready"}, nil
}
func (p *frontendProbe) ReleaseFrontend(ctx context.Context, _ *pb.Empty) (*pb.Empty, error) {
	id, e := p.headers(ctx, "")
	if e != nil {
		return nil, e
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.released[id] = true
	return &pb.Empty{}, nil
}

type frontendRemote struct {
	rpc.UnimplementedShikiDaemonHandler
	p *frontendProbe
}

func (r *frontendRemote) GetSystemInfo(ctx context.Context, _ *pb.Empty) (*pb.SystemInfo, error) {
	id, e := r.p.headers(ctx, "server")
	if e != nil {
		return nil, e
	}
	return &pb.SystemInfo{DaemonId: id}, nil
}
func (r *frontendRemote) TerminalStream(ctx context.Context, s rpc.ShikiDaemonTerminalStreamServerStream) error {
	return r.p.terminal(ctx, s)
}
func frontendFixture(t *testing.T, p *frontendProbe) func() *Client {
	t.Helper()
	p.released = map[string]bool{}
	root, e := os.MkdirTemp("/tmp", "shiki-client-")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	socket := filepath.Join(root, "agent.sock")
	listener, e := connection.ListenUnix(socket, uint32(os.Getuid()))
	if e != nil {
		t.Fatal(e)
	}
	server := connect.NewServer()
	arpc.RegisterAgentHandler(server, p)
	rpc.RegisterShikiDaemonHandler(server, &frontendRemote{p: p})
	ctx, cancel := context.WithCancel(context.Background())
	hs := connection.NewHTTPServer(ctx, connection.NewRPCHandler(server))
	done := make(chan error, 1)
	go func() { done <- hs.Serve(listener) }()
	t.Cleanup(func() { cancel(); hs.Close(); <-done })
	cfg := config.DefaultClient()
	cfg.Agent.Socket = socket
	return func() *Client {
		c, e := dial(context.Background(), "unused", cfg, time.Second)
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { c.Close() })
		return c
	}
}
func TestFrontendIdentityIsolationAndRelease(t *testing.T) {
	p := &frontendProbe{}
	open := frontendFixture(t, p)
	a, b := open(), open()
	if a.FrontendID() == b.FrontendID() {
		t.Fatal("frontends shared identity")
	}
	ctx, info := connect.NewClientContext(context.Background())
	info.RequestHeader().Set("Authorization", "Bearer caller-secret")
	info.RequestHeader().Set("X-Secret", "caller-secret")
	info.RequestHeader().Set(frontendHeader, "forged")
	info.RequestHeader().Set(serverHeader, "other-server")
	for _, c := range []*Client{a, b} {
		result, e := c.Remote("server").GetSystemInfo(ctx, &pb.Empty{})
		if e != nil {
			t.Fatal(e)
		}
		if result.DaemonId != c.FrontendID() {
			t.Fatal("identity crossed clients")
		}
		if _, e = c.Agent().GetStatus(ctx, &pb.Empty{}); e != nil {
			t.Fatal(e)
		}
	}
	if e := a.Close(); e != nil {
		t.Fatal(e)
	}
	if _, e := b.Agent().GetStatus(ctx, &pb.Empty{}); e != nil {
		t.Fatalf("other frontend closed: %v", e)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.released[a.FrontendID()] || p.released[b.FrontendID()] {
		t.Fatalf("wrong release: %v", p.released)
	}
}

func TestAgentHandshakeRequiresControlCapability(t *testing.T) {
	probe := &frontendProbe{}
	current := frontendFixture(t, probe)()
	probe.omitControl.Store(true)
	if next, err := dial(context.Background(), "unused", current.Config, time.Second); err == nil {
		next.Close()
		t.Fatal("accepted peer without required agent.control.v1")
	}
}
func TestLifecycleTimeoutDefaultsDoNotExtendCaller(t *testing.T) {
	c := &Client{lifetime: context.Background(), timeout: 5 * time.Second}
	for _, test := range []struct {
		procedure string
		timeout   time.Duration
	}{{arpc.AgentGetStatusProcedure, 5 * time.Second}, {arpc.AgentShutdownProcedure, 15 * time.Second}, {rpc.ShikiDaemonCreateSessionProcedure, 15 * time.Second}} {
		next := c.identity("")(func(ctx context.Context, _ connect.Spec) (connect.ClientStream, error) {
			deadline, ok := ctx.Deadline()
			remaining := time.Until(deadline)
			if !ok || remaining > test.timeout || remaining < test.timeout-time.Second {
				t.Fatalf("%s deadline %v", test.procedure, remaining)
			}
			return nil, context.Canceled
		})
		_, _ = next(context.Background(), connect.Spec{Procedure: test.procedure, StreamType: connect.StreamTypeUnary})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	next := c.identity("")(func(ctx context.Context, _ connect.Spec) (connect.ClientStream, error) {
		deadline, _ := ctx.Deadline()
		if time.Until(deadline) > 50*time.Millisecond {
			t.Fatal("caller deadline extended")
		}
		return nil, context.Canceled
	})
	_, _ = next(ctx, connect.Spec{Procedure: arpc.AgentShutdownProcedure, StreamType: connect.StreamTypeUnary})
}

func TestStartupPolicyRejectsOldAgentWithoutCallingRPC(t *testing.T) {
	c := &Client{capabilities: []string{"agent.control.v1"}} // nil RPC client proves no call
	if _, err := c.StartupPolicy(t.Context()); connect.CodeOf(err) != connect.CodeUnimplemented {
		t.Fatal(err)
	}
}
