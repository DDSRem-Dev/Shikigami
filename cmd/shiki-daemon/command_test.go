package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
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
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	"github.com/DDSRem-Dev/Shikigami/internal/node"
	"github.com/DDSRem-Dev/Shikigami/internal/server"
	"github.com/DDSRem-Dev/Shikigami/internal/server/tmux"
	"github.com/DDSRem-Dev/Shikigami/pkg/config"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	arpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent/agentconnect"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
)

// Execute the same Cobra entrypoint in a fresh process. Parent tests are built
// with -race, so lifecycle races in the serving process are checked as well.
func TestDaemonCommandProcess(t *testing.T) {
	encoded := os.Getenv("SHIKI_TEST_DAEMON_ARGS")
	if encoded == "" {
		return
	}
	var args []string
	if err := json.Unmarshal([]byte(encoded), &args); err != nil {
		os.Exit(30)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	command := NewCommand(os.Stdout, os.Stderr)
	command.SetArgs(args)
	if err := command.ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(31)
	}
	os.Exit(0)
}

type commandFixture struct {
	dir, config, address string
	roots                *x509.CertPool
	initArgs             []string
}

func daemonFixture(t *testing.T) commandFixture {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("daemon must run as an ordinary user")
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux unavailable")
	}
	account, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp("/tmp", "shiki-daemon-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "localhost"}, DNSNames: []string{"localhost"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certPath, keyPath := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	if err = os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0600); err != nil {
		t.Fatal(err)
	}
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(certificate)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	claude := filepath.Join(dir, "claude-test")
	if err = os.WriteFile(claude, []byte("#!/bin/sh\nif [ \"$1\" = --version ]; then printf '2.1.3 (Claude Code)\\n'; exit 0; fi\nprintf 'DAEMON_READY\\n'\nwhile IFS= read -r line; do printf 'REPLY:%s\\n' \"$line\"; done\n"), 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "daemon.yaml")
	args := []string{"--config", path, "init", "--user", account.Username, "--state-dir", filepath.Join(dir, "state"), "--projects-dir", filepath.Join(dir, "projects"), "--tailscale=false", "--direct-tls-address", address, "--tls-cert", certPath, "--tls-key", keyPath, "--claude-executable", claude}
	return commandFixture{dir: dir, config: path, address: address, roots: roots, initArgs: args}
}

func runCLI(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out, stderr bytes.Buffer
	cmd := NewCommand(&out, &stderr)
	cmd.SetArgs(args)
	returnOut := func() (string, error) { err := cmd.ExecuteContext(t.Context()); return out.String(), err }
	return returnOut()
}
func runProcessCLI(t *testing.T, args ...string) (string, error) {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(args)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, "-test.run=^TestDaemonCommandProcess$")
	cmd.Env = append(os.Environ(), "SHIKI_TEST_DAEMON_ARGS="+string(encoded))
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err != nil {
		return "", fmt.Errorf("command failed: %w (%s)", err, stderr.String())
	}
	return out.String(), nil
}

func TestInitAndTokenCommandsPreserveIdentityAndPrivateOutputs(t *testing.T) {
	f := daemonFixture(t)
	if _, err := runCLI(t, f.initArgs...); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(f.config)
	if err != nil {
		t.Fatal(err)
	}
	identityPath := filepath.Join(f.dir, "state", "auth", "state.json")
	identity, err := os.ReadFile(identityPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = runCLI(t, f.initArgs...); err != nil {
		t.Fatal("repeat init failed", err)
	}
	after, _ := os.ReadFile(f.config)
	afterIdentity, _ := os.ReadFile(identityPath)
	if !bytes.Equal(before, after) || !bytes.Equal(identity, afterIdentity) {
		t.Fatal("repeat init replaced configuration or signing identity")
	}
	version, _ := runCLI(t, "version")
	alias, _ := runCLI(t, "--version")
	if version != alias {
		t.Fatal("version aliases disagree")
	}
	output := filepath.Join(f.dir, "device.token")
	if _, err = runCLI(t, "--config", f.config, "token", "create", "--output", output, "--expires", "1h"); err != nil {
		t.Fatal(err)
	}
	secret, err := os.ReadFile(output)
	if err != nil || !bytes.HasPrefix(secret, []byte("shiki_")) {
		t.Fatal("token file missing")
	}
	info, _ := os.Stat(output)
	if info.Mode().Perm() != 0600 {
		t.Fatal("token file not private")
	}
	if _, err = runCLI(t, "--config", f.config, "token", "create", "--output", output); err == nil {
		t.Fatal("existing token output overwritten")
	}
	listed, err := runCLI(t, "--config", f.config, "token", "list")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(listed, string(bytes.TrimSpace(secret))) {
		t.Fatal("token list leaked secret")
	}
	var records []struct {
		ID string `json:"id"`
	}
	if err = json.Unmarshal([]byte(listed), &records); err != nil || len(records) != 1 {
		t.Fatal("failed output generated an extra token")
	}
	if _, err = runCLI(t, "--config", f.config, "token", "refresh", records[0].ID, "--expires", "1h"); err != nil {
		t.Fatal(err)
	}
}

func TestAdminNodeControlAvailableBeforeLogin(t *testing.T) {
	f := daemonFixture(t)
	if _, err := runCLI(t, f.initArgs...); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadDaemon(f.config)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Server.DirectTLS = config.DirectTLS{}
	cfg.Tailscale.Enabled = true
	// Deliberately unreachable local HTTPS control plane; no external account
	// or network enrollment is involved in proving admin-first startup.
	cfg.Tailscale.ControlURL = "https://127.0.0.1:1"
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	runtime, err := server.StartDaemon(ctx, cfg, server.DaemonOptions{Build: "test"})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	info := runtime.Info()
	if info.RemoteReady {
		t.Fatal("unauthenticated tsnet node reported remote ready")
	}
	endpoint, err := connection.NewUnixClient(info.AdminSocket, uint32(os.Geteuid()), "shiki-admin")
	if err != nil {
		t.Fatal(err)
	}
	defer endpoint.Close()
	client := arpc.NewNodeControlClient(connection.NewRPCClient(endpoint))
	agreement, err := client.Negotiate(ctx, &pb.ProtocolOffer{Major: 1, Capabilities: []string{"node.login.v1"}})
	if err != nil {
		t.Fatal("admin listener blocked on tsnet login", err)
	}
	query, headers := connect.NewClientContext(ctx)
	headers.RequestHeader().Set(node.NodeInstanceHeader, agreement.InstanceId)
	status, err := client.GetStatus(query, &pb.Empty{})
	if err != nil {
		t.Fatal(err)
	}
	if status.State == "online" || status.LoginUrl != "" {
		t.Fatal("unrequested authentication was started")
	}
	remote := rpc.NewShikiDaemonClient(connection.NewRPCClient(endpoint))
	if _, err = remote.HealthCheck(ctx, &pb.Empty{}); err == nil {
		t.Fatal("admin socket exposed the daemon business service")
	}
}

type safeBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (b *safeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.Write(p)
}
func (b *safeBuffer) String() string { b.mu.Lock(); defer b.mu.Unlock(); return b.buffer.String() }

type daemonProcess struct {
	cmd    *exec.Cmd
	done   chan struct{}
	err    error
	stderr safeBuffer
}

func startProcess(t *testing.T, config string) *daemonProcess {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal([]string{"--config", config, "serve"})
	p := &daemonProcess{done: make(chan struct{})}
	p.cmd = exec.Command(executable, "-test.run=^TestDaemonCommandProcess$")
	p.cmd.Env = append(os.Environ(), "SHIKI_TEST_DAEMON_ARGS="+string(encoded))
	p.cmd.Stderr = &p.stderr
	out, err := p.cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = p.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { p.err = p.cmd.Wait(); close(p.done) }()
	t.Cleanup(func() {
		select {
		case <-p.done:
		default:
			p.cmd.Process.Kill()
			<-p.done
		}
	})
	started := make(chan error, 1)
	go func() {
		var event struct {
			Event string `json:"event"`
		}
		err := json.NewDecoder(out).Decode(&event)
		if err == nil && event.Event != "daemon_started" {
			err = errors.New("unexpected startup event")
		}
		started <- err
		_, _ = io.Copy(io.Discard, out)
	}()
	select {
	case err := <-started:
		if err != nil {
			t.Fatalf("daemon startup: %v (%s)", err, p.stderr.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("daemon startup timeout", p.stderr.String())
	}
	return p
}
func (p *daemonProcess) stop(t *testing.T, signal os.Signal) {
	t.Helper()
	if err := p.cmd.Process.Signal(signal); err != nil {
		t.Fatal(err)
	}
	select {
	case <-p.done:
		if signal == syscall.SIGTERM && p.err != nil {
			t.Fatalf("graceful daemon exit: %v (%s)", p.err, p.stderr.String())
		}
	case <-time.After(12 * time.Second):
		t.Fatal("daemon did not exit", p.stderr.String())
	}
}

func remoteClient(t *testing.T, f commandFixture, token string) rpc.ShikiDaemonClient {
	t.Helper()
	endpoint, err := connection.NewTLSClient("https://"+f.address, connection.TLSOptions{RootCAs: f.roots, ServerName: "localhost"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(endpoint.Close)
	credential, err := connection.BearerToken(token)
	if err != nil {
		t.Fatal(err)
	}
	return rpc.NewShikiDaemonClient(connection.NewRPCClient(endpoint, credential))
}

func TestDaemonProcessTLSRevocationCrashAndRestartPreserveTmux(t *testing.T) {
	f := daemonFixture(t)
	if _, err := runProcessCLI(t, f.initArgs...); err != nil {
		t.Fatal(err)
	}
	output, err := runProcessCLI(t, "--config", f.config, "token", "create", "--expires", "1h")
	if err != nil {
		t.Fatal(err)
	}
	token := strings.TrimSpace(output)
	first := startProcess(t, f.config)
	client := remoteClient(t, f, token)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	if _, err = client.NegotiateProtocol(ctx, &pb.ProtocolOffer{Major: 1, Capabilities: []string{"terminal.v1"}}); err != nil {
		t.Fatal(err)
	}
	if _, err = runProcessCLI(t, "--config", f.config, "serve"); err == nil {
		t.Fatal("duplicate daemon acquired the same state directory")
	}
	version, err := client.GetClaudeVersion(ctx, &pb.Empty{})
	if err != nil || version.Version != "2.1.3" {
		t.Fatalf("real version query: %+v %v", version, err)
	}
	project, err := client.CreateProject(ctx, &pb.CreateProjectRequest{Name: "process-test", Path: filepath.Join(f.dir, "projects", "app"), RequestId: "project"})
	if err != nil {
		t.Fatal(err)
	}
	session, err := client.CreateSession(ctx, &pb.CreateSessionRequest{ProjectId: project.Id, Name: "main", RequestId: "session"})
	if err != nil {
		t.Fatal(err)
	}
	mux, err := tmux.New("")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := mux.Delete(ctx, session.Id); err != nil {
			t.Error(err)
		}
	})
	before, err := mux.Inspect(ctx, session.Id)
	if err != nil {
		t.Fatal(err)
	}
	stream, err := client.TerminalStream(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if err = stream.Send(&pb.TerminalInput{Payload: &pb.TerminalInput_Open{Open: &pb.TerminalOpen{SessionId: session.Id, Size: &pb.ResizeEvent{Rows: 24, Cols: 80}}}}); err != nil {
		t.Fatal(err)
	}
	for {
		out, err := stream.Receive()
		if err != nil {
			t.Fatal(err)
		}
		if out.GetEvent().GetType() == pb.SessionEvent_READY {
			break
		}
	}
	listed, err := runProcessCLI(t, "--config", f.config, "token", "list")
	if err != nil {
		t.Fatal(err)
	}
	var records []struct {
		ID string `json:"id"`
	}
	if err = json.Unmarshal([]byte(listed), &records); err != nil || len(records) != 1 {
		t.Fatal("token registry mismatch")
	}
	if _, err = runProcessCLI(t, "--config", f.config, "token", "revoke", records[0].ID); err != nil {
		t.Fatal(err)
	}
	for {
		_, err = stream.Receive()
		if err != nil {
			break
		}
	}
	if errors.Is(err, io.EOF) {
		t.Fatal("revoked stream ended as successful EOF")
	}
	if _, err = client.HealthCheck(ctx, &pb.Empty{}); connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Fatalf("revoked token admitted: %v", err)
	}
	output, err = runProcessCLI(t, "--config", f.config, "token", "create", "--expires", "1h")
	if err != nil {
		t.Fatal(err)
	}
	secondToken := strings.TrimSpace(output)
	first.stop(t, syscall.SIGKILL)
	after, err := mux.Inspect(ctx, session.Id)
	if err != nil || after.PID != before.PID || after.Dead {
		t.Fatal("daemon crash killed or restarted pane", err)
	}
	second := startProcess(t, f.config)
	afterRestart := remoteClient(t, f, secondToken)
	if _, err = afterRestart.GetSession(ctx, &pb.GetSessionRequest{Id: session.Id}); err != nil {
		t.Fatal("persisted token/session did not survive restart", err)
	}
	second.stop(t, syscall.SIGTERM)
	after, err = mux.Inspect(ctx, session.Id)
	if err != nil || after.PID != before.PID || after.Dead {
		t.Fatal("graceful shutdown killed pane", err)
	}
}
