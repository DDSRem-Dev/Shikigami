//go:build darwin && e2e

package e2e

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	frontend "github.com/DDSRem-Dev/Shikigami/internal/client"
	"github.com/DDSRem-Dev/Shikigami/internal/install"
	"github.com/DDSRem-Dev/Shikigami/pkg/config"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
)

// Opt-in only: this exercises the actual standard Native LaunchAgent service,
// not a mock manager or a changed HOME. scripts/test-launchd.sh freezes source
// and supplies a second cleanup layer if the test process is terminated.
func TestNativeLaunchAgentLifecycle(t *testing.T) {
	root := os.Getenv("SHIKI_LAUNCHD_TEST_ROOT")
	if root == "" {
		t.Skip("opt-in real LaunchAgent test: run scripts/test-launchd.sh")
	}
	if os.Geteuid() == 0 || filepath.Dir(root) != "/private/tmp" || !strings.HasPrefix(filepath.Base(root), "shiki-launchd.") {
		t.Fatal("invalid ordinary-user test root")
	}
	account, err := user.Current()
	if err != nil || os.Getenv("HOME") != account.HomeDir {
		t.Fatal("test must preserve the actual account HOME", err)
	}
	domain := "gui/" + account.Uid
	target := domain + "/dev.shikigami.agent"
	service := install.ServicePath("client", account.HomeDir)
	ctx, stop := signal.NotifyContext(t.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 6*time.Minute)
	defer cancel()
	if err = exec.CommandContext(ctx, "/bin/launchctl", "print", domain).Run(); err != nil {
		t.Fatal("GUI launchd domain unavailable", err)
	}
	if _, err = os.Lstat(service); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("existing plist must never be replaced", err)
	}
	if err = exec.CommandContext(ctx, "/bin/launchctl", "print", target).Run(); err == nil {
		t.Fatal("existing launchd job must never be stopped or replaced")
	}
	t.Logf("preflight: user=%s uid=%s HOME=%s; standard job and plist absent", account.Username, account.Uid, account.HomeDir)
	repository := filepath.Join(root, "source")
	t.Cleanup(func() {
		cleanup, done := context.WithTimeout(context.Background(), 40*time.Second)
		defer done()
		out, err := exec.CommandContext(cleanup, "python3", filepath.Join(repository, "scripts/test-launchd-cleanup.py"), root).CombinedOutput()
		if err != nil {
			t.Errorf("LaunchAgent cleanup: %v\n%s", err, out)
		}
	})
	fixture := filepath.Join(root, "runtime")
	if err = os.Mkdir(fixture, 0700); err != nil {
		t.Fatal(err)
	}
	prefix := filepath.Join(fixture, "install")
	configuration := filepath.Join(fixture, "client.yaml")
	cfg := config.DefaultClient()
	cfg.Tailscale.Enabled = false
	cfg.Tailscale.StateDir = filepath.Join(fixture, "tailscale")
	cfg.Agent.AutoStart = false
	cfg.Agent.Socket = filepath.Join(fixture, "run", "agent.sock")
	cfg.Agent.StateDir = filepath.Join(fixture, "state")
	cfg.PluginsDir = filepath.Join(fixture, "plugins")
	if err = config.SaveClient(configuration, cfg); err != nil {
		t.Fatal(err)
	}
	environment := append(os.Environ(), "GORACE=halt_on_error=1")
	first, firstPin := buildLaunchdBundle(t, ctx, repository, fixture, "launchd-test-a")
	second, secondPin := buildLaunchdBundle(t, ctx, repository, fixture, "launchd-test-b")
	// Repeat immediately before activation after the potentially long build.
	if _, err = os.Lstat(service); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("service appeared during build; refusing activation", err)
	}
	if err = exec.CommandContext(ctx, "/bin/launchctl", "print", target).Run(); err == nil {
		t.Fatal("job appeared during build; refusing activation")
	}
	helper := filepath.Join(first, "shiki-install")
	installArgs := []string{"--role", "client", "--prefix", prefix, "--config", configuration, "--state-dir", cfg.Agent.StateDir, "--plugins-dir", cfg.PluginsDir, "--bin-dir", filepath.Join(fixture, "commands"), "--path", "/usr/bin:/bin", "--non-interactive"}
	ready := func(result install.Result) {
		t.Logf("installer result: state=%s reason=%s node=%s application=%s", result.State, result.Reason, result.NodeState, result.ApplicationState)
		launchdWait(t, ctx, 30*time.Second, func() error {
			if result.State == "local_application_ready" && result.NodeState == "offline" && result.NodeReason == "NODE_DISABLED" && result.ExternalPeerAcceptance == "pending" {
				return nil
			}
			var err error
			result, err = launchdInstallResult(ctx, environment, helper, "probe", "--prefix", prefix, "--non-interactive")
			if err != nil {
				return err
			}
			if result.State != "local_application_ready" || result.NodeState != "offline" || result.NodeReason != "NODE_DISABLED" || result.ExternalPeerAcceptance != "pending" {
				return fmt.Errorf("application not ready: %+v", result)
			}
			return nil
		})
	}
	activate := func(bundle, pin, version string) install.Result {
		args := append([]string{"install"}, installArgs...)
		args = append(args, "--bundle", bundle, "--checksums-sha256", pin, "--version", version)
		result, err := launchdInstallResult(ctx, environment, helper, args...)
		if err != nil {
			t.Fatal(err)
		}
		ready(result)
		return result
	}
	activate(first, firstPin, "launchd-test-a")
	plan := install.Plan{Role: "client", User: account.Username, Home: account.HomeDir, Path: "/usr/bin:/bin", Config: configuration}
	expected, err := install.RenderService(plan, prefix)
	if err != nil {
		t.Fatal(err)
	}
	written, err := os.ReadFile(service)
	if err != nil || string(written) != string(expected) {
		t.Fatal("Native installer did not write the exact production service for custom paths", err)
	}
	cli := func(args ...string) []byte {
		return run(t, ctx, environment, filepath.Join(prefix, "current", "shiki"), append([]string{"--config", configuration, "--json"}, args...)...)
	}
	open := func() *frontend.Client {
		c, err := frontend.Open(ctx, configuration, frontend.Options{AutoStart: false, Timeout: time.Second})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { c.Close() })
		return c
	}
	initial := open()
	firstID := initial.InstanceID()
	pid := launchdPID(t, ctx, target, prefix)
	if launchdLockPID(t, cfg.Agent.StateDir) != pid {
		t.Fatal("launchd and agent instance lock identify different processes")
	}
	for _, plugin := range []string{"sync", "port"} {
		cli("plugins", "enable", plugin, "--side", "local")
	}
	children := launchdPluginPIDs(t, ctx, pid, prefix)
	if len(children) != 2 {
		t.Fatalf("expected two real plugin children, got %v", children)
	}
	cli("config", "set", "no_color", "true")
	sentinel := filepath.Join(cfg.Agent.StateDir, "retention-marker")
	if err = os.WriteFile(sentinel, []byte("preserve launchd test data"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Logf("native bootstrap ready: agent=%d instance=%s; enabled plugin PIDs=%v", pid, firstID, children)
	if err = syscall.Kill(pid, syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	launchdWait(t, ctx, 35*time.Second, func() error {
		c, err := frontend.Open(ctx, configuration, frontend.Options{AutoStart: false, Timeout: time.Second})
		if err != nil {
			return err
		}
		defer c.Close()
		if c.InstanceID() == firstID {
			return errors.New("launchd has not replaced the crashed instance")
		}
		_, err = c.Agent().GetStatus(ctx, &pb.Empty{})
		return err
	})
	launchdWaitGone(t, ctx, append(children, pid))
	restarted := open()
	secondID := restarted.InstanceID()
	if _, err = initial.Agent().GetStatus(ctx, &pb.Empty{}); err == nil {
		t.Fatal("old frontend accepted after the host instance changed")
	}
	initial.Close()
	launchdWaitPlugins(t, ctx, restarted)
	pid = launchdPID(t, ctx, target, prefix)
	children = launchdPluginPIDs(t, ctx, pid, prefix)
	t.Logf("SIGKILL recovery: new agent=%d instance=%s; original host/plugins gone", pid, secondID)
	restarted = launchdCheckCrashLoop(t, ctx, environment, configuration, cfg, prefix, target, restarted)
	secondID = restarted.InstanceID()
	pid = launchdPID(t, ctx, target, prefix)
	children = launchdPluginPIDs(t, ctx, pid, prefix)
	cli("agent", "stop")
	restarted.Close()
	launchdWaitGone(t, ctx, append(children, pid))
	launchdAssertUnlocked(t, cfg.Agent.StateDir)
	// KeepAlive's default throttle is 10s. Observe beyond it without making
	// any auto-starting CLI calls, so an unintended respawn cannot be hidden.
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		data, err := exec.CommandContext(ctx, "/bin/launchctl", "print", target).Output()
		if err != nil || strings.Contains(string(data), "\n\tpid = ") || !strings.Contains(string(data), "last exit code = 0") {
			t.Fatalf("successful stop respawned or failed: %v\n%s", err, data)
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(250 * time.Millisecond):
		}
	}
	t.Log("explicit agent stop: exit 0, host/plugins gone, locks released, no respawn for 15s")
	upgrade := activate(second, secondPin, "launchd-test-b")
	if upgrade.Backup == "" {
		t.Fatal("upgrade did not record a backup")
	}
	if info, err := os.Stat(upgrade.Backup); err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		t.Fatal("upgrade backup is missing or not private", err)
	}
	upgraded := open()
	if upgraded.InstanceID() == secondID {
		t.Fatal("upgrade reused the stopped agent instance")
	}
	launchdWaitPlugins(t, ctx, upgraded)
	launchdAssertPreferences(t, ctx, upgraded)
	upgraded.Close()
	rolled, err := launchdInstallResult(ctx, environment, helper, "rollback", "--prefix", prefix, "--non-interactive")
	if err != nil || rolled.Version != "launchd-test-a" {
		t.Fatal("native rollback failed", rolled, err)
	}
	ready(rolled)
	rolledClient := open()
	launchdWaitPlugins(t, ctx, rolledClient)
	launchdAssertPreferences(t, ctx, rolledClient)
	pid = launchdPID(t, ctx, target, prefix)
	children = launchdPluginPIDs(t, ctx, pid, prefix)
	rolledClient.Close()
	var removed install.Result
	if err = json.Unmarshal(run(t, ctx, environment, helper, "uninstall", "--prefix", prefix, "--non-interactive"), &removed); err != nil || removed.State != "uninstalled" {
		t.Fatal("native uninstall failed", removed, err)
	}
	launchdWaitGone(t, ctx, append(children, pid))
	launchdAssertUnlocked(t, cfg.Agent.StateDir)
	if _, err = os.Lstat(service); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("uninstall retained the service file", err)
	}
	if err = exec.CommandContext(ctx, "/bin/launchctl", "print", target).Run(); err == nil {
		t.Fatal("uninstall retained a registered service")
	}
	for _, removedPath := range []string{filepath.Join(prefix, "current"), cfg.PluginsDir, filepath.Join(fixture, "commands", "shiki"), filepath.Join(fixture, "commands", "shiki-agent")} {
		if _, err = os.Lstat(removedPath); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("uninstall retained managed link", removedPath, err)
		}
	}
	if data, err := os.ReadFile(sentinel); err != nil || string(data) != "preserve launchd test data" {
		t.Fatal("uninstall deleted private state", err)
	}
	if saved, err := config.LoadClient(configuration); err != nil || saved.Agent.Socket != cfg.Agent.Socket || saved.Tailscale.Enabled {
		t.Fatal("uninstall changed retained private config", err)
	}
	for _, retained := range []string{upgrade.Backup, filepath.Join(cfg.Agent.StateDir, "resources", "state.json"), filepath.Join(prefix, "releases", "launchd-test-a-darwin-"+runtime.GOARCH), filepath.Join(prefix, "releases", "launchd-test-b-darwin-"+runtime.GOARCH)} {
		if _, err := os.Stat(retained); err != nil {
			t.Fatal("uninstall removed retained backup/state/release", retained, err)
		}
	}
	t.Log("native upgrade A→B, rollback B→A, and uninstall passed; custom configuration, preferences, state and backups retained")
}

func launchdAssertPreferences(t *testing.T, ctx context.Context, c *frontend.Client) {
	t.Helper()
	prefs, err := c.Agent().GetPreferences(ctx, &pb.Empty{})
	if err != nil || !prefs.NoColor {
		t.Fatal("upgrade/rollback lost saved preferences", err)
	}
}

func launchdLockPID(t *testing.T, state string) int {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(state, "instance.lock"))
	pid, parseErr := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || parseErr != nil || pid <= 1 {
		t.Fatal("invalid agent lock PID", err, parseErr)
	}
	return pid
}

func launchdWaitPlugins(t *testing.T, ctx context.Context, c *frontend.Client) {
	t.Helper()
	launchdWait(t, ctx, 15*time.Second, func() error {
		list, err := c.Agent().ListPlugins(ctx, &apb.PluginTarget{Side: apb.PluginTarget_LOCAL})
		if err != nil {
			return err
		}
		ready := 0
		for _, plugin := range list.Plugins {
			if plugin.Enabled && plugin.State == "ready" {
				ready++
			}
		}
		if ready != 2 {
			return fmt.Errorf("enabled plugin readiness count=%d", ready)
		}
		return nil
	})
}
