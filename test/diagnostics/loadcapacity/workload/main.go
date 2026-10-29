//go:build shiki_load_capacity

// The driver runs real shipped processes. It is never linked into a host/plugin.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	frontend "github.com/DDSRem-Dev/Shikigami/internal/client"
	"github.com/DDSRem-Dev/Shikigami/pkg/config"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func need(err error) {
	if err != nil {
		panic(err)
	}
}
func take[T any](value T, err error) T { need(err); return value }
func ensure(ok bool, why string) {
	if !ok {
		panic(errors.New(why))
	}
}
func save(path string, value any) {
	file := take(os.CreateTemp(filepath.Dir(path), ".capacity-evidence-*"))
	defer os.Remove(file.Name())
	need(file.Chmod(0600))
	_, err := file.Write(append(take(json.MarshalIndent(value, "", "  ")), '\n'))
	need(err)
	need(file.Sync())
	need(file.Close())
	need(os.Rename(file.Name(), path))
}
func decode(data []byte, value proto.Message) { need(protojson.Unmarshal(data, value)) }
func until(ctx context.Context, probe func() bool) {
	for !probe() {
		select {
		case <-ctx.Done():
			panic(ctx.Err())
		case <-time.After(20 * time.Millisecond):
		}
	}
}

type environment struct {
	ctx                                             context.Context
	root, bin, output, configuration, local, remote string
	env                                             []string
	cleanups                                        []func()
	daemon                                          *exec.Cmd
	client                                          *frontend.Client
	project                                         *pb.Project
	session                                         *pb.Session
	mapping                                         *ppb.Mapping
	terminal                                        *terminal
	target                                          *target
	forward                                         *ppb.ForwardState
	report                                          report
	commandSequence                                 int
	agentPID                                        int32
	cleanupVerified                                 bool
	directAddress                                   string
}

func (e *environment) deferClose(f func()) { e.cleanups = append(e.cleanups, f) }
func (e *environment) close() {
	for i := len(e.cleanups) - 1; i >= 0; i-- {
		func() {
			defer func() {
				if r := recover(); r != nil {
					e.report.Errors = append(e.report.Errors, fmt.Sprint(r))
				}
			}()
			e.cleanups[i]()
		}()
	}
}
func (e *environment) command(ctx context.Context, name string, args ...string) []byte {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = e.env
	out, err := cmd.CombinedOutput()
	if err != nil {
		e.commandSequence++
		log := filepath.Join(e.output, fmt.Sprintf("private-command-error-%03d.log", e.commandSequence))
		need(os.WriteFile(log, out, 0600))
		panic(fmt.Errorf("command %s failed: %w (private error log %s)", filepath.Base(name), err, filepath.Base(log)))
	}
	return out
}
func (e *environment) cli(args ...string) []byte {
	ctx, cancel := context.WithTimeout(e.ctx, 15*time.Second)
	defer cancel()
	return e.command(ctx, filepath.Join(e.bin, "shiki"), append([]string{"--config", e.configuration, "--json", "--timeout", "10s"}, args...)...)
}
func (e *environment) setup() {
	ensure(os.Geteuid() != 0, "application driver must run as an ordinary user")
	account := take(user.Current())
	e.root = take(os.MkdirTemp("/tmp", "shiki-capacity-private-"))
	need(os.Chmod(e.root, 0700))
	e.deferClose(func() {
		for _, name := range []string{"daemon.log", "agent-state/agent.log"} {
			data, err := os.ReadFile(filepath.Join(e.root, name))
			if err == nil {
				need(os.WriteFile(filepath.Join(e.output, "private-"+filepath.Base(name)), data, 0600))
			}
		}
	})
	e.env = []string{"HOME=" + e.root, "USER=" + account.Username, "PATH=" + e.bin + string(os.PathListSeparator) + os.Getenv("PATH"), "TERM=xterm-256color", "LANG=C.UTF-8", "GOMAXPROCS=2"}
	e.configuration = filepath.Join(e.root, "client.yaml")
	e.recordRuntime()
	fixture := filepath.Join(e.root, "capacity-terminal-fixture")
	need(os.WriteFile(fixture, []byte("#!/bin/sh\nif [ \"${1:-}\" = --version ]; then echo 0.0.0-capacity-fixture; exit 0; fi\nif [ \"${1:-} ${2:-} ${3:-}\" = 'auth status --json' ]; then echo '{\"loggedIn\":true}'; exit 0; fi\nprintf 'CAPACITY_READY\\n'\nwhile IFS= read -r line; do printf 'CAPACITY_REPLY:%s\\n' \"$line\"; done\n"), 0700))
	cert, key := certificate(e.root)
	listener := take(net.Listen("tcp4", "127.0.0.1:0"))
	address := listener.Addr().String()
	e.directAddress = address
	need(listener.Close())
	daemonConfig := filepath.Join(e.root, "daemon.yaml")
	daemonBin := filepath.Join(e.bin, "shiki-daemon")
	e.command(e.ctx, daemonBin, "init", "--config", daemonConfig, "--user", account.Username, "--state-dir", filepath.Join(e.root, "daemon-state"), "--projects-dir", filepath.Join(e.root, "projects"), "--tailscale=false", "--direct-tls-address", address, "--tls-cert", cert, "--tls-key", key, "--claude-executable", fixture)
	daemonSettings := take(config.LoadDaemon(daemonConfig))
	daemonSettings.PluginsDir = e.bin
	need(config.SaveDaemon(daemonConfig, daemonSettings))
	token := filepath.Join(e.root, "device.token")
	e.command(e.ctx, daemonBin, "token", "create", "--config", daemonConfig, "--output", token)
	log := take(os.OpenFile(filepath.Join(e.root, "daemon.log"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600))
	e.deferClose(func() { need(log.Close()) })
	e.daemon = exec.Command(daemonBin, "serve", "--config", daemonConfig)
	e.daemon.Env = e.env
	e.daemon.Stdout = log
	e.daemon.Stderr = log
	need(e.daemon.Start())
	e.deferClose(func() { stopProcess(e.daemon) })
	e.remember([]processIdentity{identity(int32(e.daemon.Process.Pid))})
	c := config.DefaultClient()
	c.Agent.Socket = filepath.Join(e.root, "run", "agent.sock")
	c.Agent.StateDir = filepath.Join(e.root, "agent-state")
	c.Tailscale.Enabled = false
	c.Tailscale.StateDir = filepath.Join(e.root, "node")
	c.PluginsDir = e.bin
	need(config.SaveClient(e.configuration, c))
	e.deferClose(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		e.command(ctx, filepath.Join(e.bin, "shiki"), "--config", e.configuration, "agent", "stop")
	})
	e.cli("agent", "start")
	e.agentPID = int32(take(strconv.Atoi(strings.TrimSpace(string(take(os.ReadFile(filepath.Join(e.root, "agent-state", "instance.lock"))))))))
	e.remember([]processIdentity{identity(e.agentPID)})
	e.cli("connect", "capacity", "--tls-address", address, "--tls-server-name", "localhost", "--tls-ca", cert, "--token-file", token)
	e.client = take(frontend.Open(e.ctx, e.configuration, frontend.Options{Timeout: 5 * time.Second}))
	e.deferClose(func() { need(e.client.Close()) })
	e.remote = filepath.Join(e.root, "projects", "capacity")
	e.local = filepath.Join(e.root, "local")
	for _, path := range []string{e.remote, e.local} {
		need(os.MkdirAll(path, 0700))
		for _, file := range []string{"to-remote.bin", "to-local.bin"} {
			need(os.WriteFile(filepath.Join(path, file), []byte("common capacity baseline\n"), 0600))
		}
	}
	e.project = new(pb.Project)
	decode(e.cli("projects", "create", "capacity", "--path", e.remote), e.project)
	e.session = new(pb.Session)
	decode(e.cli("sessions", "create", "capacity-pane", "--project", e.project.Id), e.session)
	e.deferClose(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		e.command(ctx, "tmux", "-L", fmt.Sprintf("shiki-%d", os.Getuid()), "kill-session", "-t", "shiki-"+e.session.Id)
	})
	e.recordRuntime()
	for _, plugin := range []string{"sync", "port"} {
		e.cli("--server", "capacity", "plugins", "enable", plugin, "--side", "remote")
		e.waitPlugin(plugin, "remote")
		e.rememberWorkers()
		e.cli("plugins", "enable", plugin, "--side", "local")
		e.waitPlugin(plugin, "local")
		e.rememberWorkers()
	}
	mappings := new(ppb.MappingList)
	decode(e.cli("--server", "capacity", "sync", "map", e.project.Id, "--local", e.local), mappings)
	ensure(len(mappings.Mappings) == 1, "one private mapping expected")
	e.mapping = mappings.Mappings[0]
	e.cli("sync", "resume", e.mapping.Id)
	ctx, cancel := context.WithTimeout(e.ctx, 30*time.Second)
	defer cancel()
	until(ctx, e.syncSettled)
	e.target = newTarget()
	e.deferClose(e.target.close)
	e.setLimits(128, 128)
	e.terminal = openTerminal(e)
	ensure(strings.HasPrefix(e.session.Id, "sess_"), "session identity missing")
}

func execute() (code int) {
	bin := flag.String("bin", "", "built ordinary application binaries")
	out := flag.String("output", "", "private evidence directory")
	precheck := flag.Bool("precheck", false, "one small harness round, never capacity acceptance")
	terminalProbe := flag.Bool("terminal-probe", false, "fixed512-marker synthetic terminal parser diagnostic; no capacity claim")
	admissionProbe := flag.Bool("admission-probe", false, "fixed two128-holder admission cases only; no sweep claim")
	flag.Parse()
	e := &environment{bin: *bin, output: *out}
	e.report.Mode = "formal"
	if *precheck {
		e.report.Mode = "harness-precheck"
	}
	if *terminalProbe {
		ensure(!*precheck, "diagnostic modes are exclusive")
		e.report.Mode = "terminal-parser-diagnostic"
	}
	if *admissionProbe {
		ensure(!*precheck && !*terminalProbe, "diagnostic modes are exclusive")
		e.report.Mode = "admission-route-diagnostic"
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	e.ctx = ctx
	defer func() {
		if r := recover(); r != nil {
			e.report.Errors = append(e.report.Errors, fmt.Sprint(r))
			code = 1
		}
		if e.daemon != nil && e.agentPID > 0 {
			func() {
				defer func() {
					if r := recover(); r != nil {
						e.report.Errors = append(e.report.Errors, "final worker ownership observation: "+fmt.Sprint(r))
					}
				}()
				e.rememberWorkers()
			}()
		}
		e.close()
		func() {
			defer func() {
				if r := recover(); r != nil {
					e.report.Errors = append(e.report.Errors, "cleanup verification: "+fmt.Sprint(r))
				}
			}()
			e.verifyCleanup()
			e.finishCleanup()
		}()
		if len(e.report.Errors) > 0 {
			code = 1
		}
		e.report.Passed = code == 0
		save(filepath.Join(e.output, "result.json"), e.report)
	}()
	ensure(e.bin != "" && e.output != "", "bin/output required")
	e.setup()
	if *terminalProbe {
		e.report.Tracked = e.identities()
		e.remember(e.report.Tracked)
		e.report.Runtime = runtimeRecord()
		e.terminal.collect(e.ctx, nil, 1000, 512, &e.report.IdleBefore)
		ensure(len(e.report.IdleBefore) == 512, "terminal diagnostic did not finish all512 markers")
		need(terminalError(e.terminal))
	} else if *admissionProbe {
		e.report.Tracked = e.identities()
		e.remember(e.report.Tracked)
		e.report.Runtime = runtimeRecord()
		e.admission()
		need(terminalError(e.terminal))
	} else {
		e.run(*precheck)
	}
	need(e.ctx.Err())
	return 0
}
func main() { os.Exit(execute()) }
