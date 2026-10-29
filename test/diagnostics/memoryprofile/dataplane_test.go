//go:build memoryprofile

package memoryprofile_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	frontend "github.com/DDSRem-Dev/Shikigami/internal/client"
	"github.com/DDSRem-Dev/Shikigami/pkg/config"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	"github.com/shirou/gopsutil/v4/process"
	"google.golang.org/protobuf/types/known/structpb"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime/debug"
	"testing"
	"time"
)

type dataFixture struct {
	root          string
	ctx           context.Context
	agent, daemon *child
	client        *frontend.Client
	serverID      string
	project       *pb.Project
}

func startDataFixture(t *testing.T) *dataFixture {
	return startDataFixtureFor(t, 3*time.Minute)
}

func startDataFixtureFor(t *testing.T, duration time.Duration) *dataFixture {
	if *outputDirectory == "" {
		t.Fatal("explicit -memoryprofile-output directory is required")
	}
	if os.Geteuid() == 0 {
		t.Fatal("diagnostic daemon requires an ordinary user")
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Fatal(err)
	}
	root, err := filepath.Abs(*outputDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	// Nested private go-plugin sockets have a platform path-length limit. Keep
	// the diagnostic's evidence root short instead of changing host paths.
	if len(root) > 45 {
		t.Fatal("diagnostic runtime root must be at most 45 bytes; use a short /tmp evidence directory", root)
	}
	t.Cleanup(func() {
		if err := recordReadyIdentities(root); err != nil {
			t.Error("process inventory", err)
		}
	})
	ctx, cancel := context.WithTimeout(t.Context(), duration)
	t.Cleanup(cancel)
	stopSignal := context.AfterFunc(diagnosticContext, cancel)
	t.Cleanup(func() { stopSignal() })
	// Workload cancellation must still allow both hosts to close their workers.
	// The independent process lifetime is canceled only after child cleanups.
	processContext, stopProcesses := context.WithTimeout(context.Background(), duration+30*time.Second)
	t.Cleanup(stopProcesses)
	workerManifests(t, root)
	account, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	certificate, key := certificateFiles(t, root)
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	dc := config.DefaultDaemon(account.Username, filepath.Join(root, "daemon", "state"))
	dc.ProjectsDir = filepath.Join(root, "projects")
	dc.PluginsDir = filepath.Join(root, "plugins")
	dc.Tailscale.Enabled = false
	dc.Claude.Executable = "/bin/sh"
	dc.Server.DirectTLS = config.DirectTLS{Enabled: true, Address: address, CertFile: certificate, KeyFile: key}
	if err := config.SaveDaemon(filepath.Join(root, "daemon.yaml"), dc); err != nil {
		t.Fatal(err)
	}
	ac := config.DefaultClient()
	ac.Tailscale.Enabled = false
	ac.Tailscale.StateDir = filepath.Join(root, "agent", "node")
	ac.Agent.StateDir = filepath.Join(root, "agent", "state")
	ac.Agent.Socket = filepath.Join(root, "agent.sock")
	ac.PluginsDir = filepath.Join(root, "plugins")
	clientConfig := filepath.Join(root, "client.yaml")
	if err := config.SaveClient(clientConfig, ac); err != nil {
		t.Fatal(err)
	}
	daemon := startChild(t, processContext, root, "daemon")
	agent := startChild(t, processContext, root, "agent")
	var info ready
	data, err := os.ReadFile(filepath.Join(root, "daemon.ready.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &info); err != nil {
		t.Fatal(err)
	}
	var client *frontend.Client
	wait, stop := context.WithTimeout(ctx, 10*time.Second)
	err = await(wait, func() error {
		var err error
		client, err = frontend.Open(wait, clientConfig, frontend.Options{Timeout: time.Second})
		return err
	})
	stop()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	if err := await(ctx, func() error {
		status, err := client.Agent().GetStatus(ctx, &pb.Empty{})
		if err != nil {
			return err
		}
		if status.State != "ready" {
			return errors.New("agent resources not ready")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	record, err := client.Agent().UpsertServer(ctx, &apb.UpsertServerRequest{RequestId: "setup-server", Config: &apb.ServerConfig{Name: "memory", DirectTlsAddress: address, TlsServerName: "example.com", TlsCaFile: certificate}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Agent().SetServerCredential(ctx, &apb.SetServerCredentialRequest{RequestId: "setup-credential", ServerId: record.Config.Id, Token: []byte(info.Token)}); err != nil {
		t.Fatal(err)
	}
	remote := client.Remote(record.Config.Id)
	project, err := remote.CreateProject(ctx, &pb.CreateProjectRequest{Name: "fixed", Path: filepath.Join(dc.ProjectsDir, "fixed"), RequestId: "setup-project"})
	if err != nil {
		t.Fatal(err)
	}
	return &dataFixture{root, ctx, agent, daemon, client, record.Config.Id, project}
}

func (f *dataFixture) manage(t *testing.T, name string, side apb.PluginTarget_Side, action pb.ManagePluginRequest_Action, values *structpb.Struct) {
	t.Helper()
	ctx, cancel := context.WithTimeout(f.ctx, 15*time.Second)
	defer cancel()
	target := &apb.PluginTarget{Side: side}
	if side == apb.PluginTarget_REMOTE {
		target.ServerId = f.serverID
	}
	list, err := f.client.Agent().ListPlugins(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	revision := ""
	for _, status := range list.Plugins {
		if status.PluginId == name {
			revision = status.Revision
		}
	}
	if revision == "" {
		t.Fatal("missing plugin revision", name)
	}
	accepted, err := f.client.Agent().ManagePlugin(ctx, &apb.ManagePluginRequest{Target: target, Request: &pb.ManagePluginRequest{PluginId: name, Action: action, Config: values, ExpectedRevision: revision, RequestId: fmt.Sprintf("%s-%d-%d", name, side, time.Now().UnixNano())}})
	if err != nil {
		t.Fatal(err)
	}
	if err := await(ctx, func() error {
		list, err := f.client.Agent().ListPlugins(ctx, target)
		if err != nil {
			return err
		}
		for _, s := range list.Plugins {
			if s.PluginId != name || s.Revision != accepted.Revision {
				continue
			}
			if s.State == "failed" || s.State == "incompatible" {
				t.Fatalf("plugin %s: %s (%s)", name, s.State, s.ReasonCode)
			}
			if s.State == "ready" || (!s.Enabled && s.State == "stopped") {
				return nil
			}
		}
		return errors.New("plugin not ready")
	}); err != nil {
		t.Fatal(err)
	}
	if err := recordReadyIdentities(f.root); err != nil {
		t.Fatal(err)
	}
}

func (f *dataFixture) samples(t *testing.T, name, label string) map[string]int {
	t.Helper()
	f.agent.sample(t, f.ctx, f.root, label)
	f.daemon.sample(t, f.ctx, f.root, label)
	result := map[string]int{}
	for _, role := range []string{name + "-client", name + "-server"} {
		result[role] = sampleWorker(t, f.ctx, f.root, role, label)
	}
	return result
}

func (f *dataFixture) idle(t *testing.T, duration time.Duration) {
	t.Helper()
	select {
	case <-f.ctx.Done():
		t.Fatal(f.ctx.Err())
	case <-time.After(duration):
	}
}

func (f *dataFixture) stopWorkers(t *testing.T, name string, pids map[string]int) {
	t.Helper()
	f.manage(t, name, apb.PluginTarget_LOCAL, pb.ManagePluginRequest_DISABLE, nil)
	f.manage(t, name, apb.PluginTarget_REMOTE, pb.ManagePluginRequest_DISABLE, nil)
	ctx, cancel := context.WithTimeout(f.ctx, 10*time.Second)
	defer cancel()
	for role, pid := range pids {
		if err := await(ctx, func() error {
			live, err := process.PidExistsWithContext(ctx, int32(pid))
			if err != nil {
				return err
			}
			if live {
				return fmt.Errorf("worker %s %d still exists", role, pid)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestWorkerMemoryProfile(t *testing.T) {
	f := startDataFixture(t)
	f.manage(t, "sync", apb.PluginTarget_REMOTE, pb.ManagePluginRequest_ENABLE, nil)
	f.manage(t, "sync", apb.PluginTarget_LOCAL, pb.ManagePluginRequest_ENABLE, nil)
	local := filepath.Join(f.root, "local-files")
	if err := os.Mkdir(local, 0700); err != nil {
		t.Fatal(err)
	}
	initial := bytes.Repeat([]byte("baseline"), 32<<10)
	for _, root := range []string{local, f.project.Path} {
		if err := os.WriteFile(filepath.Join(root, "fixed.bin"), initial, 0600); err != nil {
			t.Fatal(err)
		}
	}
	mapping, err := f.client.Agent().PutMapping(f.ctx, &apb.PutMappingRequest{RequestId: "fixed-mapping", Mapping: &ppb.Mapping{ServerId: f.serverID, ProjectId: f.project.Id, LocalRoot: local, SyncEnabled: true}})
	if err != nil {
		t.Fatal(err)
	}
	scope := &ppb.Scope{ServerId: f.serverID, ProjectId: f.project.Id, MappingId: mapping.Id}
	settle := func(root string, want [32]byte) {
		ctx, cancel := context.WithTimeout(f.ctx, 10*time.Second)
		defer cancel()
		if err := await(ctx, func() error {
			data, err := os.ReadFile(filepath.Join(root, "fixed.bin"))
			if err != nil {
				return err
			}
			if len(data) != 256<<10 || sha256.Sum256(data) != want {
				return errors.New("file checksum/length not settled")
			}
			state, err := f.client.Agent().GetSyncState(ctx, scope)
			if err != nil {
				return err
			}
			if state.State != "active" || state.PendingFiles != 0 || state.ConflictCount != 0 {
				return fmt.Errorf("sync %s pending=%d conflicts=%d", state.State, state.PendingFiles, state.ConflictCount)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	settle(local, sha256.Sum256(initial))
	pids := f.samples(t, "sync", "file-baseline")
	started := time.Now()
	for i := 0; i < 16; i++ {
		payload := bytes.Repeat([]byte{byte(i), 0xff, 0, byte(i + 1)}, 64<<10)
		source, destination := local, f.project.Path
		if i%2 != 0 {
			source, destination = destination, source
		}
		if err := os.WriteFile(filepath.Join(source, "fixed.bin"), payload, 0600); err != nil {
			t.Fatal(err)
		}
		settle(destination, sha256.Sum256(payload))
	}
	fileElapsed := time.Since(started)
	f.samples(t, "sync", "file-loaded")
	f.idle(t, fileElapsed)
	f.samples(t, "sync", "file-idle")
	mapping.SyncEnabled = false
	mapping, err = f.client.Agent().PutMapping(f.ctx, &apb.PutMappingRequest{RequestId: "pause-mapping", ExpectedRevision: mapping.Revision, Mapping: mapping})
	if err != nil {
		t.Fatal(err)
	}
	pauseCtx, pauseCancel := context.WithTimeout(f.ctx, 10*time.Second)
	defer pauseCancel()
	if err := await(pauseCtx, func() error {
		state, err := f.client.Agent().GetSyncState(pauseCtx, scope)
		if err != nil {
			return err
		}
		if state.State != "paused" {
			return errors.New("mapping not paused")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.client.Agent().RemoveMapping(f.ctx, &apb.RemoveMappingRequest{MappingId: mapping.Id, ExpectedRevision: mapping.Revision, RequestId: "remove-mapping"}); err != nil {
		t.Fatal(err)
	}
	f.stopWorkers(t, "sync", pids)
	t.Logf("file stage: 16 x 256KiB alternating direction, elapsed=%s; workers exited", fileElapsed)
	tcpElapsed, accepted := runTCPStage(t, f)
	f.client.Close()
	f.agent.close(t)
	f.daemon.close(t)
	files, err := filepath.Glob(filepath.Join(f.root, "race.*"))
	if err != nil || len(files) > 0 {
		t.Fatal("child race reports", files, err)
	}
	build, _ := debug.ReadBuildInfo()
	if err := saveJSON(filepath.Join(f.root, "experiment.json"), map[string]any{"build": build, "file_updates": 16, "file_payload_bytes": 256 << 10, "file_elapsed_ns": fileElapsed.Nanoseconds(), "file_idle_ns": fileElapsed.Nanoseconds(), "tcp_connections": 32, "tcp_payload_bytes": 1 << 20, "tcp_elapsed_ns": tcpElapsed.Nanoseconds(), "tcp_idle_ns": tcpElapsed.Nanoseconds(), "tcp_target_accepted": accepted, "all_workers_exited": true, "children_exited": true, "forced_gc_per_boundary": 2, "fixture_entrypoints": true}); err != nil {
		t.Fatal(err)
	}
	t.Logf("private artifacts: %s; all six child processes exited", f.root)
}
