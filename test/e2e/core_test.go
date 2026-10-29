//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
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
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	host "github.com/DDSRem-Dev/Shikigami/internal/plugin"
	"github.com/DDSRem-Dev/Shikigami/internal/server/tmux"
	"github.com/DDSRem-Dev/Shikigami/pkg/config"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
	"github.com/creack/pty"
	"golang.org/x/sys/unix"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// This suite executes the shipped command entrypoints. It uses an explicitly
// named terminal fixture, never the developer's Claude login or tailnet.
func TestCLIThreeProcessTLSLifecycle(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("daemon must run as an ordinary user; use scripts/test-linux.sh")
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Fatal("end-to-end acceptance requires tmux")
	}
	parent := t.Context()
	stopSignals := func() {}
	if applicationSoakDuration(t) != 0 {
		parent, stopSignals = signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	}
	defer stopSignals()
	ctx, cancel := context.WithTimeout(parent, resourceTestTimeout(t))
	defer cancel()
	directory, err := os.MkdirTemp("/tmp", "shiki-e2e-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	bin := filepath.Join(directory, "bin")
	if err := os.Mkdir(bin, 0700); err != nil {
		t.Fatal(err)
	}
	_, source, _, _ := runtime.Caller(0)
	repository := filepath.Clean(filepath.Join(filepath.Dir(source), "../.."))
	build := exec.CommandContext(ctx, "go", resourceBuildArgs(t, "-buildvcs=false", "-mod=readonly", "-o", bin+string(filepath.Separator), "./cmd/shiki", "./cmd/shiki-agent", "./cmd/shiki-daemon")...)
	build.Dir = repository
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build command entrypoints: %v\n%s", err, output)
	}
	pluginBuild := exec.CommandContext(ctx, "go", resourceBuildArgs(t, "-buildvcs=false", "-mod=readonly", "-o", filepath.Join(bin, "shiki-plugin-sync"), "./plugins/sync")...)
	pluginBuild.Dir = repository
	if output, err := pluginBuild.CombinedOutput(); err != nil {
		t.Fatalf("build plugin: %v\n%s", err, output)
	}
	if mixedSoakClients(t) > 0 {
		portBuild := exec.CommandContext(ctx, "go", resourceBuildArgs(t, "-buildvcs=false", "-mod=readonly", "-o", filepath.Join(bin, "shiki-plugin-port"), "./plugins/port")...)
		portBuild.Dir = repository
		if output, err := portBuild.CombinedOutput(); err != nil {
			t.Fatalf("build mixed workload port plugin: %v\n%s", err, output)
		}
	}
	soakEnvironment := configureSoakRace(t, directory, bin)
	writeSyncManifest(t, bin)
	if mixedSoakClients(t) > 0 {
		writePortManifest(t, bin)
	}
	account, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	environment := []string{"PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH"), "HOME=" + directory, "USER=" + account.Username, "TERM=xterm-256color", "LANG=C.UTF-8"}
	environment = append(environment, soakEnvironment...)
	daemonBinary := filepath.Join(bin, "shiki-daemon")
	cliBinary := filepath.Join(bin, "shiki")
	daemonConfig := filepath.Join(directory, "daemon.yaml")
	clientConfig := filepath.Join(directory, "client.yaml")
	cert, key, roots := certificate(t, directory)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(directory, "claude-test-fixture")
	fixtureSource := "#!/bin/sh\nif [ \"${1:-}\" = --version ]; then printf '0.0.0-shiki-terminal-test-fixture\\n'; exit 0; fi\nprintf '\\033[?1049hE2E_READY_世界\\n'\nwhile IFS= read -r line; do printf 'E2E_REPLY:%s\\n' \"$line\"; done\n"
	if err := os.WriteFile(fixture, []byte(fixtureSource), 0700); err != nil {
		t.Fatal(err)
	}
	run(t, ctx, environment, daemonBinary, "init", "--config", daemonConfig, "--user", account.Username, "--state-dir", filepath.Join(directory, "remote-state"), "--projects-dir", filepath.Join(directory, "projects"), "--tailscale=false", "--direct-tls-address", address, "--tls-cert", cert, "--tls-key", key, "--claude-executable", fixture)
	daemonSettings, err := config.LoadDaemon(daemonConfig)
	if err != nil {
		t.Fatal(err)
	}
	daemonSettings.PluginsDir = bin
	if err := config.SaveDaemon(daemonConfig, daemonSettings); err != nil {
		t.Fatal(err)
	}
	tokenPath := filepath.Join(directory, "token")
	run(t, ctx, environment, daemonBinary, "token", "create", "--config", daemonConfig, "--output", tokenPath)
	tokenBytes, err := os.ReadFile(tokenPath)
	if err != nil {
		t.Fatal(err)
	}
	token := strings.TrimSpace(string(tokenBytes))
	if !strings.HasPrefix(token, "shiki_") {
		t.Fatal("token command did not produce an application token")
	}
	if info, err := os.Stat(tokenPath); err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("token output must be private")
	}
	server := start(t, ctx, environment, filepath.Join(directory, "daemon.log"), daemonBinary, "serve", "--config", daemonConfig)
	endpoint, err := connection.NewTLSClient("https://"+address, connection.TLSOptions{RootCAs: roots, ServerName: "localhost"})
	if err != nil {
		t.Fatal(err)
	}
	defer endpoint.Close()
	credential, err := connection.BearerToken(token)
	if err != nil {
		t.Fatal(err)
	}
	remote := rpc.NewShikiDaemonClient(connection.NewRPCClient(endpoint, credential))
	await(t, ctx, func(probe context.Context) error {
		_, err := remote.NegotiateProtocol(probe, &pb.ProtocolOffer{Major: 1})
		return err
	})
	firstIdentity, err := remote.GetSystemInfo(ctx, &pb.Empty{})
	if err != nil {
		t.Fatal(err)
	}
	client := config.DefaultClient()
	client.Agent.Socket = filepath.Join(directory, "run", "agent.sock")
	client.Agent.StateDir = filepath.Join(directory, "local-state")
	client.Tailscale.Enabled = false
	client.PluginsDir = bin
	client.Tailscale.StateDir = filepath.Join(directory, "local-tsnet")
	if err := config.SaveClient(clientConfig, client); err != nil {
		t.Fatal(err)
	}
	cli := func(args ...string) []byte {
		return run(t, ctx, environment, cliBinary, append([]string{"--config", clientConfig, "--json"}, args...)...)
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 15*time.Second)
		defer stop()
		command := exec.CommandContext(cleanup, cliBinary, "--config", clientConfig, "agent", "stop")
		command.Env = environment
		_ = command.Run()
	}()
	firstAgent := concurrentStart(t, ctx, environment, cliBinary, clientConfig)
	t.Cleanup(func() {
		if t.Failed() {
			contents, _ := os.ReadFile(filepath.Join(client.Agent.StateDir, "agent.log"))
			t.Logf("agent.log:\n%s", contents)
		}
	})
	connected := cli("connect", "test", "--tls-address", address, "--tls-server-name", "localhost", "--tls-ca", cert, "--token-file", tokenPath)
	if bytes.Contains(connected, []byte(token)) {
		t.Fatal("connection response leaked a token")
	}
	projectRoot := filepath.Join(directory, "project-files")
	if err := os.Mkdir(projectRoot, 0700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(projectRoot, "keep.txt")
	if err := os.WriteFile(marker, []byte("preserved project contents"), 0600); err != nil {
		t.Fatal(err)
	}
	project := new(pb.Project)
	decode(t, cli("projects", "create", "application", "--path", projectRoot), project)
	created := new(pb.Session)
	decode(t, cli("sessions", "create", "main", "--project", project.Id), created)
	mux, err := tmux.New("")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if err := mux.Delete(cleanup, created.Id); err != nil {
			t.Error(err)
		}
	}()
	controller, controllerPTY := terminal(t, ctx, environment, cliBinary, "--config", clientConfig, "attach", created.Id)
	defer controllerPTY.Close()
	readTerminal(t, ctx, controllerPTY, "E2E_READY_世界")
	observer, observerPTY := terminal(t, ctx, environment, cliBinary, "--config", clientConfig, "attach", created.Id, "--read-only")
	defer observerPTY.Close()
	readTerminal(t, ctx, observerPTY, "E2E_READY_世界")
	if _, err := observerPTY.Write([]byte{17}); err != nil {
		t.Fatal(err)
	}
	observer.wait(t, ctx, false)
	if _, err := controllerPTY.Write([]byte("from-controller\n")); err != nil {
		t.Fatal(err)
	}
	readTerminal(t, ctx, controllerPTY, "E2E_REPLY:from-controller")
	if _, err := controllerPTY.Write([]byte{17}); err != nil {
		t.Fatal(err)
	}
	controller.wait(t, ctx, false)
	before := new(pb.Session)
	decode(t, cli("sessions", "info", created.Id), before)
	if before.IsConnected || before.ClaudeStatus != "running" || before.ClaudePid == 0 {
		t.Fatalf("detach did not preserve running process: %v", before)
	}
	exerciseTerminalResources(t, ctx, environment, cliBinary, clientConfig, client.Agent.StateDir, server.command.Process.Pid, before, cli)
	// Screen preview crosses the real local whitelist and does not attach or
	// change control/size. Rename changes display metadata only.
	previewClient, err := frontend.Open(ctx, clientConfig, frontend.Options{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	selected, err := previewClient.ResolveServer(ctx, "test")
	if err != nil {
		previewClient.Close()
		t.Fatal(err)
	}
	screen, err := previewClient.Remote(selected.Config.Id).GetSessionScreen(ctx, &pb.GetSessionRequest{Id: created.Id})
	previewClient.Close()
	if err != nil || screen.SessionId != created.Id || !strings.Contains(screen.Text, "E2E_REPLY:from-controller") || screen.CapturedAt == 0 {
		t.Fatalf("current screen proxy failed: %v %v", screen, err)
	}
	renamed := new(pb.Session)
	decode(t, cli("sessions", "edit", created.Id, "--name", "renamed"), renamed)
	decode(t, cli("sessions", "info", created.Id), renamed)
	if renamed.Id != created.Id || renamed.Name != "renamed" || renamed.ClaudePid != before.ClaudePid || renamed.IsConnected {
		t.Fatalf("rename or preview changed live session identity: %v", renamed)
	}
	// Exercise diagnostic commands through the production agent whitelist;
	// permissive command mocks must not hide an illegal remote negotiation call.
	stats := new(pb.StatsSnapshot)
	decode(t, cli("stats", created.Id), stats)
	if stats.Timestamp == 0 || len(stats.Sessions) != 1 || stats.Sessions[0].SessionId != created.Id {
		t.Fatalf("session stats did not cross the agent proxy: %v", stats)
	}
	var diagnosis struct {
		Remote      *pb.SystemInfo    `json:"remote"`
		RemoteError string            `json:"remote_error"`
		Resources   *pb.StatsSnapshot `json:"resources"`
		Errors      map[string]string `json:"errors"`
	}
	if err := json.Unmarshal(cli("doctor"), &diagnosis); err != nil {
		t.Fatal(err)
	}
	if diagnosis.Remote == nil || diagnosis.Remote.DaemonId != firstIdentity.DaemonId || diagnosis.Resources == nil || diagnosis.RemoteError != "" || len(diagnosis.Errors) != 0 {
		t.Fatalf("production diagnostics failed: %+v", diagnosis)
	}
	prefs := new(apb.Preferences)
	decode(t, cli("config", "set", "no_color", "true"), prefs)
	if !prefs.NoColor || prefs.Revision == "" {
		t.Fatal("configuration did not persist through agent")
	}
	cli("agent", "status")
	if err := server.command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	server.wait(t, ctx, true)
	server = start(t, ctx, environment, filepath.Join(directory, "daemon-restarted.log"), daemonBinary, "serve", "--config", daemonConfig)
	await(t, ctx, func(probe context.Context) error {
		_, err := remote.NegotiateProtocol(probe, &pb.ProtocolOffer{Major: 1})
		return err
	})
	secondIdentity, err := remote.GetSystemInfo(ctx, &pb.Empty{})
	if err != nil {
		t.Fatal(err)
	}
	if firstIdentity.DaemonId != secondIdentity.DaemonId {
		t.Fatal("daemon restart replaced persistent identity")
	}
	after := new(pb.Session)
	decode(t, cli("sessions", "info", created.Id), after)
	if after.ClaudePid != before.ClaudePid || after.ClaudeStatus != "running" {
		t.Fatal("daemon crash/restart ended the tmux pane")
	}
	// Kill the shared agent while a real frontend owns a live terminal. The
	// next CLI must recover its stale socket without replacing daemon state.
	attached, attachedPTY := terminal(t, ctx, environment, cliBinary, "--config", clientConfig, "attach", created.Id, "--reconnect=false")
	defer attachedPTY.Close()
	readTerminal(t, ctx, attachedPTY, "E2E_READY_世界")
	pidBytes, err := os.ReadFile(filepath.Join(client.Agent.StateDir, "instance.lock"))
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(pidBytes)))
	if err != nil || pid <= 1 {
		t.Fatal("invalid test agent PID", err)
	}
	if err := syscall.Kill(pid, syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	attached.wait(t, ctx, true)
	await(t, ctx, func(context.Context) error {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return nil
		}
		return errors.New("killed agent has not exited")
	})
	secondAgent := concurrentStart(t, ctx, environment, cliBinary, clientConfig)
	if firstAgent == secondAgent {
		t.Fatal("agent crash recovery reused instance identity")
	}
	decode(t, cli("sessions", "info", created.Id), after)
	if after.ClaudePid != before.ClaudePid {
		t.Fatal("agent crash ended remote pane")
	}
	// A separate local token command must terminate an already authenticated
	// stream through daemon, agent, and raw-mode CLI, without stopping tmux.
	revoked, revokedPTY := terminal(t, ctx, environment, cliBinary, "--config", clientConfig, "attach", created.Id, "--reconnect=false")
	defer revokedPTY.Close()
	readTerminal(t, ctx, revokedPTY, "E2E_READY_世界")
	var tokens []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(run(t, ctx, environment, daemonBinary, "token", "list", "--config", daemonConfig), &tokens); err != nil || len(tokens) != 1 {
		t.Fatal("unexpected token registry", err)
	}
	run(t, ctx, environment, daemonBinary, "token", "revoke", tokens[0].ID, "--config", daemonConfig)
	revoked.wait(t, ctx, true)
	if revoked.err == nil {
		t.Fatal("revoked terminal returned success")
	}
	replacementToken := filepath.Join(directory, "replacement-token")
	run(t, ctx, environment, daemonBinary, "token", "create", "--config", daemonConfig, "--output", replacementToken)
	cli("connect", "test", "--token-file", replacementToken)
	decode(t, cli("sessions", "info", created.Id), after)
	if after.ClaudePid != before.ClaudePid {
		t.Fatal("token revocation ended remote pane")
	}
	exerciseSync(t, ctx, cli, directory, project, clientConfig, created.Id, server.command.Process.Pid)
	stopped := new(pb.Session)
	decode(t, cli("sessions", "stop-claude", created.Id, "--yes"), stopped)
	if stopped.ClaudeStatus != "stopped" {
		t.Fatal("stop-claude did not stop process")
	}
	cli("sessions", "restart-claude", created.Id, "--yes")
	cli("sessions", "delete", created.Id, "--yes")
	contents, err := os.ReadFile(marker)
	if err != nil || string(contents) != "preserved project contents" {
		t.Fatal("session deletion changed project files")
	}
	cli("projects", "delete", project.Id, "--yes")
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("project deletion removed files", err)
	}
	cli("agent", "stop")
	await(t, ctx, func(context.Context) error {
		_, err := os.Lstat(client.Agent.Socket)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return errors.New("agent socket still exists")
	})
	if err := server.command.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	server.wait(t, ctx, false)
}

type process struct {
	command *exec.Cmd
	done    chan struct{}
	err     error
}

func watch(t *testing.T, command *exec.Cmd) *process {
	p := &process{command: command, done: make(chan struct{})}
	go func() { p.err = command.Wait(); close(p.done) }()
	t.Cleanup(func() {
		select {
		case <-p.done:
			return
		default:
		}
		_ = command.Process.Kill()
		<-p.done
	})
	return p
}
func start(t *testing.T, ctx context.Context, environment []string, logPath, binary string, args ...string) *process {
	t.Helper()
	log, err := os.OpenFile(logPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = log.Close() })
	t.Cleanup(func() {
		if t.Failed() {
			contents, _ := os.ReadFile(logPath)
			t.Logf("%s:\n%s", filepath.Base(logPath), contents)
		}
	})
	command := exec.CommandContext(ctx, binary, args...)
	command.Env = environment
	command.Stdout = log
	command.Stderr = log
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	return watch(t, command)
}
func terminal(t *testing.T, ctx context.Context, environment []string, binary string, args ...string) (*process, *os.File) {
	t.Helper()
	command := exec.CommandContext(ctx, binary, args...)
	command.Env = environment
	file, err := pty.StartWithSize(command, &pty.Winsize{Rows: 30, Cols: 100})
	if err != nil {
		t.Fatal(err)
	}
	process := watch(t, command)
	// PTY reads must be pollable so test deadlines and cleanup can interrupt
	// a child that never prints its readiness marker.
	fd, err := unix.FcntlInt(file.Fd(), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := unix.SetNonblock(fd, true); err != nil {
		_ = unix.Close(fd)
		_ = file.Close()
		t.Fatal(err)
	}
	_ = file.Close()
	return process, os.NewFile(uintptr(fd), "e2e-terminal")
}
func (p *process) wait(t *testing.T, ctx context.Context, allowFailure bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	select {
	case <-p.done:
		if !allowFailure && p.err != nil {
			t.Fatalf("%s exited: %v", filepath.Base(p.command.Path), p.err)
		}
	case <-ctx.Done():
		t.Fatalf("waiting for %s: %v", p.command.Path, ctx.Err())
	}
}

func concurrentStart(t *testing.T, ctx context.Context, environment []string, binary, configPath string) string {
	t.Helper()
	type result struct {
		output []byte
		err    error
	}
	results := make(chan result, 4)
	for range 4 {
		go func() {
			command := exec.CommandContext(ctx, binary, "--config", configPath, "--json", "agent", "start")
			command.Env = environment
			output, err := command.CombinedOutput()
			results <- result{output, err}
		}()
	}
	instance := ""
	for range 4 {
		result := <-results
		if result.err != nil {
			t.Errorf("concurrent startup: %v: %s", result.err, result.output)
			continue
		}
		status := new(apb.AgentStatus)
		if err := protojson.Unmarshal(result.output, status); err != nil {
			t.Errorf("startup JSON: %v: %s", err, result.output)
			continue
		}
		if status.State != "ready" || status.InstanceId == "" {
			t.Errorf("agent not ready: %v", status)
		}
		if instance != "" && instance != status.InstanceId {
			t.Error("concurrent CLI started different agents")
		}
		instance = status.InstanceId
	}
	if t.Failed() {
		t.FailNow()
	}
	return instance
}
func run(t *testing.T, ctx context.Context, environment []string, binary string, args ...string) []byte {
	t.Helper()
	command := exec.CommandContext(ctx, binary, args...)
	command.Env = environment
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", filepath.Base(binary), args, err, stderr.String())
	}
	return output
}
func decode(t *testing.T, data []byte, message proto.Message) {
	t.Helper()
	if err := protojson.Unmarshal(data, message); err != nil {
		t.Fatalf("expected protocol JSON: %v (%s)", err, data)
	}
}
func await(t *testing.T, ctx context.Context, probe func(context.Context) error) {
	t.Helper()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	for {
		attempt, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
		err := probe(attempt)
		cancel()
		if err == nil {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-deadline.C:
			t.Fatalf("readiness timeout: %v", err)
		case <-time.After(50 * time.Millisecond):
		}
	}
}
func readTerminal(t *testing.T, ctx context.Context, file *os.File, marker string) {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		var data []byte
		buffer := make([]byte, 4096)
		for {
			n, err := file.Read(buffer)
			data = append(data, buffer[:n]...)
			if bytes.Contains(data, []byte(marker)) {
				done <- nil
				return
			}
			if err != nil {
				done <- fmt.Errorf("waiting for %q: %w, bytes %q", marker, err, data)
				return
			}
			if len(data) > 1<<20 {
				done <- errors.New("terminal output exceeded test bound")
				return
			}
		}
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		_ = file.Close()
		<-done
		t.Fatal(ctx.Err())
	case <-time.After(10 * time.Second):
		_ = file.Close()
		<-done
		t.Fatalf("terminal timed out waiting for %q", marker)
	}
}
func certificate(t *testing.T, directory string) (string, string, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Shiki E2E fixture"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour + applicationSoakDuration(t)), DNSNames: []string{"localhost"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	private, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certPath, keyPath := filepath.Join(directory, "tls-cert.pem"), filepath.Join(directory, "tls-key.pem")
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if err := os.WriteFile(certPath, certPEM, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private}), 0600); err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(certPEM) {
		t.Fatal("test certificate invalid")
	}
	return certPath, keyPath, roots
}

func writeSyncManifest(t *testing.T, bin string) {
	t.Helper()
	binary := "shiki-plugin-sync"
	file, err := os.Open(filepath.Join(bin, binary))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.New()
	_, err = io.Copy(sum, file)
	closeErr := file.Close()
	if err != nil || closeErr != nil {
		t.Fatal(err, closeErr)
	}
	protocol, err := sdk.BuiltinProtocol("sync", "both")
	if err != nil {
		t.Fatal(err)
	}
	manifest := host.Manifest{Name: "sync", Version: "dev", Side: "both", Executable: binary, SHA256: fmt.Sprintf("%x", sum.Sum(nil)), Protocol: protocol}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "sync.manifest.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
}
func exerciseSync(t *testing.T, ctx context.Context, cli func(...string) []byte, directory string, project *pb.Project, clientConfig, sessionID string, daemonPID int) {
	t.Helper()
	cli("plugins", "enable", "sync", "--side", "remote", "--server", "test")
	cli("plugins", "enable", "sync", "--side", "local")
	local := filepath.Join(directory, "local-files")
	if err := os.Mkdir(local, 0700); err != nil {
		t.Fatal(err)
	}
	write := func(root, name, contents string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, name), []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(local, "source.txt", "first local version")
	write(local, ".env", "PRIVATE_TEST_SECRET")
	mappings := new(ppb.MappingList)
	decode(t, cli("sync", "map", project.Id, "--local", local), mappings)
	if len(mappings.Mappings) != 1 {
		t.Fatal("expected one mapping")
	}
	mapping := mappings.Mappings[0]
	cli("sync", "resume", mapping.Id)
	conflicts := new(ppb.ConflictList)
	await(t, ctx, func(context.Context) error {
		decode(t, cli("sync", "conflicts", mapping.Id), conflicts)
		if len(conflicts.Conflicts) == 2 {
			return nil
		}
		return fmt.Errorf("initial plan has %d conflicts", len(conflicts.Conflicts))
	})
	for _, conflict := range conflicts.Conflicts {
		if conflict.Base != nil {
			t.Fatal("initial plan invented a common baseline")
		}
		choice := "keep-remote"
		if conflict.Path == "source.txt" {
			choice = "keep-local"
		}
		cli("sync", "resolve", conflict.Id, "--mapping", mapping.Id, "--generation", conflict.Generation, "--choice", choice, "--yes")
	}
	expect := func(root, name, want string) {
		t.Helper()
		await(t, ctx, func(context.Context) error {
			data, err := os.ReadFile(filepath.Join(root, name))
			if err != nil {
				return err
			}
			if string(data) != want {
				return fmt.Errorf("%s has not reconciled: got %q want %q", name, string(data), want)
			}
			// File replacement precedes the remote RPC ACK and common-baseline
			// commit. Await that commit before simulating the next independent edit.
			status := new(ppb.SyncState)
			decode(t, cli("sync", "status", mapping.Id), status)
			if status.State != "active" || status.PendingFiles != 0 || status.ConflictCount != 0 {
				return fmt.Errorf("sync has not settled: %v", status)
			}
			return nil
		})
	}
	expect(project.Path, "source.txt", "first local version")
	expect(local, "keep.txt", "preserved project contents")
	if _, err := os.Lstat(filepath.Join(project.Path, ".env")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("default ignore copied credentials", err)
	}
	// No CLI remains running while this edit is propagated by the background worker.
	write(local, "source.txt", "background local change")
	expect(project.Path, "source.txt", "background local change")
	write(project.Path, "source.txt", "remote change")
	expect(local, "source.txt", "remote change")
	exerciseApplicationSoak(t, ctx, clientConfig, local, project, mapping, sessionID, daemonPID)
	cli("sync", "pause", mapping.Id)
	write(local, "source.txt", "local conflicting change")
	write(project.Path, "source.txt", "remote conflicting change")
	cli("sync", "resume", mapping.Id)
	await(t, ctx, func(context.Context) error {
		decode(t, cli("sync", "conflicts", mapping.Id), conflicts)
		if len(conflicts.Conflicts) == 1 && conflicts.Conflicts[0].Path == "source.txt" {
			return nil
		}
		return errors.New("waiting for conflicting edits")
	})
	conflict := conflicts.Conflicts[0]
	cli("agent", "stop")
	cli("agent", "start")
	await(t, ctx, func(context.Context) error {
		status := new(pb.PluginStatus)
		decode(t, cli("plugins", "status", "sync", "--side", "local"), status)
		if status.State == "ready" {
			return nil
		}
		return errors.New("waiting for restored sync worker")
	})
	decode(t, cli("sync", "conflicts", mapping.Id), conflicts)
	if len(conflicts.Conflicts) != 1 || conflicts.Conflicts[0].Generation != conflict.Generation {
		t.Fatal("agent restart lost conflict review state")
	}
	cli("sync", "resolve", conflict.Id, "--mapping", mapping.Id, "--generation", conflict.Generation, "--choice", "keep-local", "--yes")
	expect(project.Path, "source.txt", "local conflicting change")
	if err := os.Remove(filepath.Join(local, "source.txt")); err != nil {
		t.Fatal(err)
	}
	await(t, ctx, func(context.Context) error {
		_, err := os.Lstat(filepath.Join(project.Path, "source.txt"))
		if !errors.Is(err, os.ErrNotExist) {
			return errors.New("waiting for conditional deletion")
		}
		// Removal on disk precedes its RPC acknowledgement and the durable
		// common baseline. Pausing in that window correctly retains an unknown
		// operation, which cannot be silently discarded by ordinary unmap.
		status := new(ppb.SyncState)
		decode(t, cli("sync", "status", mapping.Id), status)
		if status.State != "active" || status.PendingFiles != 0 || status.ConflictCount != 0 {
			return fmt.Errorf("conditional deletion has not settled: %v", status)
		}
		return nil
	})
	cli("sync", "pause", mapping.Id)
	cli("sync", "unmap", mapping.Id, "--yes")
	cli("plugins", "disable", "sync", "--side", "local")
	cli("plugins", "disable", "sync", "--side", "remote", "--server", "test")
	if _, err := os.Stat(filepath.Join(local, "keep.txt")); err != nil {
		t.Fatal("unmap changed local files", err)
	}
}
