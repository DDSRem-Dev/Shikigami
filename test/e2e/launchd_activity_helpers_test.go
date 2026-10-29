//go:build darwin && e2e

package e2e

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	"github.com/DDSRem-Dev/Shikigami/pkg/config"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
	processinfo "github.com/shirou/gopsutil/v4/process"
)

func launchdActivityAbsent(t *testing.T, ctx context.Context, domain, target, service string) {
	t.Helper()
	if err := exec.CommandContext(ctx, "/bin/launchctl", "print", domain).Run(); err != nil {
		t.Fatal("actual GUI launchd domain unavailable", err)
	}
	if _, err := os.Lstat(service); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("existing native plist must not be replaced", err)
	}
	if output, err := exec.CommandContext(ctx, "/bin/launchctl", "print", target).CombinedOutput(); err == nil {
		t.Fatalf("existing native job must not be replaced: %s", output)
	} else if !strings.Contains(string(output), "Could not find service") {
		t.Fatalf("could not establish job absence: %v %s", err, output)
	}
}

func launchdActivityDaemon(t *testing.T, ctx, processes context.Context, root, bundle string, environment []string, username string, evidence *launchdActivityEvidence) (*config.Daemon, string, string, string, string, *process) {
	t.Helper()
	cert, key, roots := certificate(t, root)
	reservation, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := reservation.Addr().String()
	reservation.Close()
	ledger := filepath.Join(root, "terminal-ledger")
	fixture := filepath.Join(root, "terminal-fixture")
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }
	script := "#!/bin/sh\nif [ \"${1:-}\" = --version ]; then printf '0.0.0-launchd-activity-fixture\\n'; exit 0; fi\nprintf 'SYSTEMD_FIXTURE_READY\\n'\nwhile IFS= read -r line; do printf '%s\\n' \"$line\" >> " + quote(ledger) + "; printf 'REPLY:%s\\n' \"$line\"; done\n"
	if err := os.WriteFile(fixture, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	configuration := filepath.Join(root, "daemon.yaml")
	binary := filepath.Join(bundle, "shiki-daemon")
	run(t, ctx, environment, binary, "init", "--config", configuration, "--user", username, "--state-dir", filepath.Join(root, "daemon-state"), "--projects-dir", filepath.Join(root, "projects"), "--tailscale=false", "--direct-tls-address", address, "--tls-cert", cert, "--tls-key", key, "--claude-executable", fixture)
	settings, err := config.LoadDaemon(configuration)
	if err != nil {
		t.Fatal(err)
	}
	settings.PluginsDir = bundle
	if err := config.SaveDaemon(configuration, settings); err != nil {
		t.Fatal(err)
	}
	token := filepath.Join(root, "token")
	run(t, ctx, environment, binary, "token", "create", "--config", configuration, "--output", token, "--expires", "2h")
	daemon := start(t, processes, environment, filepath.Join(root, "daemon.log"), binary, "serve", "--config", configuration)
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_ = daemon.command.Process.Signal(syscall.SIGTERM)
		daemon.wait(t, cleanup, false)
	})
	evidence.process("daemon", daemon.command.Process.Pid)
	endpoint, err := connection.NewTLSClient("https://"+address, connection.TLSOptions{RootCAs: roots, ServerName: "localhost"})
	if err != nil {
		t.Fatal(err)
	}
	defer endpoint.Close()
	secret, err := os.ReadFile(token)
	if err != nil {
		t.Fatal(err)
	}
	credential, err := connection.BearerToken(strings.TrimSpace(string(secret)))
	clear(secret)
	if err != nil {
		t.Fatal(err)
	}
	remote := rpc.NewShikiDaemonClient(connection.NewRPCClient(endpoint, credential))
	await(t, ctx, func(call context.Context) error {
		_, err := remote.NegotiateProtocol(call, &pb.ProtocolOffer{Major: 1})
		return err
	})
	return settings, cert, token, address, ledger, daemon
}

type launchdActivityProcess struct {
	rotationProcessIdentity
	Start string `json:"ps_start"`
}
type launchdActivitySession struct {
	ID      string `json:"session_id"`
	TmuxID  string `json:"tmux_id"`
	PaneID  string `json:"pane_id"`
	PID     int    `json:"pid"`
	Fixture string `json:"fixture"`
}
type launchdActivityEvidence struct {
	t            *testing.T
	ctx          context.Context
	root, output string
	processes    []launchdActivityProcess
	sessions     []launchdActivitySession
}

func newLaunchdActivityEvidence(t *testing.T, ctx context.Context, root string) *launchdActivityEvidence {
	e := &launchdActivityEvidence{t: t, ctx: ctx, root: root, output: os.Getenv("SHIKI_RUN_EVIDENCE_DIR")}
	e.process("test", os.Getpid())
	e.record("fixture", map[string]any{"root": root, "real_home": os.Getenv("HOME"), "isolated_runtime": filepath.Join(root, "runtime"), "service_label": "dev.shikigami.agent"})
	return e
}
func (e *launchdActivityEvidence) process(role string, pid int) rotationProcessIdentity {
	e.t.Helper()
	p, err := processinfo.NewProcessWithContext(e.ctx, int32(pid))
	if err != nil {
		e.t.Fatal(err)
	}
	created, err := p.CreateTimeWithContext(e.ctx)
	if err != nil {
		e.t.Fatal(err)
	}
	exe, err := p.ExeWithContext(e.ctx)
	if err != nil {
		e.t.Fatal(err)
	}
	identity := rotationProcessIdentity{Role: role, PID: pid, Created: created, Executable: exe}
	start, err := exec.CommandContext(e.ctx, "/bin/ps", "-p", fmt.Sprint(pid), "-o", "lstart=").Output()
	if err != nil {
		e.t.Fatal(err)
	}
	e.processes = append(e.processes, launchdActivityProcess{rotationProcessIdentity: identity, Start: strings.TrimSpace(string(start))})
	e.persist()
	e.record("process", identity)
	return identity
}
func (e *launchdActivityEvidence) children(role string, parent int, prefix string) []rotationProcessIdentity {
	e.t.Helper()
	var result []rotationProcessIdentity
	launchdWait(e.t, e.ctx, 15*time.Second, func() error {
		pids := launchdPluginPIDs(e.t, e.ctx, parent, prefix)
		if len(pids) != 2 {
			return fmt.Errorf("expected two %s workers, found %v", role, pids)
		}
		for _, pid := range pids {
			result = append(result, e.process(role+"-worker", pid))
		}
		return nil
	})
	return result
}
func (e *launchdActivityEvidence) sessionIntent(record launchdActivitySession) {
	for index, current := range e.sessions {
		if current.ID == record.ID {
			e.sessions[index] = record
			e.persist()
			return
		}
	}
	e.sessions = append(e.sessions, record)
	e.persist()
}

func (e *launchdActivityEvidence) persist() {
	value := map[string]any{"processes": e.processes, "sessions": e.sessions}
	paths := []string{filepath.Join(e.root, "runtime", "activity-owned.json")}
	if e.output != "" {
		paths = append(paths, filepath.Join(e.output, "processes.json"))
	}
	for _, path := range paths {
		data, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			e.t.Fatal(err)
		}
		if err := os.WriteFile(path+".tmp", data, 0600); err != nil {
			e.t.Fatal(err)
		}
		if err := os.Rename(path+".tmp", path); err != nil {
			e.t.Fatal(err)
		}
	}
}
func (e *launchdActivityEvidence) record(kind string, value any) {
	e.t.Helper()
	data, err := json.Marshal(map[string]any{"event": kind, "time": time.Now().UTC(), "value": value})
	if err != nil {
		e.t.Fatal(err)
	}
	e.t.Logf("LAUNCHD_ACTIVITY_EVENT %s", data)
	if e.output != "" {
		f, err := os.OpenFile(filepath.Join(e.output, "launchd-activity-events.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			e.t.Fatal(err)
		}
		_, writeErr := f.Write(append(data, '\n'))
		closeErr := f.Close()
		if writeErr != nil || closeErr != nil {
			e.t.Fatal(writeErr, closeErr)
		}
	}
}
func (e *launchdActivityEvidence) control(directory, name string) {
	data, err := os.ReadFile(filepath.Join(directory, name+".json"))
	if err != nil {
		e.t.Fatal(err)
	}
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		e.t.Fatal(err)
	}
	e.record(name, value)
	if e.output != "" {
		if err := os.WriteFile(filepath.Join(e.output, name+".json"), data, 0600); err != nil {
			e.t.Fatal(err)
		}
	}
}

func launchdActivityRemoveSession(ctx context.Context, record launchdActivitySession) error {
	target := "shiki-" + record.ID
	if !strings.HasPrefix(record.ID, "sess_") || strings.ContainsAny(record.ID, " \t\n:;") {
		return errors.New("invalid immutable session ID")
	}
	args := []string{"-L", fmt.Sprintf("shiki-%d", os.Getuid())}
	format := "#{session_name}\t#{session_id}\t#{pane_id}\t#{pane_pid}\t#{pane_start_command}"
	output, err := exec.CommandContext(ctx, "tmux", append(args, "list-panes", "-t", "="+target, "-F", format)...).CombinedOutput()
	if err != nil {
		inventory, listErr := exec.CommandContext(ctx, "tmux", append(args, "list-sessions", "-F", "#{session_name}")...).CombinedOutput()
		if listErr == nil {
			for _, name := range strings.Split(strings.TrimSpace(string(inventory)), "\n") {
				if name == target {
					return fmt.Errorf("owned session remains but inspection failed: %w", err)
				}
			}
			return nil
		}
		if strings.Contains(string(inventory), "no server running") || strings.Contains(string(inventory), "No such file or directory") {
			return nil
		}
		return fmt.Errorf("cannot establish owned session absence: %w", listErr)
	}
	rows := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(rows) != 1 {
		return errors.New("owned session has multiple panes; refusing cleanup")
	}
	fields := strings.SplitN(rows[0], "\t", 5)
	if len(fields) != 5 || fields[0] != target || !strings.Contains(fields[4], record.Fixture) {
		return errors.New("owned fixture start command/session changed; refusing cleanup")
	}
	if record.TmuxID != "" && (fields[1] != record.TmuxID || fields[2] != record.PaneID || fields[3] != fmt.Sprint(record.PID)) {
		return errors.New("owned pane identity changed; refusing cleanup")
	}
	if out, err := exec.CommandContext(ctx, "tmux", append(args, "kill-session", "-t", fields[1])...).CombinedOutput(); err != nil {
		return fmt.Errorf("owned session deletion: %w %s", err, out)
	}
	return nil
}
