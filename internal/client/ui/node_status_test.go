//go:build darwin || linux

package ui

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/pkg/config"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	arpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent/agentconnect"
	"github.com/DDSRem-Dev/Shikigami/pkg/protocol/version"
)

type dashboardNodeProbe struct {
	arpc.UnimplementedNodeControlHandler
	state, reason string
	login         bool
	mutations     atomic.Int32
}

func (p *dashboardNodeProbe) Negotiate(_ context.Context, offer *pb.ProtocolOffer) (*apb.NodeNegotiation, error) {
	local := &pb.ProtocolOffer{Major: 1}
	if p.login {
		local.Capabilities = []string{"node.login.v1"}
	}
	agreement, err := version.Negotiate(local, offer, "dashboard-node-test")
	if err != nil {
		return nil, err
	}
	return &apb.NodeNegotiation{Protocol: agreement, InstanceId: "dashboard-node"}, nil
}
func (p *dashboardNodeProbe) GetStatus(ctx context.Context, _ *pb.Empty) (*apb.NodeStatus, error) {
	info, _ := connect.CallInfoForServerContext(ctx)
	if info.RequestHeader().Get("Shiki-Node-Control-Instance-Id") != "dashboard-node" {
		return nil, connect.NewError(connect.CodeFailedPrecondition, "NODE_CONTROL_RESTARTED")
	}
	return &apb.NodeStatus{State: p.state, ReasonCode: p.reason}, nil
}
func (p *dashboardNodeProbe) BeginLogin(context.Context, *apb.BeginLoginRequest) (*apb.NodeStatus, error) {
	p.mutations.Add(1)
	return nil, connect.NewError(connect.CodeFailedPrecondition, "unexpected login")
}
func (p *dashboardNodeProbe) CancelLogin(context.Context, *apb.CancelLoginRequest) (*apb.NodeStatus, error) {
	p.mutations.Add(1)
	return nil, connect.NewError(connect.CodeFailedPrecondition, "unexpected cancel")
}

func TestNodeStatusDashboardUsesRuntimeDiagnosticAndAction(t *testing.T) {
	for _, tc := range []struct {
		state, reason, guidance string
		login                   bool
	}{
		{"offline", "NODE_DISABLED", "disabled", false},
		{"offline", "NODE_STATE_UNAVAILABLE", "repair", false},
		{"needs_login", "NODE_AUTH_REQUIRED", "shiki node login", true},
		{"online", "", "Dashboard exit keeps the agent running.", true},
	} {
		t.Run(tc.state+tc.reason, func(t *testing.T) {
			p := &dashboardNodeProbe{state: tc.state, reason: tc.reason, login: tc.login}
			c, _, _ := dashboardClient(t, p)
			// Neither a frontend snapshot nor a later disk rewrite establishes
			// the running endpoint's state or its supported operations.
			c.Config.Tailscale.Enabled = tc.reason == "NODE_DISABLED"
			if err := config.SaveClient(c.ConfigPath, c.Config); err != nil {
				t.Fatal(err)
			}
			m := New(t.Context(), c, Options{})
			defer m.Close()
			panel := m.loadPanel(t.Context(), 1, 4, "", "")().(panelMsg)
			rows := strings.Join(panel.rows, "\n")
			if panel.err != nil || !strings.Contains(rows, "Embedded node "+tc.state+" · "+tc.reason) || !strings.Contains(rows, tc.guidance) || strings.Contains(rows, "PROTOCOL_INCOMPATIBLE") {
				t.Fatalf("runtime node diagnostic: %s, %v", rows, panel.err)
			}
			if tc.state != "needs_login" && strings.Contains(rows, "shiki node login") {
				t.Fatalf("unavailable or online node suggested login: %s", rows)
			}
			if p.mutations.Load() != 0 {
				t.Fatal("status panel invoked login RPC")
			}
		})
	}
}
