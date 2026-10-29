package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	"github.com/DDSRem-Dev/Shikigami/internal/node"
	"github.com/DDSRem-Dev/Shikigami/pkg/config"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	agentpb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	agentrpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent/agentconnect"
)

func runtimeConfig(t *testing.T) (*config.Client, string) {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "shiki-runtime-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	cfg := config.DefaultClient()
	cfg.Agent.StateDir = filepath.Join(dir, "agent")
	cfg.Agent.Socket = filepath.Join(dir, "run", "agent.sock")
	cfg.Tailscale.StateDir = filepath.Join(dir, "node")
	cfg.Tailscale.Enabled = false
	path := filepath.Join(dir, "config.yaml")
	if err = config.SaveClient(path, cfg); err != nil {
		t.Fatal(err)
	}
	return cfg, path
}
func awaitRuntime(t *testing.T, ctx context.Context, cfg *config.Client) (agentrpc.AgentClient, *connect.Client, *agentpb.HandshakeResponse) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		endpoint, err := connection.NewUnixClient(cfg.Agent.Socket, uint32(os.Getuid()), "shiki-agent")
		if err == nil {
			raw := connection.NewRPCClient(endpoint)
			client := agentrpc.NewAgentClient(raw)
			handshake, err := client.Handshake(ctx, &agentpb.HandshakeRequest{Protocol: &pb.ProtocolOffer{Major: 1}})
			if err == nil && handshake.Status.State != "starting" {
				t.Cleanup(endpoint.Close)
				return client, raw, handshake
			}
			endpoint.Close()
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
	t.Fatal("agent did not become locally queryable")
	return nil, nil, nil
}
func TestRuntimeReadinessShutdownAndCorruptDiagnostics(t *testing.T) {
	cfg, path := runtimeConfig(t)
	for _, corrupt := range []bool{false, true} {
		if corrupt {
			if err := os.WriteFile(filepath.Join(cfg.Agent.StateDir, "resources", "state.json"), []byte("truncated"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		done := make(chan error, 1)
		go func() { done <- Run(ctx, path, "test") }()
		client, raw, front := awaitRuntime(t, ctx, cfg)
		wanted := "ready"
		if corrupt {
			wanted = "degraded"
		}
		if front.Status.State != wanted {
			t.Fatalf("state=%s wanted=%s", front.Status.State, wanted)
		}
		nc := agentrpc.NewNodeControlClient(raw)
		negotiation, err := nc.Negotiate(ctx, &pb.ProtocolOffer{Major: 1})
		if err != nil {
			t.Fatal(err)
		}
		nodeCtx, info := connect.NewClientContext(ctx)
		info.RequestHeader().Set(node.NodeInstanceHeader, negotiation.InstanceId)
		status, err := nc.GetStatus(nodeCtx, &pb.Empty{})
		if err != nil || status.ReasonCode != "NODE_DISABLED" {
			t.Fatal("disabled node diagnostics unavailable")
		}
		if err = RunConfig(ctx, cfg, "duplicate"); !errors.Is(err, ErrAlreadyRunning) {
			t.Fatalf("second agent started: %v", err)
		}
		localCtx := callContext(ctx, front, "")
		forwards, forwardErr := client.ListForwards(localCtx, &pb.Empty{})
		if corrupt {
			if connect.CodeOf(forwardErr) != connect.CodeFailedPrecondition {
				t.Fatal("corrupt forward state exposed", forwardErr)
			}
		} else if forwardErr != nil || len(forwards.Forwards) != 0 {
			t.Fatal("fresh agent must list no forwards", forwards, forwardErr)
		}
		if corrupt {
			if _, err = client.UpsertServer(localCtx, &agentpb.UpsertServerRequest{Config: &agentpb.ServerConfig{Name: "blocked", DirectTlsAddress: "localhost:9527", TlsServerName: "localhost"}, RequestId: "blocked"}); connect.CodeOf(err) != connect.CodeFailedPrecondition {
				t.Fatal("corrupt state accepted mutation")
			}
		}
		if _, err = client.Shutdown(localCtx, &agentpb.ShutdownRequest{ExpectedInstanceId: "wrong", RequestId: "stale"}); connect.CodeOf(err) != connect.CodeFailedPrecondition {
			t.Fatal("stale shutdown accepted")
		}
		stopping, err := client.Shutdown(localCtx, &agentpb.ShutdownRequest{ExpectedInstanceId: front.InstanceId, RequestId: "stop"})
		if err != nil || stopping.State != "stopping" {
			t.Fatalf("shutdown response: %v", err)
		}
		select {
		case err = <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-ctx.Done():
			t.Fatal("agent did not exit after Shutdown")
		}
		cancel()
		if _, err = os.Lstat(cfg.Agent.Socket); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("listener socket retained after shutdown")
		}
		lock, err := AcquireInstance(cfg.Agent.StateDir, cfg.Agent.Socket)
		if err != nil {
			t.Fatal("instance lock remained held")
		}
		lock.Close()
	}
}
