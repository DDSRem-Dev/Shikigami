//go:build e2e

package e2e

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net"
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
	"github.com/DDSRem-Dev/Shikigami/internal/server/tmux"
	"github.com/DDSRem-Dev/Shikigami/pkg/config"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	arpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent/agentconnect"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
	"github.com/DDSRem-Dev/Shikigami/test/fixtures/resources"
	"google.golang.org/protobuf/types/known/structpb"
)

// All credential timers are owned by unchanged shipped entrypoints. This
// harness has no production lifetime option, environment override or fake clock.
type defaultRotationHarness struct {
	t                                  *testing.T
	ctx                                context.Context
	root, bin, configuration, serverID string
	agentConfig                        *config.Client
	daemonConfig                       *config.Daemon
	front                              *frontend.Client
	local                              arpc.AgentClient
	remote                             rpc.ShikiDaemonClient
	parents                            map[string]int
	allPIDs                            []int
	identities                         []rotationProcessIdentity
	events                             *os.File
	cleanupVerified                    bool
	cli                                func(...string) []byte
}

func newDefaultRotationHarness(t *testing.T) *defaultRotationHarness {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Fatal("ordinary user required: a root skip is not acceptance")
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Fatal(err)
	}
	parent, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	ctx, cancel := context.WithTimeout(parent, 12*time.Hour+50*time.Minute)
	t.Cleanup(func() { cancel(); stopSignals() })
	processContext, stopProcesses := context.WithCancel(context.Background())
	t.Cleanup(stopProcesses)
	root, err := os.MkdirTemp("/tmp", "shiki-default-rotation-")
	if err != nil {
		t.Fatal(err)
	}
	h := &defaultRotationHarness{t: t, ctx: ctx, root: root, bin: filepath.Join(root, "bin"), parents: map[string]int{}}
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("failed private rotation runtime retained: %s", root)
			if evidence := os.Getenv("SHIKI_RUN_EVIDENCE_DIR"); evidence != "" {
				if !h.cleanupVerified {
					t.Errorf("private runtime copy withheld: owned cleanup not fully verified; original retained at %s", root)
					return
				}
				retained, err := retainRotationRuntime(root, evidence)
				if err != nil {
					t.Errorf("private runtime retention %s: %v", retained, err)
				} else {
					t.Logf("private runtime copied into acceptance evidence: %s", retained)
				}
			}
			return
		}
		if err := os.RemoveAll(root); err != nil {
			t.Error(err)
		}
	})
	if evidence := os.Getenv("SHIKI_RUN_EVIDENCE_DIR"); evidence != "" {
		h.events, err = os.OpenFile(filepath.Join(evidence, "harness-events.jsonl"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := h.events.Close(); err != nil {
				t.Error(err)
			}
		})
	}
	h.identity("test", os.Getpid())
	h.event("fixture", map[string]any{"root": root, "build_mode": "ordinary", "production_credential_lifetime": "12h", "certificate_lifetime": "24h", "daemon_token_lifetime": "24h"})
	if err := os.Mkdir(h.bin, 0700); err != nil {
		t.Fatal(err)
	}
	_, source, _, _ := runtime.Caller(0)
	repository := filepath.Clean(filepath.Join(filepath.Dir(source), "../.."))
	for _, item := range []struct {
		output   string
		packages []string
	}{{h.bin + string(filepath.Separator), []string{"./cmd/shiki", "./cmd/shiki-agent", "./cmd/shiki-daemon"}}, {filepath.Join(h.bin, "shiki-plugin-sync"), []string{"./plugins/sync"}}, {filepath.Join(h.bin, "shiki-plugin-port"), []string{"./plugins/port"}}} {
		args := append([]string{"build", "-buildvcs=false", "-mod=readonly", "-o", item.output}, item.packages...)
		command := exec.CommandContext(ctx, "go", args...)
		command.Dir = repository
		if out, err := command.CombinedOutput(); err != nil {
			t.Fatalf("ordinary shipped build: %v\n%s", err, out)
		}
	}
	for _, name := range []string{"shiki", "shiki-agent", "shiki-daemon", "shiki-plugin-sync", "shiki-plugin-port"} {
		data, err := os.ReadFile(filepath.Join(h.bin, name))
		if err != nil {
			t.Fatal(err)
		}
		h.event("binary", map[string]any{"path": filepath.Join(h.bin, name), "sha256": fmt.Sprintf("%x", sha256.Sum256(data))})
	}
	writeSyncManifest(t, h.bin)
	writePortManifest(t, h.bin)
	account, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	environment := []string{"PATH=" + h.bin + string(os.PathListSeparator) + os.Getenv("PATH"), "HOME=" + root, "USER=" + account.Username, "TERM=xterm-256color", "LANG=C.UTF-8"}
	cert, key, _ := defaultRotationCertificate(t, root)
	reservation, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := reservation.Addr().String()
	reservation.Close()
	fixture := filepath.Join(root, "terminal-fixture")
	if err := os.WriteFile(fixture, []byte("#!/bin/sh\nif [ \"${1:-}\" = --version ]; then printf '0.0.0-rotation-fixture\\n'; exit 0; fi\nprintf 'E2E_READY\\n'\nwhile IFS= read -r line; do printf 'E2E_REPLY:%s\\n' \"$line\"; done\n"), 0700); err != nil {
		t.Fatal(err)
	}
	dc := filepath.Join(root, "daemon.yaml")
	db := filepath.Join(h.bin, "shiki-daemon")
	cb := filepath.Join(h.bin, "shiki")
	run(t, ctx, environment, db, "init", "--config", dc, "--user", account.Username, "--state-dir", filepath.Join(root, "remote-state"), "--projects-dir", filepath.Join(root, "projects"), "--tailscale=false", "--direct-tls-address", address, "--tls-cert", cert, "--tls-key", key, "--claude-executable", fixture)
	h.daemonConfig, err = config.LoadDaemon(dc)
	if err != nil {
		t.Fatal(err)
	}
	h.daemonConfig.PluginsDir = h.bin
	if err := config.SaveDaemon(dc, h.daemonConfig); err != nil {
		t.Fatal(err)
	}
	token := filepath.Join(root, "token")
	run(t, ctx, environment, db, "token", "create", "--config", dc, "--output", token, "--expires", "24h")
	daemon := start(t, processContext, environment, filepath.Join(root, "daemon.log"), db, "serve", "--config", dc)
	h.parents["server"] = daemon.command.Process.Pid
	h.identity("daemon", h.parents["server"])
	h.agentConfig = config.DefaultClient()
	h.agentConfig.Agent.AutoStart = false
	h.agentConfig.Agent.Socket = filepath.Join(root, "agent.sock")
	h.agentConfig.Agent.StateDir = filepath.Join(root, "agent")
	h.agentConfig.Tailscale.Enabled = false
	h.agentConfig.Tailscale.StateDir = filepath.Join(root, "tsnet")
	h.agentConfig.PluginsDir = h.bin
	h.configuration = filepath.Join(root, "client.yaml")
	if err := config.SaveClient(h.configuration, h.agentConfig); err != nil {
		t.Fatal(err)
	}
	h.cli = func(args ...string) []byte {
		return run(t, ctx, environment, cb, append([]string{"--config", h.configuration, "--json"}, args...)...)
	}
	// Explicitly own the shipped agent in the runner process group. CLI autostart
	// is disabled so a failed agent cannot silently become an unregistered epoch.
	agent := start(t, processContext, environment, filepath.Join(root, "agent.log"), filepath.Join(h.bin, "shiki-agent"), "--config", h.configuration)
	h.parents["client"] = agent.command.Process.Pid
	h.identity("agent", h.parents["client"])
	h.allPIDs = append(h.allPIDs, h.parents["client"], h.parents["server"])

	t.Cleanup(func() {
		if h.front != nil {
			h.front.Close()
		}
		cleanup, stop := context.WithTimeout(context.Background(), 20*time.Second)
		defer stop()
		command := exec.CommandContext(cleanup, cb, "--config", h.configuration, "agent", "stop")
		command.Env = environment
		if output, err := command.CombinedOutput(); err != nil {
			t.Errorf("agent cleanup: %v %s", err, output)
		}
		agent.wait(t, cleanup, false)
		_ = daemon.command.Process.Signal(syscall.SIGTERM)
		daemon.wait(t, cleanup, false)
		resources.Reaped(t, h.allPIDs...)
		socketsAbsent := true
		for _, side := range []string{"client", "server"} {
			paths, _ := filepath.Glob(filepath.Join(h.runDir(side), "p-*", "core.sock"))
			if len(paths) != 0 {
				socketsAbsent = false
				t.Errorf("residual private sockets: %v", paths)
			}
		}
		h.cleanupVerified = socketsAbsent
		h.event("owned_cleanup", map[string]any{"pids": h.allPIDs, "failed": t.Failed(), "cleanup_verified": h.cleanupVerified})
	})
	h.await("owned agent ready", func(call context.Context) error {
		front, err := frontend.Open(call, h.configuration, frontend.Options{Timeout: time.Second})
		if err != nil {
			return err
		}
		defer front.Close()
		status, err := front.Agent().GetStatus(call, &pb.Empty{})
		if err != nil {
			return err
		}
		if status.State != "ready" {
			return errors.New("agent starting")
		}
		return nil
	})
	h.cli("connect", "rotation", "--tls-address", address, "--tls-server-name", "localhost", "--tls-ca", cert, "--token-file", token)
	h.front, err = frontend.Open(ctx, h.configuration, frontend.Options{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	h.local = h.front.Agent()
	selected, err := h.front.ResolveServer(ctx, "rotation")
	if err != nil {
		t.Fatal(err)
	}
	h.serverID = selected.Config.Id
	h.remote = h.front.Remote(h.serverID)
	pidText, err := os.ReadFile(filepath.Join(h.agentConfig.Agent.StateDir, "instance.lock"))
	if err != nil {
		t.Fatal(err)
	}
	recordedPID, err := strconv.Atoi(strings.TrimSpace(string(pidText)))
	if err != nil {
		t.Fatal(err)
	}
	if recordedPID != h.parents["client"] {
		t.Fatal("CLI connected to an unowned agent")
	}
	for _, side := range []string{"client", "server"} {
		for _, id := range []string{"sync", "port"} {
			h.manage(side, id, pb.ManagePluginRequest_DISABLE, nil)
			h.await("initial disabled worker", func(call context.Context) error {
				status := h.plugin(side, id)
				if status.State != "stopped" {
					return errors.New("worker still stopping")
				}
				return nil
			})
		}
	}
	return h
}

func (h *defaultRotationHarness) event(kind string, value any) {
	h.t.Helper()
	record := map[string]any{"time": time.Now().UTC(), "event": kind, "value": value}
	data, err := json.Marshal(record)
	if err != nil {
		h.t.Fatal(err)
	}
	h.t.Logf("DEFAULT_ROTATION_EVENT %s", data)
	if h.events != nil {
		if _, err := h.events.Write(append(data, '\n')); err != nil {
			h.t.Fatal(err)
		}
		if err := h.events.Sync(); err != nil {
			h.t.Fatal(err)
		}
	}
}
func (h *defaultRotationHarness) id() string { return rand.Text() }
func (h *defaultRotationHarness) target(side string) *apb.PluginTarget {
	if side == "client" {
		return &apb.PluginTarget{Side: apb.PluginTarget_LOCAL}
	}
	return &apb.PluginTarget{Side: apb.PluginTarget_REMOTE, ServerId: h.serverID}
}
func (h *defaultRotationHarness) runDir(side string) string {
	if side == "client" {
		return filepath.Join(h.agentConfig.Agent.StateDir, "plugin-run")
	}
	return filepath.Join(h.daemonConfig.StateDir, "run", "p")
}
func (h *defaultRotationHarness) await(label string, check func(context.Context) error) {
	h.t.Helper()
	ctx, cancel := context.WithTimeout(h.ctx, 20*time.Second)
	defer cancel()
	var last error
	for ctx.Err() == nil {
		call, stop := context.WithTimeout(ctx, 2*time.Second)
		last = check(call)
		stop()
		if last == nil {
			return
		}
		select {
		case <-ctx.Done():
		case <-time.After(25 * time.Millisecond):
		}
	}
	h.t.Fatalf("%s: %v (last %v)", label, ctx.Err(), last)
}
func (h *defaultRotationHarness) plugin(side, id string) *pb.PluginStatus {
	h.t.Helper()
	ctx, cancel := context.WithTimeout(h.ctx, 3*time.Second)
	defer cancel()
	list, err := h.local.ListPlugins(ctx, h.target(side))
	if err != nil {
		h.t.Fatal(err)
	}
	for _, s := range list.Plugins {
		if s.PluginId == id {
			return s
		}
	}
	h.t.Fatal("missing plugin", side, id)
	return nil
}
func (h *defaultRotationHarness) manage(side, id string, action pb.ManagePluginRequest_Action, values *structpb.Struct) {
	h.t.Helper()
	status := h.plugin(side, id)
	if _, err := h.local.ManagePlugin(h.ctx, &apb.ManagePluginRequest{Target: h.target(side), Request: &pb.ManagePluginRequest{PluginId: id, Action: action, Config: values, ExpectedRevision: status.Revision, RequestId: h.id()}}); err != nil {
		h.t.Fatal(err)
	}
}
func (h *defaultRotationHarness) terminal() (*deviceTerminal, *pb.Session) {
	project, err := h.remote.CreateProject(h.ctx, &pb.CreateProjectRequest{Name: "terminal", Path: filepath.Join(h.daemonConfig.ProjectsDir, "terminal"), RequestId: h.id()})
	if err != nil {
		h.t.Fatal(err)
	}
	session := new(pb.Session)
	decode(h.t, h.cli("sessions", "create", "rotation-terminal", "--project", project.Id), session)
	mux, err := tmux.New("")
	if err != nil {
		h.t.Fatal(err)
	}
	h.t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := mux.Delete(ctx, session.Id); err != nil {
			h.t.Error(err)
		}
	})
	terminal := openDeviceTerminal(h.t, h.ctx, h.front, h.serverID, session.Id, false)
	terminal.expectMode(h.t, "controller")
	terminal.mu.Lock()
	terminal.textLimit = 128 << 10
	terminal.mu.Unlock()
	h.t.Cleanup(func() { terminal.stream.Close() })
	session, err = h.remote.GetSession(h.ctx, &pb.GetSessionRequest{Id: session.Id})
	if err != nil || session.GetClaudePid() <= 1 {
		h.t.Fatal("terminal fixture not running", err)
	}
	h.allPIDs = append(h.allPIDs, int(session.ClaudePid))
	return terminal, session
}
