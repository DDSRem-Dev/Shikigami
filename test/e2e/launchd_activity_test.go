//go:build darwin && e2e

package e2e

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/DDSRem-Dev/Shikigami/internal/install"
	"github.com/DDSRem-Dev/Shikigami/internal/server/tmux"
	"github.com/DDSRem-Dev/Shikigami/pkg/config"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	"google.golang.org/protobuf/proto"
)

// This replaces only a private native LaunchAgent installation. The daemon is
// a TLS fixture process: no macOS native daemon-service claim is made here.
func TestNativeLaunchAgentUpgradeWithActiveTransfers(t *testing.T) {
	root := os.Getenv("SHIKI_LAUNCHD_ACTIVITY_ROOT")
	if root == "" {
		t.Skip("opt-in native activity: scripts/test-launchd-activity.sh")
	}
	if os.Geteuid() == 0 || filepath.Dir(root) != "/private/tmp" || !strings.HasPrefix(filepath.Base(root), "shiki-launchd.") {
		t.Fatal("invalid private native fixture root")
	}
	direction := upgradeTransferDirection(os.Getenv("SHIKI_LAUNCHD_ACTIVITY_DIRECTION"))
	if direction == "" {
		direction = upgradeUpload
	}
	if direction != upgradeUpload && direction != upgradeDownload {
		t.Fatal("native activity direction must be upload or download")
	}
	account, err := user.Current()
	if err != nil || os.Getenv("HOME") != account.HomeDir {
		t.Fatal("actual ordinary-user HOME required", err)
	}
	domain := "gui/" + account.Uid
	target := domain + "/dev.shikigami.agent"
	service := install.ServicePath("client", account.HomeDir)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	t.Cleanup(stop)
	ctx, cancel := context.WithTimeout(ctx, 12*time.Minute)
	t.Cleanup(cancel)
	processes, stopProcesses := context.WithCancel(context.Background())
	t.Cleanup(stopProcesses)
	launchdActivityAbsent(t, ctx, domain, target, service)
	repository := filepath.Join(root, "source")
	fixture := filepath.Join(root, "runtime")
	if err := os.Mkdir(fixture, 0700); err != nil {
		t.Fatal(err)
	}
	// The normal cleanup and shell fallback both check this invocation's exact
	// service/daemon/session ownership; neither ever kills the shared tmux server.
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		out, err := exec.CommandContext(cleanup, "python3", filepath.Join(repository, "scripts/test-launchd-activity-cleanup.py"), root).CombinedOutput()
		if err != nil {
			t.Errorf("owned activity cleanup: %v\n%s", err, out)
		}
	})
	evidence := newLaunchdActivityEvidence(t, ctx, root)
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		for _, record := range evidence.processes {
			if record.PID != os.Getpid() && rotationAlive(cleanup, record.rotationProcessIdentity) {
				t.Errorf("recorded owned process still alive before fallback: %+v", record)
			}
		}
		evidence.record("normal-cleanup", map[string]any{"failed": t.Failed(), "recorded_identities": len(evidence.processes)})
	})
	first, firstPin := buildLaunchdBundle(t, ctx, repository, fixture, "launchd-active-a")
	second, secondPin := buildLaunchdBundle(t, ctx, repository, fixture, "launchd-active-b")
	prefix := filepath.Join(fixture, "install")
	configuration := filepath.Join(fixture, "client.yaml")
	cfg := config.DefaultClient()
	cfg.Tailscale.Enabled = false
	cfg.Tailscale.StateDir = filepath.Join(fixture, "tailscale")
	cfg.Agent.AutoStart = false
	cfg.Agent.Socket = filepath.Join(fixture, "run", "agent.sock")
	cfg.Agent.StateDir = filepath.Join(fixture, "state")
	cfg.PluginsDir = filepath.Join(fixture, "plugins")
	if err := config.SaveClient(configuration, cfg); err != nil {
		t.Fatal(err)
	}
	environment := append(os.Environ(), "GORACE=halt_on_error=1")
	helper := filepath.Join(first, "shiki-install")
	installArgs := []string{"--role", "client", "--prefix", prefix, "--config", configuration, "--state-dir", cfg.Agent.StateDir, "--plugins-dir", cfg.PluginsDir, "--bin-dir", filepath.Join(fixture, "commands"), "--path", "/usr/bin:/bin", "--non-interactive"}
	ready := func(t *testing.T, result install.Result) {
		launchdWait(t, ctx, 30*time.Second, func() error {
			if result.State == "local_application_ready" && result.NodeState == "offline" && result.NodeReason == "NODE_DISABLED" && result.ExternalPeerAcceptance == "pending" {
				return nil
			}
			var err error
			result, err = launchdInstallResult(ctx, environment, helper, "probe", "--prefix", prefix, "--non-interactive")
			if err != nil {
				return err
			}
			return fmt.Errorf("waiting for native local readiness: %s/%s", result.State, result.Reason)
		})
	}
	activate := func(t *testing.T, bundle, pin, version string) install.Result {
		args := append([]string{"install"}, installArgs...)
		args = append(args, "--bundle", bundle, "--checksums-sha256", pin, "--version", version)
		result, err := launchdInstallResult(ctx, environment, helper, args...)
		if err != nil {
			t.Fatal(err)
		}
		ready(t, result)
		return result
	}
	launchdActivityAbsent(t, ctx, domain, target, service) // immediately before first activation
	activate(t, first, firstPin, "launchd-active-a")
	agentPID := launchdPID(t, ctx, target, prefix)
	evidence.process("agent-a", agentPID)
	cli := func(args ...string) []byte {
		return run(t, ctx, environment, filepath.Join(prefix, "current", "shiki"), append([]string{"--config", configuration, "--json"}, args...)...)
	}
	daemonConfig, cert, token, address, ledger, daemon := launchdActivityDaemon(t, ctx, processes, fixture, first, environment, account.Username, evidence)
	cli("connect", "launchd-active", "--tls-address", address, "--tls-server-name", "localhost", "--tls-ca", cert, "--token-file", token)
	project := new(pb.Project)
	decode(t, cli("projects", "create", "active-transfer", "--path", filepath.Join(fixture, "projects", "active-transfer")), project)
	if err := os.WriteFile(filepath.Join(project.Path, "keep.txt"), []byte("retained native-upgrade project"), 0600); err != nil {
		t.Fatal(err)
	}
	session := new(pb.Session)
	decode(t, cli("sessions", "create", "native-active", "--project", project.Id), session)
	ownedSession := launchdActivitySession{ID: session.Id, Fixture: filepath.Join(fixture, "terminal-fixture")}
	// Register cleanup immediately after receiving the new immutable session ID.
	// Pending identity still requires the private fixture start command before
	// removal; a complete pane identity tightens the check once inspected.
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		if err := launchdActivityRemoveSession(cleanup, ownedSession); err != nil {
			t.Error(err)
		}
	})
	evidence.sessionIntent(ownedSession)
	mux, err := tmux.New("")
	if err != nil {
		t.Fatal(err)
	}
	pane, err := mux.Inspect(ctx, session.Id)
	if err != nil {
		t.Fatal(err)
	}
	ownedSession.TmuxID, ownedSession.PaneID, ownedSession.PID = pane.TmuxSessionID, pane.PaneID, pane.PID
	evidence.sessionIntent(ownedSession)
	evidence.process("pane", pane.PID)
	control := filepath.Join(fixture, "control")
	if err := os.Mkdir(control, 0700); err != nil {
		t.Fatal(err)
	}
	a := &upgradeActivity{t: t, ctx: ctx, cfg: upgradeActivityConfig{CLI: filepath.Join(prefix, "current", "shiki"), ClientConfig: configuration, Server: "launchd-active", ProjectID: project.Id, SessionID: session.Id, RemoteRoot: project.Path, LocalRoot: filepath.Join(fixture, "active-files"), Ledger: ledger, Control: control, TLSAddress: address, TokenFile: token, TLSCert: cert}}
	cli("plugins", "enable", "sync", "--side", "remote", "--server", "launchd-active")
	cli("plugins", "enable", "sync", "--side", "local")
	setupFront := a.client()
	selected, err := setupFront.ResolveServer(ctx, "launchd-active")
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []*apb.PluginTarget{{Side: apb.PluginTarget_LOCAL}, {Side: apb.PluginTarget_REMOTE, ServerId: selected.Config.Id}} {
		a.wait(20*time.Second, func(call context.Context) error {
			list, err := setupFront.Agent().ListPlugins(call, target)
			if err != nil {
				return err
			}
			for _, status := range list.Plugins {
				if status.PluginId == "sync" && status.Enabled && status.State == "ready" {
					return nil
				}
			}
			return errors.New("sync worker is not ready")
		})
	}
	setupFront.Close()
	a.proxy = newUpgradeProxy(t, ctx, address)
	a.echo = newUpgradeEcho(t)
	a.prepare()
	// Additional durable fixtures deliberately remain unresolved/paused/stopped
	// across BOTH installer boundaries, independently of the active flow fixture.
	front := a.client()
	h := &defaultRotationHarness{t: t, ctx: ctx, root: fixture, agentConfig: cfg, daemonConfig: daemonConfig, front: front, local: front.Agent(), remote: front.Remote(a.mapping.ServerId), serverID: a.mapping.ServerId}
	persistent := h.prepareSync()
	stopped, err := h.local.CreateForward(ctx, &apb.CreateForwardRequest{RequestId: rand.Text(), Spec: &ppb.ForwardSpec{ServerId: h.serverID, RemoteAddress: "127.0.0.1", RemotePort: a.forward.Spec.RemotePort, LocalAddress: "127.0.0.1", Enabled: true}})
	if err != nil {
		t.Fatal(err)
	}
	stopRequest := rand.Text()
	if _, err := h.local.StopForward(ctx, &apb.StopForwardRequest{ForwardId: stopped.Spec.Id, ExpectedRevision: stopped.Spec.Revision, RequestId: stopRequest}); err != nil {
		t.Fatal(err)
	}
	durablePort := &rotationPortWork{active: proto.Clone(a.forward.Spec).(*ppb.ForwardSpec), stopped: proto.Clone(stopped.Spec).(*ppb.ForwardSpec), stopRequest: stopRequest}
	h.verifySync(persistent)
	h.durableRotation(persistent, durablePort)
	stable := evidence.children("server", daemon.command.Process.Pid, first)
	stable = append(stable, evidence.process("stable-daemon", daemon.command.Process.Pid), evidence.process("stable-pane", pane.PID))
	firstServer, err := h.remote.GetSystemInfo(ctx, &pb.Empty{})
	if err != nil {
		t.Fatal(err)
	}
	var backup string
	for index, label := range []string{"agent-upgrade", "agent-rollback"} {
		if !t.Run(label, func(t *testing.T) {
			phase := *a
			phase.t = t
			parentT := evidence.t
			evidence.t = t
			defer func() { evidence.t = parentT; h.t = parentT }()
			prior := phase.client()
			beforeInstance := prior.InstanceID()
			oldPID := launchdPID(t, ctx, target, prefix)
			oldWorkers := evidence.children(label+"-old", oldPID, prefix)
			oldSockets, _ := filepath.Glob(filepath.Join(cfg.Agent.StateDir, "plugin-run", "p-*", "core.sock"))
			if len(oldSockets) != 2 {
				t.Fatal("two old CoreAPI sockets required", oldSockets)
			}
			phase.boundary = func(boundary string) {
				evidence.control(control, boundary+".ready")
				if index == 0 {
					result := activate(t, second, secondPin, "launchd-active-b")
					backup = result.Backup
					if backup == "" {
						t.Fatal("upgrade omitted backup")
					}
					if info, err := os.Stat(backup); err != nil || info.Mode().Perm() != 0600 {
						t.Fatal("backup must be private", err)
					}
				} else {
					result, err := launchdInstallResult(ctx, environment, helper, "rollback", "--prefix", prefix, "--non-interactive")
					if err != nil || result.Version != "launchd-active-a" {
						t.Fatal("native rollback failed", result, err)
					}
					ready(t, result)
				}
				oldPIDs := []int{oldPID}
				for _, identity := range oldWorkers {
					oldPIDs = append(oldPIDs, identity.PID)
				}
				launchdWaitGone(t, ctx, oldPIDs)
				for _, socket := range oldSockets {
					if _, err := os.Lstat(socket); !errors.Is(err, os.ErrNotExist) {
						t.Fatal("old private socket retained", socket, err)
					}
				}
				evidence.process(label+"-new-agent", launchdPID(t, ctx, target, prefix))
				evidence.children(label+"-new", launchdPID(t, ctx, target, prefix), prefix)
				if err := os.WriteFile(filepath.Join(control, boundary+".after"), []byte("native installer switched\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			phase.exercise(label, direction, upgradeAgent)
			recovered := phase.client()
			if recovered.InstanceID() == beforeInstance {
				t.Fatal("installer reused agent instance")
			}
			if _, err := prior.Agent().GetStatus(ctx, &pb.Empty{}); err == nil {
				t.Fatal("old frontend accepted after native replacement")
			}
			h.t = t
			h.front = recovered
			h.local = recovered.Agent()
			h.remote = recovered.Remote(h.serverID)
			h.verifySync(persistent)
			h.durableRotation(persistent, durablePort)
			nowServer, err := h.remote.GetSystemInfo(ctx, &pb.Empty{})
			if err != nil || nowServer.DaemonId != firstServer.DaemonId {
				t.Fatal("native agent switch changed daemon identity", err)
			}
			for _, identity := range stable {
				if !rotationAlive(ctx, identity) {
					t.Fatal("unrelated TLS daemon/worker/pane replaced", identity)
				}
			}
			currentPane, err := mux.Inspect(ctx, session.Id)
			if err != nil || currentPane.PaneID != pane.PaneID || currentPane.TmuxSessionID != pane.TmuxSessionID || currentPane.PID != pane.PID {
				t.Fatal("original pane identity changed", err)
			}
			evidence.control(control, label+".verified")
			evidence.record(label+"-durable", map[string]any{"paused": persistent.paused.Id, "conflict": persistent.conflict.Id, "generation": persistent.conflict.Generation, "stopped_forward": stopped.Spec.Id, "stop_request": stopRequest, "daemon_id": firstServer.DaemonId, "original_pane": pane, "old_processes_reaped": true, "old_sockets_removed": true})
		}) {
			return
		}
	}
	a.cli("sync", "pause", a.mapping.Id)
	a.cli("ports", "stop", a.forward.Spec.Id)
	a.cli("servers", "update", a.cfg.Server, "--tls-address", address)
	// Uninstall must preserve application state; test-owned temp-tree deletion is
	// a separate cleanup after the retained-data assertions.
	finalPID := launchdPID(t, ctx, target, prefix)
	finalChildren := launchdPluginPIDs(t, ctx, finalPID, prefix)
	removed, err := launchdInstallResult(ctx, environment, helper, "uninstall", "--prefix", prefix, "--non-interactive")
	if err != nil || removed.State != "uninstalled" {
		t.Fatal("native uninstall", removed, err)
	}
	launchdWaitGone(t, ctx, append(finalChildren, finalPID))
	launchdAssertUnlocked(t, cfg.Agent.StateDir)
	for _, path := range []string{configuration, filepath.Join(cfg.Agent.StateDir, "resources", "state.json"), backup, filepath.Join(project.Path, "keep.txt"), token} {
		if _, err := os.Stat(path); err != nil {
			t.Fatal("uninstall removed retained data", path, err)
		}
	}
	launchdActivityAbsent(t, ctx, domain, target, service)
	evidence.record("application-complete", map[string]any{"boundaries": 2, "sync_direction": direction, "replaced_host": upgradeAgent, "native_service": "LaunchAgent", "daemon_service": "ordinary TLS fixture only", "external_acceptance": "pending"})
}
