package hostrotation_test

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DDSRem-Dev/Shikigami/internal/agent"
	frontend "github.com/DDSRem-Dev/Shikigami/internal/client"
	host "github.com/DDSRem-Dev/Shikigami/internal/plugin"
	"github.com/DDSRem-Dev/Shikigami/internal/server"
	"github.com/DDSRem-Dev/Shikigami/pkg/config"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	"github.com/DDSRem-Dev/Shikigami/pkg/plugin/buildinfo"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	arpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent/agentconnect"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
	"github.com/DDSRem-Dev/Shikigami/test/fixtures/resources"
	"google.golang.org/protobuf/types/known/structpb"
)

const credentialLifetime = 30 * time.Second

type harness struct {
	t            *testing.T
	ctx          context.Context
	root, side   string
	agentConfig  *config.Client
	daemonConfig *config.Daemon
	daemon       *server.Runtime
	front        *frontend.Client
	local        arpc.AgentClient
	remote       rpc.ShikiDaemonClient
	serverID     string
	sequence     int
	pids         []int
}

func TestRealHostCredentialRotation(t *testing.T) {
	root, binaries := rotationArtifacts(t)
	for _, side := range []string{"client", "server"} {
		t.Run(side, func(t *testing.T) {
			h := startHosts(t, filepath.Join(root, side), side, binaries)
			exerciseRotation(h)
		})
	}
}

func rotationArtifacts(t *testing.T) (string, map[string][]byte) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Fatal("real daemon acceptance requires an ordinary user; use scripts/test-linux.sh")
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Fatal("real daemon acceptance requires tmux", err)
	}
	root, err := os.MkdirTemp("/tmp", "shiki-hr-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	binaries := map[string][]byte{}
	for _, id := range []string{"sync", "port"} {
		path := filepath.Join(root, id)
		command := exec.CommandContext(t.Context(), "go", "build", "-race", "-o", path, "../../../plugins/"+id)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("build %s: %v\n%s", id, err, output)
		}
		binaries[id], err = os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
	}
	return root, binaries
}

func TestNegativeCredentialLifetimeRejectedBeforeStartingHosts(t *testing.T) {
	root := t.TempDir()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	client := config.DefaultClient()
	client.Agent.StateDir = filepath.Join(root, "agent-state")
	client.Agent.Socket = filepath.Join(root, "agent.sock")
	client.PluginsDir = filepath.Join(root, "plugins")
	client.Tailscale.Enabled = false
	client.Tailscale.StateDir = filepath.Join(root, "agent-node")
	want := "plugin credential lifetime cannot be negative"
	if err := agent.RunConfigWithOptions(ctx, client, "dev", agent.RuntimeOptions{PluginCredentialLifetime: -time.Second}); err == nil || err.Error() != want {
		t.Fatal("agent did not reject the negative option itself", err)
	}
	account, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	daemon := config.DefaultDaemon(account.Username, filepath.Join(root, "daemon-state"))
	daemon.ProjectsDir = filepath.Join(root, "projects")
	daemon.PluginsDir = filepath.Join(root, "plugins")
	daemon.Tailscale.Enabled = false
	runtime, err := server.StartDaemon(ctx, daemon, server.DaemonOptions{PluginCredentialLifetime: -time.Second})
	if runtime != nil {
		t.Cleanup(func() { runtime.Close() })
	}
	if err == nil || err.Error() != want || runtime != nil {
		t.Fatal("daemon did not reject the negative option itself", err)
	}
	for _, path := range []string{client.Agent.StateDir, client.Agent.Socket, daemon.StateDir} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("invalid option created runtime state", path, err)
		}
	}
}

func startHosts(t *testing.T, root, side string, binaries map[string][]byte) *harness {
	return startHostsVia(t, root, side, binaries, nil)
}

// A test-only route may relay TLS without replacing any host or plugin handler.
func startHostsVia(t *testing.T, root, side string, binaries map[string][]byte, route func(context.Context, string, string, string) string) *harness {
	t.Helper()
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Second)
	t.Cleanup(cancel)
	h := &harness{t: t, ctx: ctx, root: root, side: side}
	// Test-only launchers preserve production environment filtering. Their
	// checksummed manifests exec the unchanged race binaries; no Core/Ready hook.
	for _, hostSide := range []string{"client", "server"} {
		dir := filepath.Join(root, hostSide, "plugins")
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		for id, binary := range binaries {
			actual := filepath.Join(dir, id+".race")
			if err := os.WriteFile(actual, binary, 0700); err != nil {
				t.Fatal(err)
			}
			quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
			script := []byte(fmt.Sprintf("#!/bin/sh\nGORACE=%s exec %s \"$@\"\n", quote("halt_on_error=1 log_path="+filepath.Join(root, "race")), quote(actual)))
			executable := "shiki-plugin-" + id
			if err := os.WriteFile(filepath.Join(dir, executable), script, 0700); err != nil {
				t.Fatal(err)
			}
			digest := sha256.Sum256(script)
			offer, err := sdk.BuiltinProtocol(id, hostSide)
			if err != nil {
				t.Fatal(err)
			}
			manifest, err := json.Marshal(host.Manifest{Name: id, Version: buildinfo.Version, Side: hostSide, Executable: executable, SHA256: hex.EncodeToString(digest[:]), Protocol: offer})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, id+".manifest.json"), manifest, 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Cleanup(func() {
		paths, err := filepath.Glob(filepath.Join(root, "race.*"))
		if err != nil {
			t.Error(err)
		}
		for _, path := range paths {
			data, err := os.ReadFile(path)
			t.Errorf("child race report %s: %v\n%s", filepath.Base(path), err, data[:min(len(data), 64<<10)])
		}
	})
	certPath, keyPath := testCertificate(t, root)
	account, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	dc := config.DefaultDaemon(account.Username, filepath.Join(root, "server", "state"))
	dc.ProjectsDir = filepath.Join(root, "projects")
	dc.PluginsDir = filepath.Join(root, "server", "plugins")
	dc.Tailscale.Enabled = false
	dc.Claude.Executable = "/bin/sh"
	dc.Server.DirectTLS = config.DirectTLS{Enabled: true, Address: address, CertFile: certPath, KeyFile: keyPath}
	h.daemonConfig = dc
	dopts := server.DaemonOptions{Build: buildinfo.Version}
	if side == "server" {
		dopts.PluginCredentialLifetime = credentialLifetime
	}
	h.daemon, err = server.StartDaemon(ctx, dc, dopts)
	if err != nil {
		t.Fatal(err)
	}
	if route != nil {
		address = route(ctx, address, certPath, keyPath)
	}
	var stopAgent context.CancelFunc
	var agentDone chan error
	t.Cleanup(func() {
		if h.front != nil {
			h.front.Close()
		}
		if stopAgent != nil {
			stopAgent()
			if err := <-agentDone; err != nil {
				t.Error("agent shutdown", err)
			}
		}
		if err := h.daemon.Close(); err != nil {
			t.Error("daemon shutdown", err)
		}
		resources.Reaped(t, h.pids...)
		for _, side := range []string{"client", "server"} {
			entries, err := os.ReadDir(h.runDir(side))
			if err != nil || len(entries) != 0 {
				t.Errorf("%s runtime residue: %v %v", side, entries, err)
			}
		}
	})
	token, _, err := h.daemon.Service.Auth.Create(account.Username, []string{"*"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	ac := config.DefaultClient()
	ac.Tailscale.Enabled = false
	ac.Tailscale.StateDir = filepath.Join(root, "client", "node")
	ac.Agent.StateDir = filepath.Join(root, "client", "state")
	ac.Agent.Socket = filepath.Join(root, "agent.sock")
	ac.PluginsDir = filepath.Join(root, "client", "plugins")
	h.agentConfig = ac
	configPath := filepath.Join(root, "client.yaml")
	if err := config.SaveClient(configPath, ac); err != nil {
		t.Fatal(err)
	}
	agentCtx, stop := context.WithCancel(ctx)
	stopAgent = stop
	agentDone = make(chan error, 1)
	aopts := agent.RuntimeOptions{}
	if side == "client" {
		aopts.PluginCredentialLifetime = credentialLifetime
	}
	go func() { agentDone <- agent.RunConfigWithOptions(agentCtx, ac, buildinfo.Version, aopts) }()
	h.await("agent IPC", func(call context.Context) error {
		var err error
		h.front, err = frontend.Open(call, configPath, frontend.Options{Timeout: time.Second})
		return err
	})
	h.local = h.front.Agent()
	h.await("agent resources ready", func(call context.Context) error {
		status, err := h.local.GetStatus(call, &pb.Empty{})
		if err != nil {
			return err
		}
		if status.State != "ready" {
			return fmt.Errorf("agent %s/%s", status.State, status.ReasonCode)
		}
		return nil
	})
	record, err := h.local.UpsertServer(ctx, &apb.UpsertServerRequest{RequestId: h.id(), Config: &apb.ServerConfig{Name: "rotation", DirectTlsAddress: address, TlsServerName: "example.com", TlsCaFile: certPath}})
	if err != nil {
		t.Fatal(err)
	}
	h.serverID = record.Config.Id
	if _, err := h.local.SetServerCredential(ctx, &apb.SetServerCredentialRequest{ServerId: h.serverID, Token: []byte(token), RequestId: h.id()}); err != nil {
		t.Fatal(err)
	}
	h.remote = h.front.Remote(h.serverID)
	return h
}

func testCertificate(t *testing.T, root string) (string, string) {
	t.Helper()
	tls := httptest.NewTLSServer(http.NotFoundHandler())
	certificate := tls.TLS.Certificates[0]
	tls.Close()
	key, err := x509.MarshalPKCS8PrivateKey(certificate.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	certPath, keyPath := filepath.Join(root, "cert.pem"), filepath.Join(root, "key.pem")
	for path, data := range map[string][]byte{certPath: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Certificate[0]}), keyPath: pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key})} {
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return certPath, keyPath
}

func (h *harness) id() string { h.sequence++; return fmt.Sprintf("request-%d", h.sequence) }
func (h *harness) target(side string) *apb.PluginTarget {
	if side == "client" {
		return &apb.PluginTarget{Side: apb.PluginTarget_LOCAL}
	}
	return &apb.PluginTarget{Side: apb.PluginTarget_REMOTE, ServerId: h.serverID}
}
func (h *harness) runDir(side string) string {
	if side == "client" {
		return filepath.Join(h.agentConfig.Agent.StateDir, "plugin-run")
	}
	return filepath.Join(h.daemonConfig.StateDir, "run", "p")
}
func (h *harness) await(label string, check func(context.Context) error) {
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
	h.t.Fatalf("%s: %v (last: %v)", label, ctx.Err(), last)
}
func (h *harness) plugin(side, id string) *pb.PluginStatus {
	h.t.Helper()
	list, err := h.local.ListPlugins(h.ctx, h.target(side))
	if err != nil {
		h.t.Fatal(err)
	}
	for _, status := range list.Plugins {
		if status.PluginId == id {
			return status
		}
	}
	h.t.Fatal("plugin missing", side, id)
	return nil
}
func (h *harness) manage(side, id string, action pb.ManagePluginRequest_Action, values *structpb.Struct) {
	h.t.Helper()
	status := h.plugin(side, id)
	if _, err := h.local.ManagePlugin(h.ctx, &apb.ManagePluginRequest{Target: h.target(side), Request: &pb.ManagePluginRequest{PluginId: id, Action: action, Config: values, ExpectedRevision: status.Revision, RequestId: h.id()}}); err != nil {
		h.t.Fatal("manage", side, id, err)
	}
}
func (h *harness) ready(side string) {
	h.await(side+" workers ready", func(call context.Context) error {
		list, err := h.local.ListPlugins(call, h.target(side))
		if err != nil {
			return err
		}
		if len(list.Plugins) != 2 {
			return errors.New("missing worker")
		}
		for _, status := range list.Plugins {
			if status.State != "ready" || !status.Enabled {
				return fmt.Errorf("%s: %s/%s", status.PluginId, status.State, status.ReasonCode)
			}
		}
		return nil
	})
}
