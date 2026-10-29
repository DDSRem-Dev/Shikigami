package sync_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	host "github.com/DDSRem-Dev/Shikigami/internal/plugin"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	"github.com/DDSRem-Dev/Shikigami/pkg/plugin/buildinfo"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	syncclient "github.com/DDSRem-Dev/Shikigami/plugins/sync/client"
	"github.com/DDSRem-Dev/Shikigami/plugins/sync/filetree"
	"github.com/DDSRem-Dev/Shikigami/test/fixtures/pluginhost"
	"google.golang.org/protobuf/types/known/structpb"
)

// This uses the production binary, native gRPC SDK adapter and authenticated
// CoreAPI socket. A passing in-process worker alone would not verify this hop.
func TestProductionSyncProcessThroughSupervisor(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	dir, err := os.MkdirTemp("/tmp", "shiki-sync-process-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	executable := filepath.Join(dir, "shiki-plugin-sync")
	build := exec.CommandContext(ctx, "go", "build", "-o", executable, "../../../plugins/sync")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build sync process: %v\n%s", err, output)
	}
	binary, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(binary)
	local, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	remote := t.TempDir()
	pluginhost.Write(t, local, "process.txt", "from-client-process")
	pluginhost.Write(t, remote, "process.txt", "from-remote")
	tree, err := filetree.Open(remote, filetree.Options{ProjectID: "proj_test", StateDir: filepath.Join(dir, "remote-state")})
	if err != nil {
		t.Fatal(err)
	}
	defer tree.Close()
	mapping := &ppb.Mapping{Id: "map_test", ServerId: "srv_test", ProjectId: "proj_test", LocalRoot: local, Revision: "rev_process", SyncEnabled: true}
	probe := &pluginhost.Core{Tree: tree}
	probe.SetMapping(mapping)
	supervisor, err := host.NewSupervisor(ctx, host.SupervisorOptions{StateDir: filepath.Join(dir, "supervisor"), Validate: func(_ string, cfg *structpb.Struct) error { return syncclient.ValidateConfig(cfg) }, Config: func(_ string, values *structpb.Struct) (host.Config, error) {
		return host.Config{Manifest: host.Manifest{Name: "sync", Version: buildinfo.Version, Executable: executable, SHA256: hex.EncodeToString(sum[:]), Side: "client", Protocol: &pb.ProtocolOffer{Major: 1, Capabilities: []string{"core.scope.v1", sdk.SyncCapability}}}, RunDir: filepath.Join(dir, "run"), StateDir: filepath.Join(dir, "client-state"), HostInstanceID: "host_process", Values: values, Core: probe, Policy: host.Policy{Side: "client", Capabilities: []string{"core.mappings.read", "core.events.write", "core.events.read", "core.log", "file.read", "file.write", "file.watch"}, Scopes: []*ppb.Scope{scopeFor(mapping)}}, StartupTimeout: 10 * time.Second, CredentialLifetime: time.Hour}, nil
	}, Ready: func(ctx context.Context, _ string, worker sdk.Lifecycle) error {
		_, err := worker.Sync().ConfigureMapping(ctx, mapping)
		return err
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer supervisor.Close()
	supervisor.Start()
	list, err := supervisor.List()
	if err != nil {
		t.Fatal(err)
	}
	revision := ""
	for _, status := range list.Plugins {
		if status.PluginId == "sync" {
			revision = status.Revision
		}
	}
	if _, err = supervisor.Manage(ctx, &pb.ManagePluginRequest{PluginId: "sync", Action: pb.ManagePluginRequest_ENABLE, ExpectedRevision: revision, RequestId: "enable"}); err != nil {
		t.Fatal(err)
	}
	var conflict *ppb.Conflict
	pluginhost.WaitFor(t, func() bool {
		err := supervisor.Call(ctx, "sync", sdk.SyncCapability, func(ctx context.Context, worker sdk.Lifecycle) error {
			conflicts, err := worker.Sync().ListConflicts(ctx, scopeFor(mapping))
			if err == nil && len(conflicts.Conflicts) == 1 {
				conflict = conflicts.Conflicts[0]
			}
			return err
		})
		return err == nil && conflict != nil
	})
	if conflict.Base != nil || pluginhost.Read(t, remote, "process.txt") != "from-remote" {
		t.Fatal("subprocess bypassed first-sync plan")
	}
	err = supervisor.Call(ctx, "sync", sdk.SyncCapability, func(ctx context.Context, worker sdk.Lifecycle) error {
		stream, err := worker.Sync().ReadConflictFile(ctx, &ppb.ReadConflictFileRequest{MappingId: mapping.Id, ConflictId: conflict.Id, ExpectedGeneration: conflict.Generation, Side: ppb.ReadConflictFileRequest_LOCAL})
		if err != nil {
			return err
		}
		defer stream.Close()
		first, err := stream.Receive()
		if err != nil {
			return err
		}
		if first.GetInfo() == nil {
			t.Fatal("native conflict stream did not start with metadata")
		}
		var data []byte
		for {
			frame, err := stream.Receive()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return err
			}
			data = append(data, frame.GetChunk()...)
		}
		if string(data) != "from-client-process" {
			t.Fatal("native conflict stream corrupted content")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	err = supervisor.Call(ctx, "sync", sdk.SyncCapability, func(ctx context.Context, worker sdk.Lifecycle) error {
		_, err := worker.Sync().ResolveConflict(ctx, &ppb.ResolveConflictRequest{MappingId: mapping.Id, ConflictId: conflict.Id, ExpectedGeneration: conflict.Generation, Choice: ppb.ResolveConflictRequest_KEEP_LOCAL, RequestId: "explicit-choice"})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if pluginhost.Read(t, remote, "process.txt") != "from-client-process" {
		t.Fatal("native SyncWorker resolution did not commit through CoreAPI")
	}
	if err = supervisor.Close(); err != nil {
		t.Fatal(err)
	}
	pluginhost.WaitFor(t, func() bool { return probe.Watches.Load() == 0 })
	t.Run("server-side-native-files", func(t *testing.T) { testServerProcess(t, ctx, dir, executable, hex.EncodeToString(sum[:])) })
}

func testServerProcess(t *testing.T, ctx context.Context, dir, executable, checksum string) {
	root, _ := filepath.EvalSymlinks(t.TempDir())
	pluginhost.Write(t, root, "server.txt", "server-side-content")
	core := &pluginhost.Core{Root: root}
	supervisor, err := host.NewSupervisor(ctx, host.SupervisorOptions{StateDir: filepath.Join(dir, "server-supervisor"), Validate: func(string, *structpb.Struct) error { return nil }, Config: func(_ string, values *structpb.Struct) (host.Config, error) {
		return host.Config{Manifest: host.Manifest{Name: "sync", Version: buildinfo.Version, Executable: executable, SHA256: checksum, Side: "server", Protocol: &pb.ProtocolOffer{Major: 1, Capabilities: []string{"core.scope.v1", sdk.FileCapability}}}, RunDir: filepath.Join(dir, "server-run"), StateDir: filepath.Join(dir, "server-state"), HostInstanceID: "host_server", Core: core, Values: values, Policy: host.Policy{Side: "server", Capabilities: []string{"core.projects.read"}, Scopes: []*ppb.Scope{{ProjectId: "proj_test"}}}, StartupTimeout: 10 * time.Second, CredentialLifetime: time.Hour}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer supervisor.Close()
	supervisor.Start()
	list, err := supervisor.List()
	if err != nil {
		t.Fatal(err)
	}
	revision := ""
	for _, status := range list.Plugins {
		if status.PluginId == "sync" {
			revision = status.Revision
		}
	}
	if _, err = supervisor.Manage(ctx, &pb.ManagePluginRequest{PluginId: "sync", Action: pb.ManagePluginRequest_ENABLE, ExpectedRevision: revision, RequestId: "enable-server"}); err != nil {
		t.Fatal(err)
	}
	var info *pb.FileInfo
	pluginhost.WaitFor(t, func() bool {
		err := supervisor.Call(ctx, "sync", sdk.FileCapability, func(ctx context.Context, worker sdk.Lifecycle) error {
			var err error
			info, err = worker.Files().StatFile(ctx, &pb.StatFileRequest{ProjectId: "proj_test", Path: "server.txt"})
			return err
		})
		return err == nil && info != nil
	})
	err = supervisor.Call(ctx, "sync", sdk.FileCapability, func(ctx context.Context, worker sdk.Lifecycle) error {
		stream, err := worker.Files().ReadFile(ctx, &pb.ReadFileRequest{ProjectId: "proj_test", Path: "server.txt", ExpectedRevision: info.Revision})
		if err != nil {
			return err
		}
		defer stream.Close()
		first, err := stream.Receive()
		if err != nil {
			return err
		}
		if first.GetInfo() == nil {
			t.Fatal("server file stream omitted metadata")
		}
		var data []byte
		for {
			frame, err := stream.Receive()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return err
			}
			data = append(data, frame.GetChunk()...)
		}
		if string(data) != "server-side-content" {
			t.Fatal("server process returned wrong content")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func scopeFor(m *ppb.Mapping) *ppb.Scope {
	return &ppb.Scope{ServerId: m.ServerId, ProjectId: m.ProjectId, MappingId: m.Id}
}
