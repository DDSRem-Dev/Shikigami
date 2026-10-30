package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	frontend "github.com/DDSRem-Dev/Shikigami/internal/client"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	"github.com/DDSRem-Dev/Shikigami/pkg/config"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	arpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent/agentconnect"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
)

type timeoutAgent struct {
	arpc.UnimplementedAgentHandler
	next      atomic.Int64
	deadlines chan time.Time
}

func (p *timeoutAgent) Handshake(context.Context, *apb.HandshakeRequest) (*apb.HandshakeResponse, error) {
	return &apb.HandshakeResponse{InstanceId: "instance", FrontendId: fmt.Sprint(p.next.Add(1)),
		Protocol: &pb.ProtocolAgreement{Major: 1, Capabilities: []string{"agent.control.v1"}}}, nil
}
func (*timeoutAgent) GetStatus(context.Context, *pb.Empty) (*apb.AgentStatus, error) {
	return &apb.AgentStatus{InstanceId: "instance", State: "ready"}, nil
}
func (*timeoutAgent) ReleaseFrontend(context.Context, *pb.Empty) (*pb.Empty, error) {
	return &pb.Empty{}, nil
}
func (p *timeoutAgent) ResolveConflict(ctx context.Context, _ *ppb.ResolveConflictRequest) (*ppb.SyncState, error) {
	deadline, ok := ctx.Deadline()
	if !ok {
		return nil, connect.NewError(connect.CodeFailedPrecondition, "missing caller deadline")
	}
	p.deadlines <- deadline
	return &ppb.SyncState{}, nil
}

func TestFrontendExplicitTimeoutIsScopedAndPreservesEarlierCallerDeadline(t *testing.T) {
	root, err := os.MkdirTemp("/tmp", "shiki-fb-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	h := &helper{root: root, config: filepath.Join(root, "client.yaml"), fronts: map[string]*frontend.Client{}}
	cfg := config.DefaultClient()
	cfg.Agent.Socket = filepath.Join(root, "agent.sock")
	cfg.Agent.StateDir = filepath.Join(root, "agent")
	cfg.Agent.AutoStart = false
	cfg.Tailscale.Enabled = false
	cfg.Tailscale.StateDir = filepath.Join(root, "tailscale")
	cfg.PluginsDir = filepath.Join(root, "plugins")
	if err := config.SaveClient(h.config, cfg); err != nil {
		t.Fatal(err)
	}
	listener, err := connection.ListenUnix(cfg.Agent.Socket, uint32(os.Getuid()))
	if err != nil {
		t.Fatal(err)
	}
	probe := &timeoutAgent{deadlines: make(chan time.Time, 8)}
	router := connect.NewServer()
	arpc.RegisterAgentHandler(router, probe)
	lifetime, cancel := context.WithCancel(t.Context())
	server := connection.NewHTTPServer(lifetime, connection.NewRPCHandler(router))
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	t.Cleanup(func() {
		for _, f := range h.fronts {
			_ = f.Close()
		}
		cancel()
		_ = server.Close()
		<-done
	})
	for _, test := range []struct {
		name      string
		seconds   int
		want      time.Duration
		parent    time.Duration
		openAgain bool
	}{
		{"default", 0, 5 * time.Second, 120 * time.Second, true},
		{"collision", 90, 90 * time.Second, 120 * time.Second, true},
		{"default", 0, 5 * time.Second, 120 * time.Second, false},
		{"short", 1, time.Second, 120 * time.Second, true},
		{"collision", 90, 200 * time.Millisecond, 200 * time.Millisecond, false},
	} {
		if test.openAgain {
			data, err := json.Marshal(map[string]any{"name": test.name, "timeout_seconds": test.seconds})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := h.frontendOp(t.Context(), request{Op: "frontend.open", Args: data}); err != nil {
				t.Fatal(err)
			}
		}
		f, err := h.front(t.Context(), test.name)
		if err != nil {
			t.Fatal(err)
		}
		call, stop := context.WithTimeout(t.Context(), test.parent)
		started := time.Now()
		parentDeadline, _ := call.Deadline()
		_, err = f.Agent().ResolveConflict(call, &ppb.ResolveConflictRequest{})
		stop()
		if err != nil {
			t.Fatal(err)
		}
		deadline := <-probe.deadlines
		budget := deadline.Sub(started)
		if budget > test.want+10*time.Millisecond || budget < test.want-150*time.Millisecond {
			t.Fatalf("frontend %s budget=%v want=%v", test.name, budget, test.want)
		}
		if deadline.After(parentDeadline.Add(time.Millisecond)) {
			t.Fatal("frontend extended an earlier caller deadline")
		}
	}
}

func TestFrontendTimeoutRejectsUnboundedAndInvalidValuesBeforeOpening(t *testing.T) {
	for _, value := range []string{"-1", "91", "1.5", `"90"`, "999999999999999999999999999999999"} {
		h := &helper{fronts: map[string]*frontend.Client{}}
		_, err := h.frontendOp(t.Context(), request{Op: "frontend.open", Args: json.RawMessage(`{"name":"collision","timeout_seconds":` + value + `}`)})
		if connect.CodeOf(err) != connect.CodeInvalidArgument || len(h.fronts) != 0 {
			t.Fatalf("accepted invalid timeout %s: %v", value, err)
		}
	}
}
