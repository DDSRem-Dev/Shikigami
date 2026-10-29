//go:build darwin && e2e

package e2e

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	frontend "github.com/DDSRem-Dev/Shikigami/internal/client"
	"github.com/DDSRem-Dev/Shikigami/pkg/config"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
)

// The caller has already observed one unexpected exit. Exercise the same
// persisted three-crash limit under launchd before upgrade changes the build.
func launchdCheckCrashLoop(t *testing.T, ctx context.Context, environment []string, configuration string, cfg *config.Client, prefix, target string, current *frontend.Client) *frontend.Client {
	t.Helper()
	await := func(previous, state, reason string) *frontend.Client {
		var result *frontend.Client
		launchdWait(t, ctx, 35*time.Second, func() error {
			client, err := frontend.Open(ctx, configuration, frontend.Options{AutoStart: false, Timeout: time.Second})
			if err != nil {
				return err
			}
			status, err := client.Agent().GetStatus(ctx, &pb.Empty{})
			if err == nil && client.InstanceID() != previous && status.State == state && status.ReasonCode == reason {
				result = client
				return nil
			}
			client.Close()
			return fmt.Errorf("expected new agent state=%s reason=%s: status=%v error=%v", state, reason, status, err)
		})
		t.Cleanup(func() { result.Close() })
		return result
	}
	for crash := 2; crash <= 3; crash++ {
		pid := launchdPID(t, ctx, target, prefix)
		children := launchdPluginPIDs(t, ctx, pid, prefix)
		if len(children) != 2 {
			t.Fatalf("before crash %d expected two plugin children: %v", crash, children)
		}
		previous := current.InstanceID()
		current.Close()
		if err := syscall.Kill(pid, syscall.SIGKILL); err != nil {
			t.Fatal(err)
		}
		launchdWaitGone(t, ctx, append(children, pid))
		state, reason := "ready", ""
		if crash == 3 {
			state, reason = "degraded", "AGENT_CRASH_LOOP"
		}
		current = await(previous, state, reason)
		if crash == 2 {
			launchdWaitPlugins(t, ctx, current)
		}
	}
	blockedPID := launchdPID(t, ctx, target, prefix)
	blockedInstance := current.InstanceID()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		status, err := current.Agent().GetStatus(ctx, &pb.Empty{})
		if err != nil || status.State != "degraded" || status.ReasonCode != "AGENT_CRASH_LOOP" || launchdPID(t, ctx, target, prefix) != blockedPID || len(launchdPluginPIDs(t, ctx, blockedPID, prefix)) != 0 {
			t.Fatalf("crash guard did not remain a stable diagnostic process: status=%v error=%v", status, err)
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(250 * time.Millisecond):
		}
	}
	binary := filepath.Join(prefix, "current", "shiki")
	ordinary := exec.CommandContext(ctx, binary, "--config", configuration, "--json", "agent", "start")
	ordinary.Env = environment
	if output, err := ordinary.CombinedOutput(); err == nil || !strings.Contains(string(output), "AGENT_CRASH_LOOP") {
		t.Fatalf("ordinary agent start bypassed the crash guard: %v %s", err, output)
	}
	if launchdPID(t, ctx, target, prefix) != blockedPID {
		t.Fatal("ordinary start replaced the diagnostic process")
	}
	run(t, ctx, environment, binary, "--config", configuration, "--json", "agent", "start", "--recover-crash-loop")
	current.Close()
	launchdWaitGone(t, ctx, []int{blockedPID})
	recovered := await(blockedInstance, "ready", "")
	launchdWaitPlugins(t, ctx, recovered)
	manualPID := launchdLockPID(t, cfg.Agent.StateDir)
	children := launchdPluginPIDs(t, ctx, manualPID, prefix)
	if manualPID == blockedPID || len(children) != 2 {
		t.Fatal("explicit recovery did not create a new ready host with its workers")
	}
	launchdWait(t, ctx, 10*time.Second, func() error {
		data, err := exec.CommandContext(ctx, "/bin/launchctl", "print", target).Output()
		if err != nil || strings.Contains(string(data), "\n\tpid = ") || !strings.Contains(string(data), "last exit code = 0") {
			return errors.New("diagnostic service did not exit successfully before explicit CLI recovery")
		}
		return nil
	})
	run(t, ctx, environment, binary, "--config", configuration, "--json", "agent", "stop")
	previous := recovered.InstanceID()
	recovered.Close()
	launchdWaitGone(t, ctx, append(children, manualPID))
	launchdAssertUnlocked(t, cfg.Agent.StateDir)
	// Explicit recovery launched a CLI-owned host. Restore service ownership
	// before the remaining normal stop/upgrade/rollback assertions.
	if output, err := exec.CommandContext(ctx, "/bin/launchctl", "kickstart", target).CombinedOutput(); err != nil {
		t.Fatalf("return recovered agent to launchd: %v %s", err, output)
	}
	managed := await(previous, "ready", "")
	launchdWaitPlugins(t, ctx, managed)
	if launchdPID(t, ctx, target, prefix) != launchdLockPID(t, cfg.Agent.StateDir) {
		t.Fatal("recovered service did not regain process ownership")
	}
	t.Logf("three SIGKILLs: stable degraded/%s PID=%d with no workers for 15s; ordinary start refused; explicit recovery ready; normal stop and launchd ownership restored", "AGENT_CRASH_LOOP", blockedPID)
	return managed
}
