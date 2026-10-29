//go:build e2e

package e2e

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	frontend "github.com/DDSRem-Dev/Shikigami/internal/client"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	"github.com/DDSRem-Dev/Shikigami/internal/server/tmux"
	"github.com/DDSRem-Dev/Shikigami/pkg/config"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
)

// TestTwoIndependentAgentsSharedTLSProject is a same-host two-agent simulation,
// not physical multi-device or external-network acceptance. Each agent owns its
// own configuration, credential, socket, state, home, mapping and sync subprocess.
func TestTwoIndependentAgentsSharedTLSProject(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("daemon requires an ordinary user; use scripts/test-linux.sh")
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Fatal("end-to-end acceptance requires tmux")
	}
	ctx, cancel := context.WithTimeout(t.Context(), resourceTestTimeout(t))
	defer cancel()
	dir, err := os.MkdirTemp("/tmp", "shiki-two-agents-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	dir, err = filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "bin")
	if err := os.Mkdir(bin, 0700); err != nil {
		t.Fatal(err)
	}
	_, source, _, _ := runtime.Caller(0)
	repository := filepath.Clean(filepath.Join(filepath.Dir(source), "../.."))
	for _, packages := range [][]string{{"./cmd/shiki", "./cmd/shiki-agent", "./cmd/shiki-daemon"}, {"./plugins/sync"}} {
		output := bin + string(filepath.Separator)
		if len(packages) == 1 {
			output = filepath.Join(bin, "shiki-plugin-sync")
		}
		args := append([]string{"-buildvcs=false", "-mod=readonly", "-o", output}, packages...)
		command := exec.CommandContext(ctx, "go", resourceBuildArgs(t, args...)...)
		command.Dir = repository
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("build shipped processes: %v\n%s", err, output)
		}
	}
	writeSyncManifest(t, bin)
	account, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	environment := func(home string) []string {
		return []string{"PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH"), "HOME=" + home, "USER=" + account.Username, "TERM=xterm-256color", "LANG=C.UTF-8"}
	}
	cliBinary, daemonBinary := filepath.Join(bin, "shiki"), filepath.Join(bin, "shiki-daemon")
	daemonConfig := filepath.Join(dir, "daemon.yaml")
	cert, key, roots := certificate(t, dir)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(dir, "claude-two-agent-fixture")
	if err := os.WriteFile(fixture, []byte("#!/bin/sh\nif [ \"${1:-}\" = --version ]; then printf '0.0.0-two-agent-fixture\\n'; exit 0; fi\nprintf '\\033[?1049hTWO_AGENT_READY\\n'\nwhile IFS= read -r line; do printf 'TWO_AGENT_REPLY:%s\\n' \"$line\"; done\n"), 0700); err != nil {
		t.Fatal(err)
	}
	run(t, ctx, environment(dir), daemonBinary, "init", "--config", daemonConfig, "--user", account.Username, "--state-dir", filepath.Join(dir, "remote-state"), "--projects-dir", filepath.Join(dir, "projects"), "--tailscale=false", "--direct-tls-address", address, "--tls-cert", cert, "--tls-key", key, "--claude-executable", fixture)
	daemonSettings, err := config.LoadDaemon(daemonConfig)
	if err != nil {
		t.Fatal(err)
	}
	daemonSettings.PluginsDir = bin
	if err := config.SaveDaemon(daemonConfig, daemonSettings); err != nil {
		t.Fatal(err)
	}
	type device struct {
		config, root, token string
		cli                 func(...string) []byte
		client              *frontend.Client
		mapping             *ppb.Mapping
	}
	devices := make([]*device, 2)
	for i := range devices {
		home := filepath.Join(dir, fmt.Sprintf("device-%d", i))
		if err := os.Mkdir(home, 0700); err != nil {
			t.Fatal(err)
		}
		d := &device{config: filepath.Join(home, "client.yaml"), root: filepath.Join(home, "files"), token: filepath.Join(home, "token")}
		settings := config.DefaultClient()
		settings.Agent.Socket = filepath.Join(home, "run", "agent.sock")
		settings.Agent.StateDir = filepath.Join(home, "state")
		settings.Tailscale.Enabled = false
		settings.Tailscale.StateDir = filepath.Join(home, "tsnet")
		settings.PluginsDir = bin
		if err := config.SaveClient(d.config, settings); err != nil {
			t.Fatal(err)
		}
		run(t, ctx, environment(dir), daemonBinary, "token", "create", "--config", daemonConfig, "--output", d.token)
		d.cli = func(args ...string) []byte {
			return run(t, ctx, environment(home), cliBinary, append([]string{"--config", d.config, "--json"}, args...)...)
		}
		// Shutdown addresses this exact private configuration, including on failure.
		defer func() {
			cleanup, stop := context.WithTimeout(context.Background(), 15*time.Second)
			defer stop()
			command := exec.CommandContext(cleanup, cliBinary, "--config", d.config, "agent", "stop")
			command.Env = environment(home)
			_ = command.Run()
		}()
		t.Cleanup(func() {
			if t.Failed() {
				data, _ := os.ReadFile(filepath.Join(settings.Agent.StateDir, "agent.log"))
				t.Logf("device %d agent.log:\n%s", i, data)
			}
		})
		devices[i] = d
	}
	server := start(t, ctx, environment(dir), filepath.Join(dir, "daemon.log"), daemonBinary, "serve", "--config", daemonConfig)
	endpoint, err := connection.NewTLSClient("https://"+address, connection.TLSOptions{RootCAs: roots, ServerName: "localhost"})
	if err != nil {
		t.Fatal(err)
	}
	defer endpoint.Close()
	token, err := os.ReadFile(devices[0].token)
	if err != nil {
		t.Fatal(err)
	}
	credential, err := connection.BearerToken(strings.TrimSpace(string(token)))
	if err != nil {
		t.Fatal(err)
	}
	remote := rpc.NewShikiDaemonClient(connection.NewRPCClient(endpoint, credential))
	await(t, ctx, func(probe context.Context) error {
		_, err := remote.NegotiateProtocol(probe, &pb.ProtocolOffer{Major: 1})
		return err
	})
	for _, d := range devices {
		d.cli("connect", "shared", "--tls-address", address, "--tls-server-name", "localhost", "--tls-ca", cert, "--token-file", d.token)
		d.client = openDeviceFrontend(t, ctx, d.config)
		defer func() { _ = d.client.Close() }()
	}
	a, b := devices[0], devices[1]
	firstA, firstB := a.client.InstanceID(), b.client.InstanceID()
	if firstA == firstB {
		t.Fatal("separate device configurations reused one agent instance")
	}
	remoteRoot := filepath.Join(dir, "shared-project")
	for _, root := range []string{remoteRoot, a.root, b.root} {
		if err := os.Mkdir(root, 0700); err != nil {
			t.Fatal(err)
		}
		writeDeviceFile(t, root, "common baseline")
	}
	project := new(pb.Project)
	decode(t, a.cli("projects", "create", "shared-project", "--path", remoteRoot), project)
	created := new(pb.Session)
	decode(t, a.cli("sessions", "create", "shared-terminal", "--project", project.Id), created)
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
	a.cli("plugins", "enable", "sync", "--side", "remote", "--server", "shared")
	for _, d := range devices {
		d.cli("plugins", "enable", "sync", "--side", "local")
		mappings := new(ppb.MappingList)
		decode(t, d.cli("sync", "map", project.Id, "--local", d.root), mappings)
		if len(mappings.Mappings) != 1 {
			t.Fatalf("device must own exactly one mapping: %v", mappings)
		}
		d.mapping = mappings.Mappings[0]
		d.cli("sync", "resume", d.mapping.Id)
		awaitDeviceSync(t, ctx, d.client, d.mapping, remoteRoot, "common baseline")
	}
	if a.mapping.Id == b.mapping.Id || a.mapping.LocalRoot == b.mapping.LocalRoot {
		t.Fatal("independent agents shared a mapping identity or root")
	}
	for _, d := range devices {
		mappings, err := d.client.Agent().ListMappings(ctx, &apb.ListMappingsRequest{})
		if err != nil || len(mappings.GetMappings()) != 1 || mappings.Mappings[0].Id != d.mapping.Id || mappings.Mappings[0].LocalRoot != d.root {
			t.Fatalf("agent mappings crossed device boundaries: %v, %v", mappings, err)
		}
	}

	// A separate initial frontend makes both contenders writable observers.
	initial := openDeviceFrontend(t, ctx, a.config)
	defer initial.Close()
	initialTerminal := openDeviceTerminal(t, ctx, initial, a.mapping.ServerId, created.Id, false)
	initialTerminal.expectMode(t, "controller")
	ta := openDeviceTerminal(t, ctx, a.client, a.mapping.ServerId, created.Id, false)
	tb := openDeviceTerminal(t, ctx, b.client, b.mapping.ServerId, created.Id, false)
	ta.expectMode(t, "observer")
	tb.expectMode(t, "observer")
	reader := openDeviceFrontend(t, ctx, b.config)
	defer reader.Close()
	readOnly := openDeviceTerminal(t, ctx, reader, b.mapping.ServerId, created.Id, true)
	readOnly.expectMode(t, "observer")
	before := deviceSession(t, ctx, a.client, a.mapping.ServerId, created.Id)
	if _, err := reader.Remote(b.mapping.ServerId).TakeControl(ctx, &pb.TakeControlRequest{SessionId: created.Id, AttachmentId: readOnly.id, ExpectedControlRevision: before.ControlRevision, RequestId: rand.Text()}); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("read-only attachment takeover = %v, want PermissionDenied", err)
	}
	gate, results := make(chan struct{}), make(chan error, 2)
	for i, attachment := range []*deviceTerminal{ta, tb} {
		d := devices[i]
		go func() {
			<-gate
			_, err := d.client.Remote(d.mapping.ServerId).TakeControl(ctx, &pb.TakeControlRequest{SessionId: created.Id, AttachmentId: attachment.id, ExpectedControlRevision: before.ControlRevision, RequestId: rand.Text()})
			results <- err
		}()
	}
	close(gate)
	successes, aborted := 0, 0
	for range 2 {
		switch err := <-results; {
		case err == nil:
			successes++
		case connect.CodeOf(err) == connect.CodeAborted:
			aborted++
		default:
			t.Fatalf("competing takeover: %v", err)
		}
	}
	if successes != 1 || aborted != 1 {
		t.Fatalf("same-revision takeover: %d success, %d Aborted", successes, aborted)
	}
	current := deviceSession(t, ctx, b.client, b.mapping.ServerId, created.Id)
	if current.ControlRevision == before.ControlRevision || (current.ControllerAttachmentId != ta.id && current.ControllerAttachmentId != tb.id) {
		t.Fatalf("takeover failed to commit one contender: %v", current)
	}
	if _, err := b.client.Remote(b.mapping.ServerId).TakeControl(ctx, &pb.TakeControlRequest{SessionId: created.Id, AttachmentId: tb.id, ExpectedControlRevision: current.ControlRevision, RequestId: rand.Text()}); err != nil {
		t.Fatal(err)
	}
	tb.send(t, "both-devices-observe")
	for _, terminal := range []*deviceTerminal{ta, tb, readOnly} {
		terminal.expectText(t, ctx, "TWO_AGENT_REPLY:both-devices-observe")
		terminal.expectEvent(t, ctx, pb.SessionEvent_RESET)
		terminal.expectEvent(t, ctx, pb.SessionEvent_CONTROL_CHANGED)
	}

	// Stopping A must cancel A's streams only, without restarting B or Claude.
	a.cli("agent", "stop")
	await(t, ctx, func(context.Context) error {
		ta.mu.Lock()
		defer ta.mu.Unlock()
		if ta.err == nil {
			return errors.New("device A terminal still open after agent stop")
		}
		return nil
	})
	tb.send(t, "device-b-survives-a-stop")
	tb.expectText(t, ctx, "TWO_AGENT_REPLY:device-b-survives-a-stop")
	status, err := b.client.Agent().GetStatus(ctx, &pb.Empty{})
	if err != nil || status.GetInstanceId() != firstB {
		t.Fatalf("stopping A restarted or disconnected B: %v, %v", status, err)
	}
	after := deviceSession(t, ctx, b.client, b.mapping.ServerId, created.Id)
	if after.ClaudePid == 0 || after.ClaudePid != before.ClaudePid || after.ControllerAttachmentId != tb.id {
		t.Fatalf("stopping A changed B control or Claude process: before=%v after=%v", before, after)
	}
	writeDeviceFile(t, a.root, "device A offline change")
	writeDeviceFile(t, b.root, "device B first commit")
	awaitDeviceSync(t, ctx, b.client, b.mapping, remoteRoot, "device B first commit")
	a.cli("agent", "start")
	_ = a.client.Close()
	a.client = openDeviceFrontend(t, ctx, a.config)
	if a.client.InstanceID() == firstA || a.client.InstanceID() == firstB {
		t.Fatal("device A restart did not create its own new instance")
	}
	conflict := awaitDeviceConflict(t, ctx, a.client, a.mapping, "")
	assertDeviceConflict(t, conflict, "common baseline", "device A offline change", "device B first commit")
	assertDeviceFile(t, a.root, "device A offline change")
	assertDeviceFile(t, remoteRoot, "device B first commit")
	writeDeviceFile(t, b.root, "device B newer commit")
	awaitDeviceSync(t, ctx, b.client, b.mapping, remoteRoot, "device B newer commit")
	_, err = a.client.Agent().ResolveConflict(ctx, &ppb.ResolveConflictRequest{MappingId: a.mapping.Id, ConflictId: conflict.Id, ExpectedGeneration: conflict.Generation, Choice: ppb.ResolveConflictRequest_KEEP_LOCAL, RequestId: rand.Text()})
	if connect.CodeOf(err) != connect.CodeAborted {
		t.Fatalf("stale conflict choice = %v, want Aborted", err)
	}
	assertDeviceFile(t, a.root, "device A offline change")
	assertDeviceFile(t, b.root, "device B newer commit")
	assertDeviceFile(t, remoteRoot, "device B newer commit")
	latest := awaitDeviceConflict(t, ctx, a.client, a.mapping, conflict.Generation)
	assertDeviceConflict(t, latest, "common baseline", "device A offline change", "device B newer commit")
	if _, err := a.client.Agent().ResolveConflict(ctx, &ppb.ResolveConflictRequest{MappingId: a.mapping.Id, ConflictId: latest.Id, ExpectedGeneration: latest.Generation, Choice: ppb.ResolveConflictRequest_KEEP_REMOTE, RequestId: rand.Text()}); err != nil {
		t.Fatal(err)
	}
	awaitDeviceSync(t, ctx, a.client, a.mapping, a.root, "device B newer commit")
	writeDeviceFile(t, a.root, "device A subsequent conditional commit")
	awaitDeviceSync(t, ctx, a.client, a.mapping, remoteRoot, "device A subsequent conditional commit")
	awaitDeviceSync(t, ctx, b.client, b.mapping, b.root, "device A subsequent conditional commit")
	for _, root := range []string{a.root, b.root, remoteRoot} {
		assertDeviceFile(t, root, "device A subsequent conditional commit")
	}
	for _, d := range devices {
		d.cli("agent", "stop")
	}
	if err := server.command.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	server.wait(t, ctx, false)
}

func openDeviceFrontend(t *testing.T, ctx context.Context, path string) *frontend.Client {
	t.Helper()
	client, err := frontend.Open(ctx, path, frontend.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func deviceSession(t *testing.T, ctx context.Context, client *frontend.Client, server, session string) *pb.Session {
	t.Helper()
	result, err := client.Remote(server).GetSession(ctx, &pb.GetSessionRequest{Id: session})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func writeDeviceFile(t *testing.T, root, contents string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, "shared.txt"), []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
}

func assertDeviceFile(t *testing.T, root, want string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "shared.txt"))
	if err != nil || string(data) != want {
		t.Fatalf("%s: got %q, want %q: %v", root, data, want, err)
	}
}

func deviceScope(mapping *ppb.Mapping) *ppb.Scope {
	return &ppb.Scope{ServerId: mapping.ServerId, ProjectId: mapping.ProjectId, MappingId: mapping.Id}
}

func awaitDeviceSync(t *testing.T, ctx context.Context, client *frontend.Client, mapping *ppb.Mapping, root, want string) {
	t.Helper()
	await(t, ctx, func(probe context.Context) error {
		data, err := os.ReadFile(filepath.Join(root, "shared.txt"))
		if err != nil || string(data) != want {
			return fmt.Errorf("waiting for %s contents %q: got %q (%v)", root, want, data, err)
		}
		state, err := client.Agent().GetSyncState(probe, deviceScope(mapping))
		if err != nil {
			return err
		}
		if state.State != "active" || state.PendingFiles != 0 || state.ConflictCount != 0 {
			return fmt.Errorf("sync baseline not settled: %v", state)
		}
		return nil
	})
}

func awaitDeviceConflict(t *testing.T, ctx context.Context, client *frontend.Client, mapping *ppb.Mapping, previous string) *ppb.Conflict {
	t.Helper()
	var result *ppb.Conflict
	await(t, ctx, func(probe context.Context) error {
		conflicts, err := client.Agent().ListConflicts(probe, deviceScope(mapping))
		if err != nil {
			return err
		}
		if len(conflicts.Conflicts) != 1 || conflicts.Conflicts[0].Path != "shared.txt" || conflicts.Conflicts[0].Generation == "" || conflicts.Conflicts[0].Generation == previous {
			return fmt.Errorf("waiting for current shared-file conflict: %v", conflicts)
		}
		result = conflicts.Conflicts[0]
		return nil
	})
	return result
}

func assertDeviceConflict(t *testing.T, conflict *ppb.Conflict, base, local, remote string) {
	t.Helper()
	for i, version := range []*ppb.FileVersion{conflict.Base, conflict.Local, conflict.Remote} {
		want := fmt.Sprintf("%x", sha256.Sum256([]byte([]string{base, local, remote}[i])))
		if version == nil || !version.Exists || version.ContentHash != want {
			t.Fatalf("conflict side %d does not preserve the observed version: %v", i, conflict)
		}
	}
	if conflict.Remote.Revision == "" || conflict.Base.Revision == "" || conflict.Remote.Revision == conflict.Base.Revision {
		t.Fatalf("conflict lost conditional remote revisions: %v", conflict)
	}
}

type deviceTerminal struct {
	stream    rpc.ShikiDaemonTerminalStreamClientStream
	id        string
	mode      string
	mu        sync.Mutex
	text      string
	textLimit int
	events    map[pb.SessionEvent_Type]bool
	err       error
}

func openDeviceTerminal(t *testing.T, ctx context.Context, client *frontend.Client, server, session string, readOnly bool) *deviceTerminal {
	t.Helper()
	call, cancel := context.WithCancel(ctx)
	t.Cleanup(cancel)
	timeout := time.AfterFunc(5*time.Second, cancel)
	defer timeout.Stop()
	var stream rpc.ShikiDaemonTerminalStreamClientStream
	var ready *pb.TerminalOutput
	await(t, call, func(context.Context) error {
		var err error
		stream, err = client.Remote(server).TerminalStream(call)
		if err == nil {
			err = stream.Send(&pb.TerminalInput{Payload: &pb.TerminalInput_Open{Open: &pb.TerminalOpen{SessionId: session, ReadOnly: readOnly, Size: &pb.ResizeEvent{Rows: 30, Cols: 100}, DeviceName: "same-host-independent-agent-test"}}})
			if err == nil {
				ready, err = stream.Receive()
			}
		}
		if err != nil {
			_ = stream.Close()
			if connect.CodeOf(err) != connect.CodeResourceExhausted {
				t.Fatalf("terminal admission: %v", err)
			}
		}
		return err
	})
	if ready.GetEvent().GetType() != pb.SessionEvent_READY || ready.AttachmentId == "" || ready.SessionId != session {
		t.Fatalf("invalid terminal READY: %v", ready)
	}
	terminal := &deviceTerminal{stream: stream, id: ready.AttachmentId, mode: ready.GetEvent().Data["mode"], events: make(map[pb.SessionEvent_Type]bool)}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			message, err := stream.Receive()
			terminal.mu.Lock()
			if err != nil {
				terminal.err = err
				terminal.mu.Unlock()
				return
			}
			terminal.text += string(message.GetData())
			if terminal.textLimit > 0 && len(terminal.text) > terminal.textLimit {
				terminal.text = terminal.text[len(terminal.text)-terminal.textLimit:]
			}
			if event := message.GetEvent(); event != nil {
				terminal.events[event.Type] = true
			}
			terminal.mu.Unlock()
		}
	}()
	t.Cleanup(func() {
		cancel()
		_ = stream.Close()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("terminal receiver did not stop")
		}
	})
	return terminal
}

func (terminal *deviceTerminal) expectMode(t *testing.T, want string) {
	t.Helper()
	if terminal.mode != want {
		t.Fatalf("attachment mode %q, want %q", terminal.mode, want)
	}
}

func (terminal *deviceTerminal) send(t *testing.T, marker string) {
	t.Helper()
	if err := terminal.stream.Send(&pb.TerminalInput{Payload: &pb.TerminalInput_Data{Data: []byte(marker + "\n")}}); err != nil {
		t.Fatal(err)
	}
}

func (terminal *deviceTerminal) expectText(t *testing.T, ctx context.Context, marker string) {
	t.Helper()
	await(t, ctx, func(context.Context) error {
		terminal.mu.Lock()
		defer terminal.mu.Unlock()
		if strings.Contains(terminal.text, marker) {
			return nil
		}
		return fmt.Errorf("terminal waiting for %q: %v, output %q", marker, terminal.err, terminal.text)
	})
}

func (terminal *deviceTerminal) expectEvent(t *testing.T, ctx context.Context, event pb.SessionEvent_Type) {
	t.Helper()
	await(t, ctx, func(context.Context) error {
		terminal.mu.Lock()
		defer terminal.mu.Unlock()
		if terminal.events[event] {
			return nil
		}
		return fmt.Errorf("terminal waiting for %s: %v", event, terminal.err)
	})
}
