//go:build e2e

package e2e

import (
	"bytes"
	"connectrpc.com/connect/v2"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	"github.com/DDSRem-Dev/Shikigami/internal/install"
	"github.com/DDSRem-Dev/Shikigami/pkg/config"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	arpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent/agentconnect"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
)

// Exercise the real short-lived helper and daemon without registering a system
// service. The explicit TLS-only fixture needs no external node/account login.
func TestInstallerLocalReadinessAndExplicitTokenDelivery(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("run installer E2E as an ordinary user")
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Fatal("tmux is required", err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	root, err := os.MkdirTemp("/tmp", "shiki-bootstrap-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	prefix := filepath.Join(root, "install")
	bin := filepath.Join(prefix, "current")
	if err = os.MkdirAll(bin, 0700); err != nil {
		t.Fatal(err)
	}
	_, source, _, _ := runtime.Caller(0)
	repository := filepath.Clean(filepath.Join(filepath.Dir(source), "../.."))
	build := exec.CommandContext(ctx, "go", "build", "-race", "-buildvcs=false", "-mod=readonly", "-o", bin+string(filepath.Separator), "./cmd/shiki-install", "./cmd/shiki-daemon")
	build.Dir = repository
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	build = exec.CommandContext(ctx, "go", "build", "-race", "-buildvcs=false", "-mod=readonly", "-o", filepath.Join(bin, "shiki-plugin-sync"), "./plugins/sync")
	build.Dir = repository
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build plugin: %v\n%s", err, out)
	}
	writeSyncManifest(t, bin)
	account, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	environment := []string{"HOME=" + account.HomeDir, "USER=" + account.Username, "PATH=" + os.Getenv("PATH")}
	mode := filepath.Join(root, "login-state")
	if err = os.WriteFile(mode, []byte("true"), 0600); err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(root, "claude-fixture")
	script := "#!/bin/sh\nif [ \"$1\" = --version ]; then printf '2.1.310 (Claude Code)\\n'; exit 0; fi\n[ \"$1 $2 $3\" = 'auth status --json' ] || exit 2\nif [ \"$(cat '" + mode + "')\" = true ]; then printf '{\"loggedIn\":true,\"account\":\"private-test-account\"}'; else printf '{\"loggedIn\":false}'; exit 1; fi\n"
	if err = os.WriteFile(fixture, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	cert, key, roots := certificate(t, root)
	reservation, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := reservation.Addr().String()
	reservation.Close()
	configuration := filepath.Join(root, "daemon.yaml")
	stateDir := filepath.Join(root, "state")
	daemonBinary := filepath.Join(bin, "shiki-daemon")
	run(t, ctx, environment, daemonBinary, "init", "--config", configuration, "--user", account.Username, "--state-dir", stateDir, "--projects-dir", filepath.Join(root, "projects"), "--tailscale=false", "--direct-tls-address", address, "--tls-cert", cert, "--tls-key", key, "--claude-executable", fixture)
	cfg, err := config.LoadDaemon(configuration)
	if err != nil {
		t.Fatal(err)
	}
	cfg.PluginsDir = bin
	if err = config.SaveDaemon(configuration, cfg); err != nil {
		t.Fatal(err)
	}
	daemon := start(t, ctx, environment, filepath.Join(root, "daemon.log"), daemonBinary, "serve", "--config", configuration)
	plan := install.Plan{Role: "daemon", User: account.Username, Home: account.HomeDir, Path: os.Getenv("PATH"), Config: configuration, StateDir: stateDir, TLSAddress: address}
	tokenPath := filepath.Join(root, "device.token")
	native := &install.Native{Prefix: prefix, ExpectedVersion: "dev", IssueToken: true, TokenOutput: tokenPath}
	state, err := native.Probe(ctx, plan)
	if err != nil || state != "local_application_ready" || !native.TokenIssued {
		t.Fatal("initial local bootstrap", state, err)
	}
	if native.Report.Node.State != "offline" || native.Report.Node.ReasonCode != "NODE_DISABLED" {
		t.Fatal("TLS-only node must retain the documented offline state", native.Report.Node)
	}
	credential, err := os.ReadFile(tokenPath)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(credential)
	if info, err := os.Stat(tokenPath); err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("Token destination is not private", err)
	}
	endpoint, err := connection.NewTLSClient("https://"+address, connection.TLSOptions{RootCAs: roots, ServerName: "localhost"})
	if err != nil {
		t.Fatal(err)
	}
	defer endpoint.Close()
	bearer, err := connection.BearerToken(strings.TrimSpace(string(credential)))
	if err != nil {
		t.Fatal(err)
	}
	remote := rpc.NewShikiDaemonClient(connection.NewRPCClient(endpoint, bearer))
	if _, err := arpc.NewDaemonReadinessClient(connection.NewRPCClient(endpoint, bearer)).Negotiate(ctx, &pb.ProtocolOffer{Major: 1}); connect.CodeOf(err) != connect.CodeUnimplemented {
		t.Fatal("local readiness registered remotely", err)
	}
	admin, err := connection.NewUnixClient(filepath.Join(stateDir, "run", "admin.sock"), uint32(os.Getuid()), "installer-boundary-test")
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	if _, err := rpc.NewShikiDaemonClient(connection.NewRPCClient(admin)).GetSystemInfo(ctx, &pb.Empty{}); connect.CodeOf(err) != connect.CodeUnimplemented {
		t.Fatal("business service registered on admin socket", err)
	}
	agreement, err := remote.NegotiateProtocol(ctx, &pb.ProtocolOffer{Major: 1})
	if err != nil || agreement.DaemonId != native.Report.Readiness.System.DaemonId {
		t.Fatal("issued Token does not authenticate the probed daemon", agreement, err)
	}
	countTokens := func() int {
		var tokens []json.RawMessage
		if err := json.Unmarshal(run(t, ctx, environment, daemonBinary, "token", "list", "--config", configuration), &tokens); err != nil {
			t.Fatal(err)
		}
		return len(tokens)
	}
	if countTokens() != 1 {
		t.Fatal("initial issuance count")
	}
	plain := &install.Native{Prefix: prefix, ExpectedVersion: "dev"}
	if state, err := plain.Probe(ctx, plan); err != nil || state != "local_application_ready" {
		t.Fatal(state, err)
	}
	if plain.TokenIssued || countTokens() != 1 {
		t.Fatal("plain probe implicitly issued a Token")
	}
	var tty bytes.Buffer
	interactive := &install.Native{Prefix: prefix, ExpectedVersion: "dev", IssueToken: true, TokenTTY: &tty}
	if _, err = interactive.Probe(ctx, plan); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(tty.String(), "shiki_") || countTokens() != 2 {
		t.Fatal("explicit controlling-output issuance failed")
	}
	result := &install.Result{State: "local_application_ready"}
	interactive.Describe(result)
	encoded, _ := json.Marshal(result)
	if bytes.Contains(encoded, bytes.TrimSpace(tty.Bytes())) || bytes.Contains(encoded, []byte("private-test-account")) || result.ExternalPeerAcceptance != "pending" || result.ClaudeLoginState != "reported_logged_in" {
		t.Fatal("public bootstrap output leaked or overclaimed", string(encoded))
	}
	clear(tty.Bytes())
	exists := &install.Native{Prefix: prefix, ExpectedVersion: "dev", IssueToken: true, TokenOutput: tokenPath}
	if _, err = exists.Probe(ctx, plan); err == nil || countTokens() != 2 {
		t.Fatal("existing Token file caused hidden additional issuance", err)
	}
	if err = os.WriteFile(mode, []byte("false"), 0600); err != nil {
		t.Fatal(err)
	}
	if state, err := plain.Probe(ctx, plan); err != nil || state != "application_pending" || plain.Report.Reason != "CLAUDE_LOGIN_REQUIRED" {
		t.Fatal("logged-out dependency claimed ready", state, err, plain.Report)
	}
	if err = os.WriteFile(mode, []byte("true"), 0600); err != nil {
		t.Fatal(err)
	}
	plugins, err := remote.ListPlugins(ctx, &pb.ListPluginsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	revision := ""
	for _, plugin := range plugins.Plugins {
		if plugin.PluginId == "sync" {
			revision = plugin.Revision
		}
	}
	if _, err = remote.ManagePlugin(ctx, &pb.ManagePluginRequest{PluginId: "sync", Action: pb.ManagePluginRequest_ENABLE, ExpectedRevision: revision, RequestId: "readiness-enable-sync"}); err != nil {
		t.Fatal(err)
	}
	await(t, ctx, func(call context.Context) error {
		list, err := remote.ListPlugins(call, &pb.ListPluginsRequest{})
		if err != nil {
			return err
		}
		for _, p := range list.Plugins {
			if p.PluginId == "sync" && p.State == "ready" {
				revision = p.Revision
				return nil
			}
		}
		return errors.New("plugin not ready")
	})
	if state, err := plain.Probe(ctx, plan); err != nil || state != "local_application_ready" || !slices.Contains(plain.Report.Readiness.System.Capabilities, sdk.FileCapability) {
		t.Fatal("running plugin was not verified", state, err)
	}
	if err = os.WriteFile(filepath.Join(bin, "sync.manifest.json"), []byte("corrupted test manifest"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = remote.ManagePlugin(ctx, &pb.ManagePluginRequest{PluginId: "sync", Action: pb.ManagePluginRequest_RESTART, ExpectedRevision: revision, RequestId: "readiness-restart-sync"}); err != nil {
		t.Fatal(err)
	}
	await(t, ctx, func(call context.Context) error {
		list, err := remote.ListPlugins(call, &pb.ListPluginsRequest{})
		if err != nil {
			return err
		}
		for _, p := range list.Plugins {
			if p.PluginId == "sync" && p.State == "failed" {
				return nil
			}
		}
		return errors.New("plugin has not reported failure")
	})
	if state, err := plain.Probe(ctx, plan); err != nil || state != "application_pending" || plain.Report.Readiness.State != "degraded" {
		t.Fatal("failed enabled plugin claimed ready", state, err)
	}
	if err = daemon.command.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	daemon.wait(t, ctx, false)
}
