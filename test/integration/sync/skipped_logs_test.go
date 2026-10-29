package sync_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	host "github.com/DDSRem-Dev/Shikigami/internal/plugin"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	"github.com/DDSRem-Dev/Shikigami/pkg/plugin/buildinfo"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	syncclient "github.com/DDSRem-Dev/Shikigami/plugins/sync/client"
	"github.com/DDSRem-Dev/Shikigami/plugins/sync/filetree"
	"github.com/DDSRem-Dev/Shikigami/test/fixtures/pluginhost"
	"golang.org/x/sys/unix"
	"google.golang.org/protobuf/types/known/structpb"
)

// Unlike a permissive CoreAPI stub, this bridge exercises the production log
// validator and ring after the worker's authenticated CoreAPI RPC arrives.
type skippedLogCore struct {
	pluginhost.Core
	supervisor *host.Supervisor
	attempts   chan error
}

func (c *skippedLogCore) Log(_ context.Context, record *pb.LogRecord) (*pb.Empty, error) {
	err := c.supervisor.RecordLog("sync", record)
	select {
	case c.attempts <- err:
	default:
	}
	return &pb.Empty{}, err
}

func TestProductionSyncSkippedFileLogs(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "shiki-skip-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	executable := filepath.Join(dir, "shiki-plugin-sync")
	build := exec.CommandContext(t.Context(), "go", "build", "-race", "-o", executable, "../../../plugins/sync")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build sync process: %v\n%s", err, output)
	}
	binary, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(binary)
	for _, side := range []string{"client", "server"} {
		for _, kind := range []string{"symlink", "fifo"} {
			t.Run(side+"/"+kind, func(t *testing.T) {
				testSkippedFileLog(t, filepath.Join(dir, side+"-"+kind), executable, hex.EncodeToString(digest[:]), side, kind)
			})
		}
	}
}

func testSkippedFileLog(t *testing.T, dir, executable, checksum, side, kind string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	pluginhost.Write(t, root, "ordinary.txt", "ordinary content")
	skipped := filepath.Join(root, "private-skipped-entry")
	if kind == "symlink" {
		outside := t.TempDir()
		pluginhost.Write(t, outside, "private-target", "must never be synchronized")
		err = os.Symlink(filepath.Join(outside, "private-target"), skipped)
	} else {
		err = unix.Mkfifo(skipped, 0600)
	}
	if err != nil {
		t.Fatal(err)
	}
	core := &skippedLogCore{attempts: make(chan error, 8)}
	mapping := &ppb.Mapping{Id: "map_test", ServerId: "srv_test", ProjectId: "proj_test", LocalRoot: root, Revision: "rev_skip", SyncEnabled: true}
	capability := sdk.FileCapability
	policy := host.Policy{Side: side, Capabilities: []string{"core.projects.read", "core.log"}, Scopes: []*ppb.Scope{{ProjectId: "proj_test"}}}
	core.Root = root
	var remote string
	if side == "client" {
		remote = t.TempDir()
		tree, err := filetree.Open(remote, filetree.Options{ProjectID: mapping.ProjectId, StateDir: filepath.Join(dir, "remote-state")})
		if err != nil {
			t.Fatal(err)
		}
		defer tree.Close()
		core.Tree = tree
		core.SetMapping(mapping)
		capability = sdk.SyncCapability
		policy = host.Policy{Side: side, Capabilities: []string{"core.mappings.read", "core.events.read", "core.events.write", "core.log", "file.read", "file.write", "file.watch"}, Scopes: []*ppb.Scope{scopeFor(mapping)}}
	}
	supervisor, err := host.NewSupervisor(ctx, host.SupervisorOptions{
		StateDir: filepath.Join(dir, "supervisor"),
		Validate: func(_ string, values *structpb.Struct) error { return syncclient.ValidateConfig(values) },
		Config: func(_ string, values *structpb.Struct) (host.Config, error) {
			return host.Config{
				Manifest: host.Manifest{Name: "sync", Version: buildinfo.Version, Executable: executable, SHA256: checksum, Side: side, Protocol: &pb.ProtocolOffer{Major: 1, Capabilities: []string{"core.scope.v1", capability}}},
				RunDir:   filepath.Join(dir, "run"), StateDir: filepath.Join(dir, "worker-state"), HostInstanceID: "host_skip",
				Values: values, Core: core, Policy: policy, StartupTimeout: 10 * time.Second, CredentialLifetime: time.Hour,
				Env: []string{"GORACE=halt_on_error=1 log_path=" + filepath.Join(dir, "race")},
			}, nil
		},
		Ready: func(ctx context.Context, _ string, worker sdk.Lifecycle) error {
			if side == "client" {
				_, err := worker.Sync().ConfigureMapping(ctx, mapping)
				return err
			}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	core.supervisor = supervisor
	defer func() {
		if err := supervisor.Close(); err != nil {
			t.Error(err)
		}
		reports, err := filepath.Glob(filepath.Join(dir, "race.*"))
		if err != nil || len(reports) != 0 {
			t.Errorf("child race reports: %v, glob error: %v", reports, err)
		}
	}()
	supervisor.Start()
	list, err := supervisor.List()
	if err != nil {
		t.Fatal(err)
	}
	var revision string
	for _, status := range list.Plugins {
		if status.PluginId == "sync" {
			revision = status.Revision
		}
	}
	if _, err := supervisor.Manage(ctx, &pb.ManagePluginRequest{PluginId: "sync", Action: pb.ManagePluginRequest_ENABLE, ExpectedRevision: revision, RequestId: "enable-skip"}); err != nil {
		t.Fatal(err)
	}
	if side == "client" {
		var conflicts *ppb.ConflictList
		pluginhost.WaitFor(t, func() bool {
			err := supervisor.Call(ctx, "sync", sdk.SyncCapability, func(ctx context.Context, worker sdk.Lifecycle) error {
				var err error
				conflicts, err = worker.Sync().ListConflicts(ctx, scopeFor(mapping))
				return err
			})
			return err == nil && conflicts != nil && len(conflicts.Conflicts) > 0
		})
		if len(conflicts.Conflicts) != 1 || conflicts.Conflicts[0].Path != "ordinary.txt" {
			t.Fatalf("client planned unsupported entry: %v", conflicts)
		}
		conflict := conflicts.Conflicts[0]
		if err := supervisor.Call(ctx, "sync", sdk.SyncCapability, func(ctx context.Context, worker sdk.Lifecycle) error {
			_, err := worker.Sync().ResolveConflict(ctx, &ppb.ResolveConflictRequest{MappingId: mapping.Id, ConflictId: conflict.Id, ExpectedGeneration: conflict.Generation, Choice: ppb.ResolveConflictRequest_KEEP_LOCAL, RequestId: "accept-ordinary-file"})
			return err
		}); err != nil {
			t.Fatal(err)
		}
		pluginhost.WaitFor(t, func() bool {
			data, err := os.ReadFile(filepath.Join(remote, "ordinary.txt"))
			return err == nil && string(data) == "ordinary content"
		})
		entries, err := os.ReadDir(remote)
		if err != nil || len(entries) != 1 || entries[0].Name() != "ordinary.txt" {
			t.Fatalf("client did not skip unsupported entry: %v, %v", entries, err)
		}
	} else {
		var files *pb.ListFilesResponse
		pluginhost.WaitFor(t, func() bool {
			err := supervisor.Call(ctx, "sync", sdk.FileCapability, func(ctx context.Context, worker sdk.Lifecycle) error {
				var err error
				files, err = worker.Files().ListFiles(ctx, &pb.ListFilesRequest{ProjectId: "proj_test"})
				return err
			})
			return err == nil && files != nil
		})
		if len(files.Files) != 1 || files.Files[0].Name != "ordinary.txt" {
			t.Fatalf("server did not skip unsupported entry: %v", files)
		}
	}
	entry, err := os.Lstat(skipped)
	if err != nil || (kind == "symlink" && entry.Mode()&os.ModeSymlink == 0) || (kind == "fifo" && entry.Mode()&os.ModeNamedPipe == 0) {
		t.Fatalf("source unsupported entry was modified: %v, %v", entry, err)
	}
	select {
	case err := <-core.attempts:
		if err != nil {
			t.Errorf("worker skipped the file but production host rejected its warning: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("scan completed without a CoreAPI log attempt", ctx.Err())
	}
	var warnings int
	if err := supervisor.WatchLogs(ctx, &pb.PluginLogsRequest{PluginId: "sync", Tail: 1000}, func(record *pb.LogRecord) error {
		if record.Message == "SYMLINK_OR_SPECIAL_FILE_SKIPPED" {
			warnings++
			if record.Level != "warn" || len(record.Fields) != 0 || record.Timestamp == 0 {
				t.Errorf("unsafe or incomplete skip warning: %v", record)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if warnings != 1 {
		t.Errorf("actual host log ring contains %d skip warnings, want 1", warnings)
	}
	if err := supervisor.RecordLog("sync", &pb.LogRecord{Level: "warn", Message: "SYMLINK_OR_SPECIAL_FILE_SKIPPED", Fields: map[string]string{"path": skipped}}); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("production log whitelist accepted arbitrary metadata: %v", err)
	}
}
