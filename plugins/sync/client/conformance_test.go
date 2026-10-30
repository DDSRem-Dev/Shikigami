package client

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	schema "github.com/DDSRem-Dev/Shikigami/pkg/plugin/config"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	"github.com/DDSRem-Dev/Shikigami/plugins/sync/filetree"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

func configuredWorker(t *testing.T, values map[string]any, remoteLimit int64) (*Worker, *coreProbe, *ppb.Mapping, string) {
	t.Helper()
	ctx := context.Background()
	local, _ := filepath.EvalSymlinks(t.TempDir())
	remote := t.TempDir()
	tree, err := filetree.Open(remote, filetree.Options{ProjectID: "proj_test", StateDir: filepath.Join(t.TempDir(), "private"), MaxFileBytes: remoteLimit})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { tree.Close() })
	probe := &coreProbe{Tree: tree}
	cfg, err := structpb.NewStruct(values)
	if err != nil {
		t.Fatal(err)
	}
	w := New()
	if err = w.Init(ctx, sdk.InitContext{Core: coreClient(t, ctx, probe), Side: "client", StateDir: filepath.Join(t.TempDir(), "state"), Config: cfg}); err != nil {
		t.Fatal(err)
	}
	if err = w.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w.Stop(context.Background()) })
	m := &ppb.Mapping{Id: "map_test", ServerId: "srv_test", ProjectId: "proj_test", LocalRoot: local}
	configure(t, w, probe, m, true)
	return w, probe, m, remote
}

func TestRepeatedMappingRevisionPreservesRunningWatchers(t *testing.T) {
	w, probe, m, _ := configuredWorker(t, map[string]any{"scan_interval_ms": 60000}, 0)
	waitFor(t, func() bool { return probe.Watches.Load() == 1 })
	task, _ := w.get(m.Id)
	task.runMu.Lock()
	before := task.run
	task.runMu.Unlock()
	// A long data operation must not let duplicate configuration hold up the
	// configuration queue, which a later pause needs in order to cancel it.
	task.op.Lock()
	repeated := make(chan error, 1)
	go func() {
		_, err := w.ConfigureMapping(context.Background(), proto.Clone(m).(*ppb.Mapping))
		repeated <- err
	}()
	select {
	case err := <-repeated:
		if err != nil {
			task.op.Unlock()
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		task.op.Unlock()
		t.Fatal("duplicate configuration waited for data operation")
	}
	task.op.Unlock()
	task.runMu.Lock()
	after := task.run
	task.runMu.Unlock()
	if before != after || before.Err() != nil {
		t.Fatal("same revision canceled or replaced active watchers")
	}
	changed := proto.Clone(m).(*ppb.Mapping)
	changed.SyncEnabled = false
	probe.SetMapping(changed)
	if _, err := w.ConfigureMapping(context.Background(), changed); connect.CodeOf(err) != connect.CodeAborted {
		t.Fatalf("reused revision accepted changed content: %v", err)
	}
	if before.Err() != nil {
		t.Fatal("invalid revision canceled active watchers")
	}
}

func TestConflictPreviewRechecksOppositeSideAtEnd(t *testing.T) {
	w, _, m, remote := configuredWorker(t, map[string]any{"scan_interval_ms": 60000}, 0)
	write(t, m.LocalRoot, "conflict.txt", "local")
	write(t, remote, "conflict.txt", "remote")
	pass(t, w, m.Id)
	c := conflicts(t, w, m)["conflict.txt"]
	changed := false
	err := w.ReadConflictFile(context.Background(), &ppb.ReadConflictFileRequest{MappingId: m.Id, ConflictId: c.Id, ExpectedGeneration: c.Generation, Side: ppb.ReadConflictFileRequest_LOCAL}, func(frame *pb.FileData) error {
		if _, ok := frame.Payload.(*pb.FileData_Chunk); ok && !changed {
			changed = true
			return os.WriteFile(filepath.Join(remote, "conflict.txt"), []byte("changed during preview"), 0644)
		}
		return nil
	})
	if connect.CodeOf(err) != connect.CodeAborted || !changed {
		t.Fatalf("preview accepted changed opposite version: %v", err)
	}
	if conflicts(t, w, m)["conflict.txt"].Generation == c.Generation {
		t.Fatal("new generation not persisted")
	}
}

func TestConfiguredSizeLimitControlsObservationAndTransfer(t *testing.T) {
	for _, size := range []int64{16, schema.DefaultMaxAutoSyncSize + 1} {
		t.Run(strconv.FormatInt(size, 10), func(t *testing.T) {
			w, _, m, remote := configuredWorker(t, map[string]any{"scan_interval_ms": 60000, "max_auto_sync_size": size}, size)
			content := strings.Repeat("x", int(size))
			write(t, m.LocalRoot, "allowed.bin", content)
			write(t, m.LocalRoot, "too-large.bin", content+"x")
			pass(t, w, m.Id)
			plan := conflicts(t, w, m)
			c := plan["allowed.bin"]
			if c == nil || plan["too-large.bin"] != nil {
				t.Fatal("configured size not applied to observations")
			}
			_, err := w.ResolveConflict(context.Background(), &ppb.ResolveConflictRequest{MappingId: m.Id, ConflictId: c.Id, ExpectedGeneration: c.Generation, Choice: ppb.ResolveConflictRequest_KEEP_LOCAL, RequestId: "size-limit"})
			if err != nil {
				t.Fatal(err)
			}
			if contents(t, remote, "allowed.bin") != content {
				t.Fatal("configured-size transfer mismatch")
			}
			if _, err = os.Stat(filepath.Join(remote, "too-large.bin")); !os.IsNotExist(err) {
				t.Fatal("oversized file was transferred")
			}
		})
	}
}

func TestAuthoritativeRemovalStopsTaskAndArchivesWithoutDeletingFiles(t *testing.T) {
	w, probe, m, remote := configuredWorker(t, map[string]any{"scan_interval_ms": 60000}, 0)
	write(t, m.LocalRoot, "retained.txt", "local evidence")
	write(t, remote, "retained.txt", "remote evidence")
	pass(t, w, m.Id)
	waitFor(t, func() bool { return probe.Watches.Load() == 1 })
	probe.SetMapping(nil)
	waitFor(t, func() bool {
		_, err := w.GetSyncState(context.Background(), scopeFor(m))
		return connect.CodeOf(err) == connect.CodeNotFound
	})
	// Local removal cancels the stream; the remote handler observes cancellation asynchronously.
	waitFor(t, func() bool { return probe.Watches.Load() == 0 })
	snapshot, err := w.store.View()
	if err != nil {
		t.Fatal(err)
	}
	var archived record
	if err = snapshot.Get(archivedMappingsBucket, m.Id, &archived); err != nil {
		t.Fatal(err)
	}
	if archived.State != "archived" || len(archived.Conflicts) != 1 {
		t.Fatal("removal lost unresolved conflict evidence")
	}
	if _, ok := snapshot.Buckets[mappingsBucket][m.Id]; ok {
		t.Fatal("removed mapping retained active ledger")
	}
	if contents(t, m.LocalRoot, "retained.txt") != "local evidence" || contents(t, remote, "retained.txt") != "remote evidence" {
		t.Fatal("mapping removal changed project files")
	}
}

func TestAuthFailureWaitsForHostCredentialEvent(t *testing.T) {
	w, probe, m, remote := configuredWorker(t, map[string]any{"scan_interval_ms": 100}, 0)
	write(t, m.LocalRoot, "safe.txt", "original")
	write(t, remote, "safe.txt", "original")
	pass(t, w, m.Id)
	probe.Denied.Store(true)
	write(t, m.LocalRoot, "safe.txt", "updated")
	w.notify()
	task, _ := w.get(m.Id)
	waitFor(t, func() bool { task.op.Lock(); defer task.op.Unlock(); return task.retryBlocked })
	calls := probe.ListCalls.Load()
	for i := 0; i < 5; i++ {
		w.notify()
		time.Sleep(50 * time.Millisecond)
	}
	if probe.ListCalls.Load() != calls {
		t.Fatal("authentication failure was automatically retried")
	}
	if contents(t, remote, "safe.txt") != "original" {
		t.Fatal("failed authentication changed remote file")
	}
	probe.Denied.Store(false)
	if err := w.refreshMappings(context.Background(), &ppb.Event{Type: "server", Scope: &ppb.Scope{ServerId: m.ServerId}}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return contents(t, remote, "safe.txt") == "updated" })
}
