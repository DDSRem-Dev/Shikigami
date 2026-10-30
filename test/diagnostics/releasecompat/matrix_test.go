//go:build releasecompat

package releasecompat_test

import (
	"bytes"
	"encoding/json"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"testing"

	"github.com/DDSRem-Dev/Shikigami/pkg/config"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
)

const a = "v0.0.1-alpha.1"
const b = "v0.0.1-alpha.2"

func TestPublicReleaseVersionBoundaries(t *testing.T) {
	l := newLab(t)
	account, err := user.Current()
	must(t, err)
	if account.Uid == "0" {
		t.Fatal("ordinary user required")
	}
	cert, key := l.certificate()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	must(t, err)
	address := listener.Addr().String()
	must(t, listener.Close())
	agentRoot := l.directory("agent")
	daemonRoot := l.directory("daemon")
	client := config.DefaultClient()
	client.Tailscale.Enabled = false
	client.Tailscale.StateDir = filepath.Join(agentRoot, "node")
	client.Agent.StateDir = filepath.Join(agentRoot, "state")
	client.Agent.Socket = filepath.Join(agentRoot, "run", "agent.sock")
	client.Agent.AutoStart = false
	client.PluginsDir = filepath.Join(agentRoot, "plugins")
	clientPath := filepath.Join(agentRoot, "config.yaml")
	must(t, config.SaveClient(clientPath, client))
	daemon := config.DefaultDaemon(account.Username, filepath.Join(daemonRoot, "state"))
	daemon.Tailscale.Enabled = false
	daemon.Tailscale.StateDir = filepath.Join(daemonRoot, "node")
	daemon.ProjectsDir = l.directory("projects")
	daemon.PluginsDir = filepath.Join(daemonRoot, "plugins")
	daemon.Server.DirectTLS = config.DirectTLS{Enabled: true, Address: address, CertFile: cert, KeyFile: key}
	daemonPath := filepath.Join(daemonRoot, "config.yaml")
	must(t, config.SaveDaemon(daemonPath, daemon))
	l.copyPlugins(b, daemon.PluginsDir)
	l.command(filepath.Join(l.bins[b], "shiki-daemon"), true, "--config", daemonPath, "init")
	token := filepath.Join(l.root, "private-token")
	l.command(filepath.Join(l.bins[b], "shiki-daemon"), true, "--config", daemonPath, "token", "create", "--expires", "1h", "--output", token)
	var projectIDs []string
	for index, phase := range []struct{ agent, daemon, cli string }{{a, b, b}, {b, a, a}, {a, b, b}} {
		l.record("phase", map[string]any{"index": index, "agent": phase.agent, "daemon": phase.daemon, "cli": phase.cli})
		l.copyPlugins(phase.agent, client.PluginsDir)
		l.copyPlugins(phase.daemon, daemon.PluginsDir)
		d := l.start(filepath.Join(l.bins[phase.daemon], "shiki-daemon"), "--config", daemonPath, "serve")
		ag := l.start(filepath.Join(l.bins[phase.agent], "shiki-agent"), "--config", clientPath)
		l.active["local"], l.active["remote"] = ag, d
		hello := l.hello(client, phase.agent)
		if hello.Status.State != "ready" {
			t.Fatal("agent not ready", hello.Status)
		}
		l.cli(phase.cli, clientPath, true, "agent", "status")
		if index == 0 {
			l.cli(phase.cli, clientPath, true, "connect", "remote", "--tls-address", address, "--tls-server-name", "localhost", "--tls-ca", cert, "--token-file", token)
		}
		// The published CLI itself traverses the real mixed agent/daemon chain.
		list := new(pb.ListProjectsResponse)
		decode(t, l.cli(phase.cli, clientPath, true, "--server", "remote", "projects", "list"), list)
		for _, id := range projectIDs {
			found := false
			for _, p := range list.Projects {
				found = found || p.Id == id
			}
			if !found {
				t.Fatal("persisted project missing after real version switch", id)
			}
		}
		servers := new(apb.ServerList)
		decode(t, l.cli(phase.cli, clientPath, true, "servers", "list"), servers)
		if len(servers.Servers) != 1 || servers.Servers[0].RemoteProtocol.GetBuildVersion() != phase.daemon || !servers.Servers[0].CredentialPresent {
			t.Fatal("actual remote negotiation/credential mismatch", servers)
		}
		l.record("actual_remote_agreement", servers.Servers[0])
		if index < 2 {
			project := new(pb.Project)
			name := "release-project-" + phase.daemon
			decode(t, l.cli(phase.cli, clientPath, true, "--server", "remote", "projects", "create", name, "--path", filepath.Join(daemon.ProjectsDir, name)), project)
			projectIDs = append(projectIDs, project.Id)
			l.record("project_created_through_public_cli", project)
			// Both sides use their own published plugin set as a positive control.
			for _, side := range []string{"local", "remote"} {
				for _, id := range []string{"sync", "port"} {
					l.plugin(phase.cli, clientPath, side, "enable", id, true, "ready")
				}
			}
			for _, side := range []string{"local", "remote"} {
				for _, id := range []string{"sync", "port"} {
					l.plugin(phase.cli, clientPath, side, "disable", id, true, "stopped")
				}
			}
			// Copy only unmodified, authenticated alternate manifests/binaries.
			l.copyPlugins(phase.daemon, client.PluginsDir)
			l.copyPlugins(phase.agent, daemon.PluginsDir)
			for _, side := range []string{"local", "remote"} {
				for _, id := range []string{"sync", "port"} {
					l.plugin(phase.cli, clientPath, side, "enable", id, false, "incompatible")
				}
			}
			l.cli(phase.cli, clientPath, true, "agent", "status")
			l.cli(phase.cli, clientPath, true, "--server", "remote", "projects", "get", project.Id)
			for _, side := range []string{"local", "remote"} {
				for _, id := range []string{"sync", "port"} {
					l.plugin(phase.cli, clientPath, side, "disable", id, true, "stopped")
				}
			}
		} else {
			for _, id := range projectIDs {
				l.cli(phase.cli, clientPath, true, "--server", "remote", "projects", "delete", id, "--yes")
			}
		}
		ag.stop()
		d.stop()
		l.record("disk_boundary", map[string]any{"phase": index, "config_schema": client.SchemaVersion, "agent_resources": schema(t, filepath.Join(client.Agent.StateDir, "resources", "state.json")), "daemon_state": schema(t, filepath.Join(daemon.StateDir, "state.json")), "claim": "same-schema real release reopen, not migration"})
	}
	for _, version := range []string{a, b} {
		l.futureState(version, client, daemon)
	}
}

func (l *lab) plugin(cli, cfg, side, operation, id string, success bool, want string) {
	args := []string{}
	if side == "remote" {
		args = append(args, "--server", "remote")
	}
	args = append(args, "plugins", "--side", side)
	l.cli(cli, cfg, success, append(append([]string{}, args...), operation, id, "--wait-timeout", "12s")...)
	status := new(pb.PluginStatus)
	decode(l.t, l.cli(cli, cfg, true, append(args, "status", id)...), status)
	if status.State != want {
		l.t.Fatal("unexpected actual plugin result", side, id, status)
	}
	if want == "ready" && (status.Protocol == nil || status.BuildVersion == "") {
		l.t.Fatal("ready without actual plugin agreement", status)
	}
	if want == "ready" {
		l.observeWorker(side, id)
	}
	if want == "incompatible" && status.Protocol != nil {
		l.t.Fatal("incompatible plugin advertised capabilities", status)
	}
	l.record("actual_plugin_boundary", map[string]any{"side": side, "status": status})
}

func schema(t *testing.T, path string) any {
	data, err := os.ReadFile(path)
	must(t, err)
	var value map[string]any
	must(t, json.Unmarshal(data, &value))
	return value["schema_version"]
}
func futureCopy(t *testing.T, source, target string) ([]byte, []byte) {
	data, err := os.ReadFile(source)
	must(t, err)
	var value map[string]any
	must(t, json.Unmarshal(data, &value))
	value["schema_version"] = 99
	future, err := json.MarshalIndent(value, "", "  ")
	must(t, err)
	must(t, os.MkdirAll(target, 0700))
	must(t, os.WriteFile(filepath.Join(target, "state.json"), future, 0600))
	must(t, os.WriteFile(filepath.Join(target, "state.previous.json"), data, 0600))
	return future, data
}
func (l *lab) futureState(version string, client *config.Client, daemon *config.Daemon) {
	root := l.directory("future-" + version)
	cfg := *client
	cfg.Revision = ""
	cfg.Agent.StateDir = filepath.Join(root, "agent-state")
	cfg.Agent.Socket = filepath.Join(root, "run", "agent.sock")
	cfg.Tailscale.StateDir = filepath.Join(root, "node")
	cfg.PluginsDir = filepath.Join(root, "plugins")
	target := filepath.Join(cfg.Agent.StateDir, "resources")
	future, previous := futureCopy(l.t, filepath.Join(client.Agent.StateDir, "resources", "state.json"), target)
	path := filepath.Join(root, "agent.yaml")
	must(l.t, config.SaveClient(path, &cfg))
	p := l.start(filepath.Join(l.bins[version], "shiki-agent"), "--config", path)
	hello := l.hello(&cfg, version)
	if hello.Status.State != "degraded" || hello.Status.ReasonCode != "AGENT_STATE_UNAVAILABLE" {
		l.t.Fatal("future agent state was not refused", hello.Status)
	}
	p.stop()
	l.unchanged(target, future, previous)
	dcfg := *daemon
	dcfg.Revision = ""
	dcfg.StateDir = filepath.Join(root, "daemon-state")
	dcfg.Tailscale.StateDir = filepath.Join(root, "daemon-node")
	dcfg.PluginsDir = cfg.PluginsDir
	future, previous = futureCopy(l.t, filepath.Join(daemon.StateDir, "state.json"), dcfg.StateDir)
	path = filepath.Join(root, "daemon.yaml")
	must(l.t, config.SaveDaemon(path, &dcfg))
	out := l.command(filepath.Join(l.bins[version], "shiki-daemon"), false, "--config", path, "serve")
	if !bytes.Contains(out, []byte("state schema is newer")) {
		l.t.Fatal("wrong future schema rejection", string(out))
	}
	l.unchanged(dcfg.StateDir, future, previous)
	l.record("future_schema_refused", map[string]any{"version": version, "agent": "degraded/AGENT_STATE_UNAVAILABLE", "daemon": "state schema is newer", "state_and_previous_unchanged": true, "synthetic_negative_only": true})
}
func (l *lab) unchanged(dir string, future, previous []byte) {
	for name, want := range map[string][]byte{"state.json": future, "state.previous.json": previous} {
		got, err := os.ReadFile(filepath.Join(dir, name))
		must(l.t, err)
		if !bytes.Equal(got, want) {
			l.t.Fatal("future state was rewritten", dir, name)
		}
	}
}
