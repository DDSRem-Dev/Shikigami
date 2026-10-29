package agent

import (
	"context"
	"os"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/node"
	"github.com/DDSRem-Dev/Shikigami/pkg/config"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	agentpb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	agentrpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent/agentconnect"
)

func TestDiagnosticNodeDoesNotPromiseLogin(t *testing.T) {
	for _, unavailable := range []bool{false, true} {
		name, reason := "disabled", "NODE_DISABLED"
		if unavailable {
			name, reason = "unavailable", "NODE_STATE_UNAVAILABLE"
		}
		t.Run(name, func(t *testing.T) {
			cfg, path := runtimeConfig(t)
			if unavailable {
				cfg.Tailscale.Enabled = true
				// A regular file cannot be an identity directory. The runtime
				// must preserve it and expose diagnostics without starting tsnet.
				if err := os.WriteFile(cfg.Tailscale.StateDir, []byte("preserve identity evidence"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := config.SaveClient(path, cfg); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			done := make(chan error, 1)
			go func() { done <- Run(ctx, path, "diagnostic-test") }()
			t.Cleanup(func() {
				cancel()
				select {
				case err := <-done:
					if err != nil {
						t.Errorf("diagnostic runtime shutdown: %v", err)
					}
				case <-time.After(5 * time.Second):
					t.Error("diagnostic runtime did not exit")
				}
			})
			_, raw, front := awaitRuntime(t, ctx, cfg)
			if unavailable && (front.Status.State != "degraded" || front.Status.ReasonCode != reason) {
				t.Fatalf("unavailable node not diagnosed: %v", front.Status)
			}
			control := agentrpc.NewNodeControlClient(raw)
			offer := &pb.ProtocolOffer{Major: 1, Capabilities: []string{"node.login.v1"}}
			hello, err := control.Negotiate(ctx, offer)
			if err != nil {
				t.Fatal(err)
			}
			if hello.InstanceId == "" || hello.Protocol.GetMajor() != 1 || len(hello.Protocol.GetCapabilities()) != 0 {
				t.Fatalf("diagnostic endpoint promised unavailable capabilities: %v", hello)
			}
			offer.RequiredCapabilities = []string{"node.login.v1"}
			if _, err := control.Negotiate(ctx, offer); connect.CodeOf(err) != connect.CodeFailedPrecondition {
				t.Fatalf("required login capability accepted: %v", err)
			}
			call, info := connect.NewClientContext(ctx)
			info.RequestHeader().Set(node.NodeInstanceHeader, hello.InstanceId)
			status, err := control.GetStatus(call, &pb.Empty{})
			if err != nil || status.GetState() != "offline" || status.GetReasonCode() != reason {
				t.Fatalf("diagnostics unavailable: %v, %v", status, err)
			}
			if _, err := control.BeginLogin(call, &agentpb.BeginLoginRequest{RequestId: "unavailable", Method: &agentpb.BeginLoginRequest_Interactive{Interactive: true}}); connect.CodeOf(err) != connect.CodeFailedPrecondition {
				t.Fatalf("diagnostic endpoint accepted login: %v", err)
			}
			if _, err := control.CancelLogin(call, &agentpb.CancelLoginRequest{}); connect.CodeOf(err) != connect.CodeFailedPrecondition {
				t.Fatalf("diagnostic endpoint accepted cancellation: %v", err)
			}
			if unavailable {
				data, err := os.ReadFile(cfg.Tailscale.StateDir)
				if err != nil || string(data) != "preserve identity evidence" {
					t.Fatalf("invalid identity evidence changed: %q, %v", data, err)
				}
			}
		})
	}
}
