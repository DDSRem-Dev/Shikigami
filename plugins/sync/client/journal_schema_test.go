package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"connectrpc.com/connect/v2"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	storage "github.com/DDSRem-Dev/Shikigami/pkg/plugin/state"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	"google.golang.org/protobuf/proto"
)

func TestSyncLedgerMigrationPreservesActiveArchiveAndReceipts(t *testing.T) {
	f := newJournalFixture(t)
	r := f.saved(t)
	r.Copies["copy.txt"] = "file.txt"
	r.Conflicts["file.txt"] = &ppb.Conflict{Id: "conf_legacy", MappingId: r.Mapping.Id, Path: "file.txt", Base: r.Baselines["file.txt"], Local: r.Baselines["file.txt"], Remote: r.Baselines["file.txt"]}
	// Use the same generation validation as all persisted conflicts.
	f.task.data = r
	if err := f.task.conflict("file.txt", r.Baselines["file.txt"], r.Baselines["file.txt"]); err != nil {
		t.Fatal(err)
	}
	r = f.task.data
	root := filepath.Join(t.TempDir(), "state")
	if err := storage.EnsurePrivateDir(root); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "ledger")
	old, err := storage.Open(dir, storage.Options{})
	if err != nil {
		t.Fatal(err)
	}
	err = old.Update(func(s *storage.Snapshot) error {
		for _, bucket := range []string{mappingsBucket, archivedMappingsBucket} {
			data, _ := json.Marshal(r)
			var fields map[string]json.RawMessage
			json.Unmarshal(data, &fields)
			delete(fields, "operations")
			delete(fields, "legacy_unverified")
			if err := s.Put(bucket, r.Mapping.Id, fields); err != nil {
				return err
			}
		}
		if _, _, err := s.Request("resolve/map_test", "old-request", map[string]string{"choice": "keep_local"}); err != nil {
			return err
		}
		return s.CompleteRequest("resolve/map_test", "old-request", "conf_legacy", map[string]string{"state": "active"}, "")
	})
	if err != nil {
		t.Fatal(err)
	}
	old.Close()
	before, err := os.ReadFile(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var original storage.Snapshot
	json.Unmarshal(before, &original)
	worker := New()
	if err := worker.Init(t.Context(), sdk.InitContext{Core: journalCore{}, Side: "client", StateDir: root, InstanceID: "plugin_migration", HostInstanceID: "host_migration"}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { worker.Stop(context.Background()) })
	health, err := worker.Health(t.Context())
	if err != nil || health.Status != pb.HealthStatus_HEALTHY {
		t.Fatal("initialized pre-Start worker was incorrectly failed", err)
	}
	migrated, err := worker.store.View()
	if err != nil {
		t.Fatal(err)
	}
	if migrated.SchemaVersion != 2 || len(migrated.Requests) != len(original.Requests) {
		t.Fatal("migration lost schema or receipts")
	}
	for key, want := range original.Requests {
		got := migrated.Requests[key]
		a, _ := json.Marshal(got)
		b, _ := json.Marshal(want)
		if !bytes.Equal(a, b) {
			t.Fatal("completed receipt changed")
		}
	}
	for _, bucket := range []string{mappingsBucket, archivedMappingsBucket} {
		var got record
		if err := migrated.Get(bucket, r.Mapping.Id, &got); err != nil {
			t.Fatal(err)
		}
		if got.Operations == nil || len(got.Operations) != 0 || !got.LegacyUnverified["file.txt"] || !got.LegacyUnverified["copy.txt"] || !proto.Equal(got.Mapping, r.Mapping) || !proto.Equal(got.Conflicts["file.txt"], r.Conflicts["file.txt"]) || got.Copies["copy.txt"] != "file.txt" || !proto.Equal(got.Baselines["file.txt"], r.Baselines["file.txt"]) {
			t.Fatal("migration lost mapping history")
		}
	}
	backups, err := filepath.Glob(filepath.Join(dir, "state.json.pre-migration-v1-*.json"))
	if err != nil || len(backups) != 1 {
		t.Fatal("migration rollback evidence missing", err)
	}
	backup, _ := os.ReadFile(backups[0])
	if !bytes.Equal(before, backup) {
		t.Fatal("original migration bytes not preserved")
	}
	worker.Stop(context.Background())
	if _, err := storage.Open(dir, storage.Options{}); !errors.Is(err, storage.ErrFutureSchema) {
		t.Fatal("old writer opened migrated sync ledger", err)
	}
}

func TestSyncInitRejectsFutureJournalWithoutChangingBytes(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	if err := storage.EnsurePrivateDir(root); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "ledger")
	future, err := storage.Open(dir, storage.Options{SchemaVersion: 3})
	if err != nil {
		t.Fatal(err)
	}
	future.Close()
	before, _ := os.ReadFile(filepath.Join(dir, "state.json"))
	worker := New()
	err = worker.Init(t.Context(), sdk.InitContext{Core: journalCore{}, Side: "client", StateDir: root, InstanceID: "plugin_future", HostInstanceID: "host_future"})
	if connect.CodeOf(err) != connect.CodeFailedPrecondition || worker.store != nil {
		t.Fatal("future sync journal was initialized", err)
	}
	after, _ := os.ReadFile(filepath.Join(dir, "state.json"))
	if !bytes.Equal(before, after) {
		t.Fatal("future journal overwritten")
	}
}

func TestLegacyActiveUnknownCommitMustNotOverwriteRevert(t *testing.T) {
	f := newJournalFixture(t)
	legacy := f.saved(t) // The last old-version snapshot was still active.
	write(t, f.task.data.Mapping.LocalRoot, "file.txt", "committed after old active snapshot")
	if err := f.transfer(t, false); err != nil {
		t.Fatal(err)
	}
	write(t, f.remoteRoot, "file.txt", "baseline") // External ABA after that real commit.
	f.task.worker.store.Close()
	dir := filepath.Join(t.TempDir(), "legacy-ledger")
	old, err := storage.Open(dir, storage.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := old.Update(func(s *storage.Snapshot) error {
		encoded, _ := json.Marshal(legacy)
		var fields map[string]json.RawMessage
		json.Unmarshal(encoded, &fields)
		delete(fields, "operations")
		delete(fields, "legacy_unverified")
		return s.Put(mappingsBucket, legacy.Mapping.Id, fields)
	}); err != nil {
		t.Fatal(err)
	}
	old.Close()
	f.ledger = dir
	f.restart(t)
	beforeWrites := f.probe.writes
	for range 3 {
		if err := f.task.reconcile(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	if f.probe.writes != beforeWrites || contents(t, f.remoteRoot, "file.txt") != "baseline" || contents(t, f.task.data.Mapping.LocalRoot, "file.txt") != "committed after old active snapshot" {
		t.Fatal("migration silently replayed an unprovable legacy commit over external revert")
	}
	if f.saved(t).Conflicts["file.txt"] == nil || !f.saved(t).LegacyUnverified["file.txt"] {
		t.Fatal("legacy uncertainty was not exposed for review")
	}
	c := f.saved(t).Conflicts["file.txt"]
	if _, err := f.task.worker.ResolveConflict(t.Context(), &ppb.ResolveConflictRequest{MappingId: "map_test", ConflictId: c.Id, ExpectedGeneration: c.Generation, Choice: ppb.ResolveConflictRequest_KEEP_REMOTE, RequestId: "legacy-review"}); err != nil {
		t.Fatal(err)
	}
	if len(f.saved(t).LegacyUnverified) != 0 || contents(t, f.task.data.Mapping.LocalRoot, "file.txt") != "baseline" {
		t.Fatal("explicit legacy review did not settle the guarded path")
	}
}

func TestLegacyMatchingPathsConvergeWithoutMutation(t *testing.T) {
	f := newJournalFixture(t)
	r := f.saved(t)
	r.LegacyUnverified["file.txt"] = true
	f.task.data = r
	if err := f.task.persist(); err != nil {
		t.Fatal(err)
	}
	f.restart(t)
	if err := f.task.reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if f.probe.writes != 0 || f.probe.deletes != 0 || len(f.saved(t).LegacyUnverified) != 0 {
		t.Fatal("equal legacy observation did not settle without mutation")
	}
}

func TestPausedLegacyGuardIsPendingWithoutConflict(t *testing.T) {
	f := newJournalFixture(t)
	f.task.data.Mapping.SyncEnabled = false
	f.task.data.State = "paused"
	f.task.data.LegacyUnverified["file.txt"] = true
	if err := f.task.persist(); err != nil {
		t.Fatal(err)
	}
	f.restart(t)
	status, err := f.task.worker.GetSyncState(t.Context(), scopeFor(f.task.data.Mapping))
	if err != nil || status.State != "paused" || status.PendingFiles != 1 || status.ConflictCount != 0 {
		t.Fatal("legacy verification guard disappeared from paused pending status", status, err)
	}
}
