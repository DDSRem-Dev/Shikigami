package client

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	arpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent/agentconnect"
	"github.com/DDSRem-Dev/Shikigami/pkg/protocol/version"
)

type statusOnlyNodeProbe struct {
	arpc.UnimplementedNodeControlHandler
	offer      *pb.ProtocolOffer
	reply      *apb.NodeNegotiation
	reason     string
	restarted  bool
	block      string
	entered    chan struct{}
	negotiates atomic.Int32
	statuses   atomic.Int32
	mutations  atomic.Int32
}

func (p *statusOnlyNodeProbe) Negotiate(ctx context.Context, offer *pb.ProtocolOffer) (*apb.NodeNegotiation, error) {
	p.negotiates.Add(1)
	if err := p.headers(ctx, false); err != nil {
		return nil, err
	}
	if p.block == "negotiate" {
		close(p.entered)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if p.reply != nil {
		return p.reply, nil
	}
	local := p.offer
	if local == nil {
		local = &pb.ProtocolOffer{Major: 1}
	}
	agreement, err := version.Negotiate(local, offer, "status-only-test")
	if err != nil {
		return nil, err
	}
	return &apb.NodeNegotiation{Protocol: agreement, InstanceId: "status-node"}, nil
}
func (p *statusOnlyNodeProbe) headers(ctx context.Context, status bool) error {
	uid, ok := connection.PeerUID(ctx)
	info, _ := connect.CallInfoForServerContext(ctx)
	h := info.RequestHeader()
	if !ok || uid != uint32(os.Getuid()) || h.Get("Authorization") != "" || h.Get(frontendHeader) != "" || h.Get("X-Secret") != "" {
		return connect.NewError(connect.CodePermissionDenied, "UID/header isolation failed")
	}
	want := ""
	if status {
		want = "status-node"
		if p.restarted {
			want = "replacement-node"
		}
	}
	if h.Get("Shiki-Node-Control-Instance-Id") != want {
		return connect.NewError(connect.CodeFailedPrecondition, "NODE_CONTROL_RESTARTED")
	}
	return nil
}
func (p *statusOnlyNodeProbe) GetStatus(ctx context.Context, _ *pb.Empty) (*apb.NodeStatus, error) {
	p.statuses.Add(1)
	if err := p.headers(ctx, true); err != nil {
		return nil, err
	}
	if p.block == "status" {
		close(p.entered)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	state := "offline"
	if p.reason == "NODE_AUTH_REQUIRED" {
		state = "needs_login"
	}
	return &apb.NodeStatus{State: state, ReasonCode: p.reason}, nil
}
func (p *statusOnlyNodeProbe) BeginLogin(context.Context, *apb.BeginLoginRequest) (*apb.NodeStatus, error) {
	p.mutations.Add(1)
	return nil, connect.NewError(connect.CodeFailedPrecondition, "unexpected BeginLogin")
}
func (p *statusOnlyNodeProbe) CancelLogin(context.Context, *apb.CancelLoginRequest) (*apb.NodeStatus, error) {
	p.mutations.Add(1)
	return nil, connect.NewError(connect.CodeFailedPrecondition, "unexpected CancelLogin")
}

func statusOnlyNodeSocket(t *testing.T, p *statusOnlyNodeProbe) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "shiki-status-probe-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	socket := filepath.Join(dir, "node.sock")
	listener, err := connection.ListenUnix(socket, uint32(os.Getuid()))
	if err != nil {
		t.Fatal(err)
	}
	server := connect.NewServer()
	arpc.RegisterNodeControlHandler(server, p)
	hs := connection.NewHTTPServer(t.Context(), connection.NewRPCHandler(server))
	done := make(chan error, 1)
	go func() { done <- hs.Serve(listener) }()
	t.Cleanup(func() { hs.Close(); <-done })
	return socket
}

func TestGetNodeStatusIndependentDiagnosticsWithoutLogin(t *testing.T) {
	for _, reason := range []string{"NODE_DISABLED", "NODE_STATE_UNAVAILABLE", "NODE_AUTH_REQUIRED"} {
		t.Run(reason, func(t *testing.T) {
			p := &statusOnlyNodeProbe{reason: reason}
			if reason == "NODE_AUTH_REQUIRED" {
				p.offer = &pb.ProtocolOffer{Major: 1, Capabilities: []string{"node.login.v1"}}
			}
			socket := statusOnlyNodeSocket(t, p)
			ctx, info := connect.NewClientContext(t.Context())
			info.RequestHeader().Set("Authorization", "Bearer caller-secret")
			info.RequestHeader().Set(frontendHeader, "caller-frontend")
			info.RequestHeader().Set("X-Secret", "caller-secret")
			info.RequestHeader().Set("Shiki-Node-Control-Instance-Id", "caller-instance")
			status, err := GetNodeStatus(ctx, socket, time.Second)
			if err != nil || status.GetReasonCode() != reason || p.statuses.Load() != 1 || p.mutations.Load() != 0 {
				t.Fatalf("status-only operation: %v, %v, reads=%d writes=%d", status, err, p.statuses.Load(), p.mutations.Load())
			}
			node, err := OpenNode(ctx, socket, time.Second)
			if node != nil {
				defer node.Close()
			}
			if reason == "NODE_AUTH_REQUIRED" {
				if err != nil {
					t.Fatalf("pre-login capability unavailable: %v", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), "PROTOCOL_INCOMPATIBLE") {
				t.Fatalf("login gate bypassed: %v", err)
			}
			if p.mutations.Load() != 0 {
				t.Fatal("diagnostic negotiation invoked a login RPC")
			}
		})
	}
}

func TestGetNodeStatusRejectsIncompatibleOrChangedIdentity(t *testing.T) {
	for _, tc := range []struct {
		name  string
		probe *statusOnlyNodeProbe
		want  string
		reads int32
	}{
		{"major", &statusOnlyNodeProbe{offer: &pb.ProtocolOffer{Major: 2}}, "PROTOCOL_INCOMPATIBLE", 0},
		{"minor", &statusOnlyNodeProbe{offer: &pb.ProtocolOffer{Major: 1, MinMinor: 1, MaxMinor: 1}}, "PROTOCOL_INCOMPATIBLE", 0},
		{"missing-instance", &statusOnlyNodeProbe{reply: &apb.NodeNegotiation{Protocol: &pb.ProtocolAgreement{Major: 1}}}, "invalid node control negotiation", 0},
		{"invalid-major-reply", &statusOnlyNodeProbe{reply: &apb.NodeNegotiation{Protocol: &pb.ProtocolAgreement{Major: 2}, InstanceId: "status-node"}}, "invalid node control negotiation", 0},
		{"invalid-minor-reply", &statusOnlyNodeProbe{reply: &apb.NodeNegotiation{Protocol: &pb.ProtocolAgreement{Major: 1, Minor: 1}, InstanceId: "status-node"}}, "invalid node control negotiation", 0},
		{"missing-protocol", &statusOnlyNodeProbe{reply: &apb.NodeNegotiation{InstanceId: "status-node"}}, "invalid node control negotiation", 0},
		{"changed-instance", &statusOnlyNodeProbe{restarted: true}, "NODE_CONTROL_RESTARTED", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := tc.probe
			status, err := GetNodeStatus(t.Context(), statusOnlyNodeSocket(t, p), time.Second)
			if err == nil || !strings.Contains(err.Error(), tc.want) || status != nil || p.statuses.Load() != tc.reads || p.mutations.Load() != 0 {
				t.Fatalf("invalid endpoint accepted: %v, %v, reads=%d writes=%d", status, err, p.statuses.Load(), p.mutations.Load())
			}
		})
	}
}

func TestGetNodeStatusDeadlineCancellationAndSocketPermissions(t *testing.T) {
	for _, stage := range []string{"negotiate", "status"} {
		for _, cancelEarly := range []bool{false, true} {
			name := stage + "/deadline"
			if cancelEarly {
				name = stage + "/cancel"
			}
			t.Run(name, func(t *testing.T) {
				p := &statusOnlyNodeProbe{block: stage, entered: make(chan struct{})}
				socket := statusOnlyNodeSocket(t, p)
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				timeout := 250 * time.Millisecond
				if cancelEarly {
					timeout = 5 * time.Second
				}
				done := make(chan error, 1)
				go func() { _, err := GetNodeStatus(ctx, socket, timeout); done <- err }()
				select {
				case <-p.entered:
				case <-time.After(2 * time.Second):
					t.Fatal("request did not reach the bounded endpoint")
				}
				want := connect.CodeDeadlineExceeded
				if cancelEarly {
					cancel()
					want = connect.CodeCanceled
				}
				select {
				case err := <-done:
					if connect.CodeOf(err) != want {
						t.Fatalf("bounded request: %v, want %v", err, want)
					}
				case <-time.After(2 * time.Second):
					t.Fatal("node request ignored deadline or cancellation")
				}
				if p.mutations.Load() != 0 {
					t.Fatal("bounded status invoked login")
				}
			})
		}
	}
	p := &statusOnlyNodeProbe{}
	socket := statusOnlyNodeSocket(t, p)
	if _, err := GetNodeStatus(t.Context(), socket, 0); err == nil {
		t.Fatal("nonpositive timeout accepted")
	}
	if err := os.Chmod(socket, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := GetNodeStatus(t.Context(), socket, time.Second); err == nil || p.negotiates.Load() != 0 {
		t.Fatalf("public socket accepted: %v", err)
	}
}
