//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	host "github.com/DDSRem-Dev/Shikigami/internal/plugin"
	"github.com/DDSRem-Dev/Shikigami/pkg/config"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
	"google.golang.org/protobuf/proto"
)

func TestCLIPortForwardTLSLifecycleAndRecovery(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("ordinary user daemon acceptance requires scripts/test-linux.sh")
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Fatal("tmux required for daemon fixture")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 4*time.Minute)
	defer cancel()
	dir, err := os.MkdirTemp("/tmp", "shiki-port-cli-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	bin := filepath.Join(dir, "bin")
	if err = os.Mkdir(bin, 0700); err != nil {
		t.Fatal(err)
	}
	_, source, _, _ := runtime.Caller(0)
	repository := filepath.Clean(filepath.Join(filepath.Dir(source), "../.."))
	build := exec.CommandContext(ctx, "go", "build", "-race", "-buildvcs=false", "-mod=readonly", "-o", bin+string(filepath.Separator), "./cmd/shiki", "./cmd/shiki-agent", "./cmd/shiki-daemon")
	build.Dir = repository
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	build = exec.CommandContext(ctx, "go", "build", "-race", "-buildvcs=false", "-mod=readonly", "-o", filepath.Join(bin, "shiki-plugin-port"), "./plugins/port")
	build.Dir = repository
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build plugin: %v\n%s", err, out)
	}
	writePortManifest(t, bin)
	ownedExecutable, ownedPort, scannerFailure := prepareOwnedPortFixture(t, ctx, dir, bin)
	account, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	environment := []string{"PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH"), "HOME=" + dir, "USER=" + account.Username, "LANG=C.UTF-8"}
	daemonBinary, cliBinary := filepath.Join(bin, "shiki-daemon"), filepath.Join(bin, "shiki")
	daemonConfig, clientConfig := filepath.Join(dir, "daemon.yaml"), filepath.Join(dir, "client.yaml")
	cert, key, roots := certificate(t, dir)
	reservation, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := reservation.Addr().String()
	reservation.Close()
	run(t, ctx, environment, daemonBinary, "init", "--config", daemonConfig, "--user", account.Username, "--state-dir", filepath.Join(dir, "remote-state"), "--projects-dir", filepath.Join(dir, "projects"), "--tailscale=false", "--direct-tls-address", address, "--tls-cert", cert, "--tls-key", key, "--claude-executable", ownedExecutable)
	daemonSettings, err := config.LoadDaemon(daemonConfig)
	if err != nil {
		t.Fatal(err)
	}
	daemonSettings.PluginsDir = bin
	if err = config.SaveDaemon(daemonConfig, daemonSettings); err != nil {
		t.Fatal(err)
	}
	tokenPath := filepath.Join(dir, "token")
	run(t, ctx, environment, daemonBinary, "token", "create", "--config", daemonConfig, "--output", tokenPath)
	token, _ := os.ReadFile(tokenPath)
	daemon := start(t, ctx, environment, filepath.Join(dir, "daemon.log"), daemonBinary, "serve", "--config", daemonConfig)
	endpoint, err := connection.NewTLSClient("https://"+address, connection.TLSOptions{RootCAs: roots, ServerName: "localhost"})
	if err != nil {
		t.Fatal(err)
	}
	defer endpoint.Close()
	credential, err := connection.BearerToken(strings.TrimSpace(string(token)))
	if err != nil {
		t.Fatal(err)
	}
	remote := rpc.NewShikiDaemonClient(connection.NewRPCClient(endpoint, credential))
	await(t, ctx, func(ctx context.Context) error {
		_, e := remote.NegotiateProtocol(ctx, &pb.ProtocolOffer{Major: 1})
		return e
	})
	clientConfigData := config.DefaultClient()
	clientConfigData.Agent.StateDir = filepath.Join(dir, "agent")
	clientConfigData.Agent.Socket = filepath.Join(dir, "run", "agent.sock")
	clientConfigData.Tailscale.Enabled = false
	clientConfigData.Tailscale.StateDir = filepath.Join(dir, "tailscale")
	clientConfigData.PluginsDir = bin
	if err = config.SaveClient(clientConfig, clientConfigData); err != nil {
		t.Fatal(err)
	}
	cli := func(args ...string) []byte {
		return run(t, ctx, environment, cliBinary, append([]string{"--config", clientConfig, "--json"}, args...)...)
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		command := exec.CommandContext(cleanup, cliBinary, "--config", clientConfig, "agent", "stop")
		command.Env = environment
		_ = command.Run()
	}()
	t.Cleanup(func() {
		if t.Failed() {
			data, _ := os.ReadFile(filepath.Join(clientConfigData.Agent.StateDir, "agent.log"))
			t.Logf("agent.log:\n%s", data)
		}
	})
	cli("connect", "port-test", "--tls-address", address, "--tls-server-name", "localhost", "--tls-ca", cert, "--token-file", tokenPath)
	target, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	targetPort := target.Addr().(*net.TCPAddr).Port
	var targetWG sync.WaitGroup
	var bytesReceived atomic.Int64
	inputObserved := make(chan struct{}, 1)
	acceptDone := make(chan struct{})
	go func() {
		defer close(acceptDone)
		for {
			conn, err := target.Accept()
			if err != nil {
				return
			}
			targetWG.Add(1)
			go func() {
				defer targetWG.Done()
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(30 * time.Second))
				var data []byte
				buffer := make([]byte, 32768)
				for {
					n, readErr := conn.Read(buffer)
					bytesReceived.Add(int64(n))
					data = append(data, buffer[:n]...)
					if bytes.Equal(data, []byte("never-replay")) {
						select {
						case inputObserved <- struct{}{}:
						default:
						}
					}
					if readErr != nil {
						err = readErr
						break
					}
				}
				if errors.Is(err, io.EOF) {
					time.Sleep(20 * time.Millisecond)
					_, _ = conn.Write(append([]byte("echo:"), data...))
				}
			}()
		}
	}()
	defer func() { target.Close(); <-acceptDone; targetWG.Wait() }()
	settings := filepath.Join(dir, "port.json")
	if err = os.WriteFile(settings, []byte(fmt.Sprintf(`{"allow_ports":[%d],"idle_timeout_seconds":30}`, targetPort)), 0600); err != nil {
		t.Fatal(err)
	}
	cli("plugins", "config", "port", "--side", "remote", "--server", "port-test", "--file", settings)
	cli("plugins", "enable", "port", "--side", "remote", "--server", "port-test")
	cli("plugins", "enable", "port", "--side", "local")
	first := new(ppb.ForwardState)
	args := []string{"--server", "port-test", "--request-id", "create-manual-forward", "ports", "forward", strconv.Itoa(targetPort), "--local-port", "0"}
	decode(t, cli(args...), first)
	if first.State != "listening" || first.BoundPort == 0 {
		t.Fatal(first)
	}
	replay := new(ppb.ForwardState)
	decode(t, cli(args...), replay)
	if !proto.Equal(first, replay) {
		t.Fatal("duplicate CLI start changed resource/result")
	}
	// A bind conflict must retain a stable desired ID and report the real failure.
	conflictArgs := []string{"--config", clientConfig, "--json", "--server", "port-test", "--request-id", "bind-conflict", "ports", "forward", strconv.Itoa(targetPort), "--local-port", strconv.Itoa(int(first.BoundPort))}
	conflictCommand := exec.CommandContext(ctx, cliBinary, conflictArgs...)
	conflictCommand.Env = environment
	conflictOutput, conflictErr := conflictCommand.Output()
	if conflictErr == nil {
		t.Fatal("conflicting bind reported success")
	}
	conflict := new(ppb.ForwardState)
	decode(t, conflictOutput, conflict)
	if conflict.Spec == nil || conflict.Spec.Id == "" || conflict.State != "error" || conflict.BoundPort != 0 || conflict.ReasonCode != "FORWARD_BIND_FAILED" {
		t.Fatal("bind failure lost desired identity or fabricated listener", conflict)
	}
	cli("ports", "stop", conflict.Spec.Id)
	state := first
	list := func() *ppb.ForwardList { v := new(ppb.ForwardList); decode(t, cli("ports", "list"), v); return v }
	current := func() *ppb.ForwardState {
		for _, v := range list().Forwards {
			if v.Spec.Id == first.Spec.Id {
				return v
			}
		}
		return nil
	}
	exchange := func(body []byte) {
		t.Helper()
		conn, err := net.DialTCP("tcp4", nil, &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: int(state.BoundPort)})
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(10 * time.Second))
		if _, err = conn.Write(body); err != nil {
			t.Fatal(err)
		}
		if err = conn.CloseWrite(); err != nil {
			t.Fatal(err)
		}
		got, err := io.ReadAll(conn)
		if err != nil || !bytes.Equal(got, append([]byte("echo:"), body...)) {
			t.Fatalf("half-close response %d bytes err=%v", len(got), err)
		}
	}
	exchange(bytes.Repeat([]byte("cli-ended"), 50000))
	status := new(apb.AgentStatus)
	decode(t, cli("agent", "status"), status)
	if status.ActiveForwards != 1 {
		t.Fatal("status did not report actual listener", status)
	}
	// Normal agent restart restores the same desired ID/revision with a fresh TCP listener.
	cli("agent", "stop")
	cli("agent", "start")
	await(t, ctx, func(context.Context) error {
		state = current()
		if state != nil && state.State == "listening" && state.BoundPort != 0 {
			return nil
		}
		return errors.New("forward not restored")
	})
	if state.Spec.Id != first.Spec.Id || state.Spec.Revision != first.Spec.Revision {
		t.Fatal("restart changed desired identity")
	}
	exchange([]byte("after-agent-restart"))
	idle := new(ppb.ForwardState)
	decode(t, cli("--server", "port-test", "ports", "forward", strconv.Itoa(targetPort), "--local-port", "0"), idle)
	if idle.BoundPort == 0 {
		t.Fatal("idle listener did not start", idle)
	}
	// Revocation must cancel an existing TCP session and close the local listener.
	pending, err := net.Dial("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(int(state.BoundPort))))
	if err != nil {
		t.Fatal(err)
	}
	defer pending.Close()
	if _, err = pending.Write([]byte("never-replay")); err != nil {
		t.Fatal(err)
	}
	select {
	case <-inputObserved:
	case <-ctx.Done():
		t.Fatal("target did not receive pending input", ctx.Err())
	}
	var tokens []struct {
		ID string `json:"id"`
	}
	if err = json.Unmarshal(run(t, ctx, environment, daemonBinary, "token", "list", "--config", daemonConfig), &tokens); err != nil || len(tokens) != 1 {
		t.Fatal(tokens, err)
	}
	run(t, ctx, environment, daemonBinary, "token", "revoke", tokens[0].ID, "--config", daemonConfig)
	pending.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, err = pending.Read(make([]byte, 1))
	if err == nil {
		t.Fatal("revoked connection remained open")
	}
	if e, ok := err.(net.Error); ok && e.Timeout() {
		t.Fatal("revocation failed to cancel TCP")
	}
	await(t, ctx, func(context.Context) error {
		states := list().Forwards
		if len(states) != 2 {
			return errors.New("revocation lost desired intent")
		}
		for _, candidate := range states {
			if candidate.BoundPort != 0 || candidate.Connections != 0 {
				return errors.New("revoked active or idle listener remained open")
			}
		}
		return nil
	})
	receivedBefore := bytesReceived.Load()
	replacement := filepath.Join(dir, "replacement-token")
	run(t, ctx, environment, daemonBinary, "token", "create", "--config", daemonConfig, "--output", replacement)
	cli("connect", "port-test", "--token-file", replacement)
	await(t, ctx, func(context.Context) error {
		states := list().Forwards
		if len(states) != 2 {
			return errors.New("recovery lost desired intent")
		}
		for _, candidate := range states {
			if candidate.State != "listening" || candidate.BoundPort == 0 {
				return errors.New("authenticated route did not restore active and idle forwards")
			}
			if candidate.Spec.Id == first.Spec.Id {
				state = candidate
			}
		}
		return nil
	})
	if bytesReceived.Load() != receivedBefore {
		t.Fatal("recovery replayed old TCP input")
	}
	exchange([]byte("fresh-connection-after-auth"))
	cli("ports", "stop", idle.Spec.Id)
	stopArgs := []string{"--request-id", "stop-manual-forward", "ports", "stop", first.Spec.Id, "--revision", first.Spec.Revision}
	cli(stopArgs...)
	cli(stopArgs...)
	if len(list().Forwards) != 0 {
		t.Fatal("stop retained desired forward")
	}
	cli("agent", "stop")
	cli("agent", "start")
	if len(list().Forwards) != 0 {
		t.Fatal("restart restored stopped forward")
	}
	exerciseAutomaticPorts(t, ctx, dir, ownedPort, targetPort, scannerFailure, cli)
	cli("agent", "stop")
	if err = daemon.command.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	daemon.wait(t, ctx, false)
}
func writePortManifest(t *testing.T, dir string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "shiki-plugin-port"))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	offer, err := sdk.BuiltinProtocol("port", "both")
	if err != nil {
		t.Fatal(err)
	}
	manifest := host.Manifest{Name: "port", Version: "dev", Side: "both", Executable: "shiki-plugin-port", SHA256: hex.EncodeToString(sum[:]), Protocol: offer}
	data, err = json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "port.manifest.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
}
