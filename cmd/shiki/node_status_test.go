package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DDSRem-Dev/Shikigami/internal/agent"
	"github.com/DDSRem-Dev/Shikigami/internal/client"
	"github.com/DDSRem-Dev/Shikigami/pkg/config"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
)

// This regression uses only APIs that existed before the status-only client
// fix, so the same test can demonstrate the old CLI's behavioral failure.
func TestNodeStatusCommandsReadRunningAgentAfterDiskConfigChanges(t *testing.T) {
	for _, unavailable := range []bool{false, true} {
		reason := "NODE_DISABLED"
		if unavailable {
			reason = "NODE_STATE_UNAVAILABLE"
		}
		t.Run(reason, func(t *testing.T) {
			path, cfg := nodeStatusRuntime(t, unavailable)
			// The disk now claims the opposite setting. Only the running local
			// NodeControl endpoint can establish which diagnostic is true.
			cfg.Tailscale.Enabled = !unavailable
			if err := config.SaveClient(path, cfg); err != nil {
				t.Fatal(err)
			}
			for _, args := range [][]string{{"doctor"}, {"node", "status"}, {"node"}, {"node", "status", "--socket", cfg.Agent.Socket}} {
				for _, structured := range []bool{false, true} {
					name := strings.Join(args[:min(2, len(args))], "_")
					if structured {
						name += "_json"
					}
					t.Run(name, func(t *testing.T) {
						var out, stderr bytes.Buffer
						command := newRoot(nil, &out, &stderr)
						flags := []string{"--config", path}
						if structured {
							flags = append(flags, "--json")
						}
						command.SetArgs(append(flags, args...))
						if err := command.ExecuteContext(t.Context()); err != nil {
							t.Fatal(err)
						}
						text := out.String()
						if !strings.Contains(text, "offline") || !strings.Contains(text, reason) || strings.Contains(text, "PROTOCOL_INCOMPATIBLE") {
							t.Fatalf("runtime diagnostic lost: %s", text)
						}
						if structured && args[0] == "doctor" {
							var result struct {
								Agent     *apb.AgentStatus  `json:"agent"`
								Servers   *apb.ServerList   `json:"servers"`
								Node      *apb.NodeStatus   `json:"node"`
								NodeError string            `json:"node_error"`
								Errors    map[string]string `json:"errors"`
							}
							if err := json.Unmarshal(out.Bytes(), &result); err != nil || result.Node.GetState() != "offline" || result.Node.GetReasonCode() != reason || result.NodeError != "" {
								t.Fatalf("doctor diagnostic: %s, %v", text, err)
							}
							if unavailable && (!strings.Contains(result.Errors["servers"], "AGENT_STATE_UNAVAILABLE") || result.Servers != nil || result.Agent.GetState() != "degraded") {
								t.Fatalf("unavailable resource inventory shown as empty: %s", text)
							}
						}
						if !structured && args[0] == "doctor" && unavailable && (!strings.Contains(text, "Registered servers: unavailable") || strings.Contains(text, "Registered servers: 0")) {
							t.Fatalf("unavailable resource inventory shown as empty: %s", text)
						}
					})
				}
			}
			for _, args := range [][]string{{"node", "login"}, {"node", "cancel", "attempt"}} {
				var out, stderr bytes.Buffer
				command := newRoot(nil, &out, &stderr)
				command.SetArgs(append([]string{"--config", path}, args...))
				if err := command.ExecuteContext(t.Context()); err == nil || !strings.Contains(err.Error(), "PROTOCOL_INCOMPATIBLE") {
					t.Fatalf("login capability gate changed: %v, %v", args, err)
				}
			}
			if unavailable {
				data, err := os.ReadFile(cfg.Tailscale.StateDir)
				if err != nil || string(data) != "preserve node identity evidence" {
					t.Fatalf("node identity evidence changed: %q, %v", data, err)
				}
			}
		})
	}
}

func nodeStatusRuntime(t *testing.T, unavailable bool) (string, *config.Client) {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "shiki-node-status-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	cfg := config.DefaultClient()
	cfg.Agent.StateDir, cfg.Agent.Socket = filepath.Join(dir, "state"), filepath.Join(dir, "run/agent.sock")
	cfg.Agent.AutoStart = false
	cfg.PluginsDir = filepath.Join(dir, "plugins")
	cfg.Tailscale.Enabled, cfg.Tailscale.StateDir = unavailable, filepath.Join(dir, "identity")
	if unavailable {
		if err := os.WriteFile(cfg.Tailscale.StateDir, []byte("preserve node identity evidence"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(dir, "config.yaml")
	if err := config.SaveClient(path, cfg); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	done := make(chan error, 1)
	go func() { done <- agent.Run(ctx, path, "node-status-test") }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("agent shutdown: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("agent did not stop")
		}
	})
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); {
		c, err := client.Open(ctx, path, client.Options{Timeout: time.Second})
		if err == nil {
			c.Close()
			return path, cfg
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
	t.Fatal("agent did not become ready for diagnostics")
	return "", nil
}
