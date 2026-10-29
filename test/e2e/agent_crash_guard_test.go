//go:build e2e

package e2e

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	frontend "github.com/DDSRem-Dev/Shikigami/internal/client"
	"github.com/DDSRem-Dev/Shikigami/pkg/config"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
)

func TestAgentCrashLoopSharedByCLIAndProcessStarts(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	directory, err := os.MkdirTemp("/tmp", "shiki-crash-e2e-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(directory) })
	bin := filepath.Join(directory, "bin")
	if err := os.Mkdir(bin, 0700); err != nil {
		t.Fatal(err)
	}
	_, source, _, _ := runtime.Caller(0)
	repository := filepath.Clean(filepath.Join(filepath.Dir(source), "../.."))
	build := exec.CommandContext(ctx, "go", "build", "-race", "-buildvcs=false", "-mod=readonly", "-o", bin+string(filepath.Separator), "./cmd/shiki", "./cmd/shiki-agent")
	build.Dir = repository
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	cfg := config.DefaultClient()
	cfg.Tailscale.Enabled = false
	cfg.Tailscale.StateDir = filepath.Join(directory, "node")
	cfg.Agent.StateDir = filepath.Join(directory, "state")
	cfg.Agent.Socket = filepath.Join(directory, "run", "agent.sock")
	cfg.PluginsDir = filepath.Join(directory, "plugins")
	configuration := filepath.Join(directory, "client.yaml")
	if err := config.SaveClient(configuration, cfg); err != nil {
		t.Fatal(err)
	}
	environment := append(os.Environ(), "GORACE=halt_on_error=1 log_path="+filepath.Join(directory, "race"))
	cliBinary := filepath.Join(bin, "shiki")
	agentBinary := filepath.Join(bin, "shiki-agent")
	cli := func(call context.Context, args ...string) ([]byte, error) {
		command := exec.CommandContext(call, cliBinary, append([]string{"--config", configuration, "--json"}, args...)...)
		command.Env = environment
		return command.CombinedOutput()
	}
	t.Cleanup(func() {
		cleanup, done := context.WithTimeout(context.Background(), 15*time.Second)
		defer done()
		_, _ = cli(cleanup, "agent", "stop")
		reports, err := filepath.Glob(filepath.Join(directory, "race.*"))
		if err != nil || len(reports) > 0 {
			t.Errorf("race-enabled child produced reports: %v, %v", reports, err)
		}
	})
	status := func(want string) *apb.AgentStatus {
		t.Helper()
		var result *apb.AgentStatus
		await(t, ctx, func(call context.Context) error {
			client, err := frontend.Open(call, configuration, frontend.Options{Timeout: time.Second})
			if err != nil {
				return err
			}
			defer client.Close()
			result, err = client.Agent().GetStatus(call, &pb.Empty{})
			if err == nil && result.State != want {
				return fmt.Errorf("state=%s, want=%s", result.State, want)
			}
			return err
		})
		return result
	}
	// Real SIGKILL cannot execute the normal completion path. Both direct
	// service-style starts and later independent CLI processes share the record.
	for attempt := 0; attempt < 3; attempt++ {
		child := start(t, ctx, environment, filepath.Join(directory, fmt.Sprintf("agent-%d.log", attempt)), agentBinary, "--config", configuration)
		status("ready")
		if err := child.command.Process.Signal(syscall.SIGKILL); err != nil {
			t.Fatal(err)
		}
		child.wait(t, ctx, true)
	}
	var callers sync.WaitGroup
	for range 4 {
		callers.Go(func() {
			out, err := cli(ctx, "agent", "start")
			if err == nil || !strings.Contains(string(out), "AGENT_CRASH_LOOP") {
				t.Errorf("ordinary CLI bypassed crash loop: %v\n%s", err, out)
			}
		})
	}
	callers.Wait()
	blocked := status("degraded")
	if blocked.ReasonCode != "AGENT_CRASH_LOOP" || blocked.ActiveForwards != 0 || blocked.ActiveSyncTasks != 0 || blocked.ActiveTerminals != 0 {
		t.Fatalf("incorrect diagnostic mode: %v", blocked)
	}
	client, err := frontend.Open(ctx, configuration, frontend.Options{Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Agent().ListServers(ctx, &pb.Empty{}); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatal("diagnostic mode exposed background resource state", err)
	}
	client.Close()
	// Normal stop must preserve the latch; another ordinary CLI still fails.
	if out, err := cli(ctx, "agent", "stop"); err != nil {
		t.Fatalf("diagnostic stop: %v\n%s", err, out)
	}
	if out, err := cli(ctx, "agent", "start"); err == nil || !strings.Contains(string(out), "AGENT_CRASH_LOOP") {
		t.Fatalf("normal stop bypassed latch: %v\n%s", err, out)
	}
	if out, err := cli(ctx, "agent", "start", "--recover-crash-loop"); err != nil {
		t.Fatalf("explicit recovery: %v\n%s", err, out)
	}
	recovered := status("ready")
	if recovered.InstanceId == blocked.InstanceId {
		t.Fatal("recovery reused the diagnostic process identity")
	}
	if out, err := cli(ctx, "agent", "start", "--recover-crash-loop"); err == nil || !strings.Contains(string(out), "not in a recoverable crash loop") {
		t.Fatalf("recovery silently restarted a healthy agent: %v\n%s", err, out)
	}
	if out, err := cli(ctx, "agent", "stop"); err != nil {
		t.Fatalf("recovered stop: %v\n%s", err, out)
	}
	if out, err := cli(ctx, "agent", "start"); err != nil {
		t.Fatalf("clean stop consumed crash budget: %v\n%s", err, out)
	}
	status("ready")
	if _, err := os.Stat(filepath.Join(cfg.Agent.StateDir, "startup.json")); errors.Is(err, os.ErrNotExist) {
		t.Fatal("startup evidence not persisted")
	}
}
