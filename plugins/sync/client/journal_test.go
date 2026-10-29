package client

import (
	"connectrpc.com/connect/v2"
	"context"
	"errors"
	storage "github.com/DDSRem-Dev/Shikigami/pkg/plugin/state"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	"golang.org/x/sys/unix"
	"google.golang.org/protobuf/proto"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestUncertainUploadKeepsJournalAndRevertConflictAcrossScans(t *testing.T) {
	for _, phase := range []string{"prepared", "committed"} {
		for _, reverted := range []string{"local", "remote"} {
			t.Run(phase+"/"+reverted, func(t *testing.T) {
				f := newJournalFixture(t)
				task := f.task
				base := proto.Clone(task.data.Baselines["file.txt"]).(*ppb.FileVersion)
				write(t, task.data.Mapping.LocalRoot, "file.txt", "uploaded")
				f.probe.lostACK = phase == "prepared"
				f.probe.before = func() {
					r := f.saved(t)
					op := r.Operations["file.txt"]
					if op == nil || op.Phase != "prepared" || !proto.Equal(r.Baselines["file.txt"], base) {
						t.Fatal("mutation entered without durable prepared operation")
					}
					if err := verifyStages(t.Context(), task.stageDir, r.Operations); err != nil {
						t.Fatal(err)
					}
				}
				err := f.transfer(t, false)
				if phase == "prepared" && connect.CodeOf(err) != connect.CodeUnavailable || phase == "committed" && err != nil {
					t.Fatal(err)
				}
				op := f.saved(t).Operations["file.txt"]
				if op.Phase != phase || phase == "committed" && op.Result.Revision == "" {
					t.Fatal("incorrect commit phase")
				}
				originalStage := op.Stage
				root := task.data.Mapping.LocalRoot
				if reverted == "remote" {
					root = f.remoteRoot
				}
				write(t, root, "file.txt", "baseline")
				f.restart(t)
				for range 3 {
					if err := task.reconcile(t.Context()); err != nil {
						t.Fatal(err)
					}
				}
				if f.probe.writes != 1 || f.probe.deletes != 0 || contents(t, root, "file.txt") != "baseline" {
					t.Fatal("uncertain commit replayed over reverted contents")
				}
				r := f.saved(t)
				if r.Operations["file.txt"] == nil || r.Conflicts["file.txt"] == nil || !proto.Equal(r.Baselines["file.txt"], base) {
					t.Fatal("recovery dropped uncertainty or baseline")
				}
				if contents(t, task.staging, originalStage) != "uploaded" {
					t.Fatal("uncertain source snapshot not retained")
				}
				f.probe.before = nil
				f.probe.lostACK = false
				c := r.Conflicts["file.txt"]
				choice := ppb.ResolveConflictRequest_KEEP_REMOTE
				if reverted == "local" {
					choice = ppb.ResolveConflictRequest_KEEP_LOCAL
				}
				if _, err := task.worker.ResolveConflict(t.Context(), &ppb.ResolveConflictRequest{MappingId: "map_test", ConflictId: c.Id, ExpectedGeneration: c.Generation, Choice: choice, RequestId: "resolve-reverted-version"}); err != nil {
					t.Fatal(err)
				}
				r = f.saved(t)
				if len(r.Operations) != 0 || len(r.Conflicts) != 0 {
					t.Fatal("explicit resolution did not atomically settle journal")
				}
				if contents(t, task.data.Mapping.LocalRoot, "file.txt") != "baseline" || contents(t, f.remoteRoot, "file.txt") != "baseline" {
					t.Fatal("explicit choice not honored")
				}
				entries, err := os.ReadDir(task.staging)
				if err != nil || len(entries) != 0 {
					t.Fatal("resolved stage not cleaned", err)
				}
			})
		}
	}
}

func TestDownloadAndDeleteCommitBeforeBaseline(t *testing.T) {
	for _, operation := range []string{"download", "delete-local", "delete-remote"} {
		t.Run(operation, func(t *testing.T) {
			f := newJournalFixture(t)
			task := f.task
			download := operation != "delete-remote"
			if operation == "download" {
				write(t, f.remoteRoot, "file.txt", "remote update")
			} else {
				root := f.remoteRoot
				if !download {
					root = task.data.Mapping.LocalRoot
				}
				if err := os.Remove(filepath.Join(root, "file.txt")); err != nil {
					t.Fatal(err)
				}
			}
			base := proto.Clone(task.data.Baselines["file.txt"]).(*ppb.FileVersion)
			if err := f.transfer(t, download); err != nil {
				t.Fatal(err)
			}
			r := f.saved(t)
			op := r.Operations["file.txt"]
			if op == nil || op.Phase != "committed" || !proto.Equal(r.Baselines["file.txt"], base) {
				t.Fatal("commit was not persisted independently of old baseline")
			}
			if operation != "download" && (op.Source.Exists || op.Result.Exists || op.Stage != "") {
				t.Fatal("deletion journal has incorrect result")
			}
			f.restart(t)
			if err := task.reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			if len(f.saved(t).Operations) != 0 {
				t.Fatal("equal recovery retained journal")
			}
			if operation != "download" && f.saved(t).Baselines["file.txt"].Exists {
				t.Fatal("deletion baseline not recovered")
			}
		})
	}
}

func TestPreparedPersistenceFailureDoesNotMutate(t *testing.T) {
	f := newJournalFixture(t)
	write(t, f.task.data.Mapping.LocalRoot, "file.txt", "must not commit")
	f.task.worker.store.Close()
	if err := f.transfer(t, false); !errors.Is(err, storage.ErrClosed) {
		t.Fatal("expected closed store error", err)
	}
	if f.probe.writes != 0 || contents(t, f.remoteRoot, "file.txt") != "baseline" {
		t.Fatal("mutation escaped failed prepared persistence")
	}
}

func TestRetainedStageRejectsFIFOAndCancellation(t *testing.T) {
	f := newJournalFixture(t)
	write(t, f.task.data.Mapping.LocalRoot, "file.txt", "retain")
	f.probe.lostACK = true
	if err := f.transfer(t, false); err == nil {
		t.Fatal("missing injected ACK failure")
	}
	operations := f.saved(t).Operations
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := verifyStages(ctx, f.task.stageDir, operations); !errors.Is(err, context.Canceled) {
		t.Fatal("stage verification ignored cancellation", err)
	}
	stage := filepath.Join(f.task.staging, operations["file.txt"].Stage)
	if err := os.Remove(stage); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mkfifo(stage, 0600); err != nil {
		t.Fatal(err)
	}
	if err := verifyStages(t.Context(), f.task.stageDir, operations); err == nil {
		t.Fatal("FIFO retained stage accepted")
	}
}

func TestCommitPersistenceFailureRetainsPreparedAndStopsWorker(t *testing.T) {
	f := newJournalFixture(t)
	write(t, f.task.data.Mapping.LocalRoot, "file.txt", "committed before local persist failure")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	f.task.worker.lifetime = ctx
	f.task.worker.cancel = cancel
	f.probe.after = func() { f.task.worker.store.Close() }
	if err := f.transfer(t, false); !errors.Is(err, storage.ErrClosed) {
		t.Fatal("expected committed journal persistence error", err)
	}
	if ctx.Err() == nil || f.probe.writes != 1 {
		t.Fatal("journal persistence failure did not cancel further worker activity")
	}
	health, err := f.task.worker.Health(t.Context())
	if err != nil || health.Status != pb.HealthStatus_UNHEALTHY {
		t.Fatal("stopped writer advertised healthy", err)
	}
	if _, err := f.task.worker.get("map_test"); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatal("stopped writer accepted business call", err)
	}
	f.restart(t)
	r := f.saved(t)
	if r.Operations["file.txt"].Phase != "prepared" || r.Baselines["file.txt"].ContentHash == r.Operations["file.txt"].Source.ContentHash {
		t.Fatal("unpersisted commit result replaced durable prepared intent")
	}
	if err := verifyStages(t.Context(), f.task.stageDir, r.Operations); err != nil {
		t.Fatal(err)
	}
}

func TestKeepBothInterruptedCopyStepsRetainIndependentEvidence(t *testing.T) {
	for _, phase := range []string{"after-local-copy", "after-remote-copy-lost-ack"} {
		t.Run(phase, func(t *testing.T) {
			f := newJournalFixture(t)
			task := f.task
			write(t, task.data.Mapping.LocalRoot, "file.txt", "local conflict")
			write(t, f.remoteRoot, "file.txt", "remote conflict")
			if err := task.reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			c := f.saved(t).Conflicts["file.txt"]
			if phase == "after-local-copy" {
				f.probe.failReadAt = 2
			} else {
				f.probe.lostACK = true
			}
			_, err := task.worker.ResolveConflict(t.Context(), &ppb.ResolveConflictRequest{MappingId: "map_test", ConflictId: c.Id, ExpectedGeneration: c.Generation, Choice: ppb.ResolveConflictRequest_KEEP_BOTH, RequestId: "copy-choice"})
			if connect.CodeOf(err) != connect.CodeUnavailable {
				t.Fatal("missing injected copy interruption", err)
			}
			r := f.saved(t)
			if len(r.Operations) != 1 || len(r.Copies) != 1 {
				t.Fatal("copy operation evidence missing")
			}
			var copyPath string
			for name, op := range r.Operations {
				copyPath = name
				if op.SourcePath != "file.txt" || r.Copies[name] != "file.txt" {
					t.Fatal("wrong copy lineage")
				}
				want := "prepared"
				if phase == "after-local-copy" {
					want = "committed"
				}
				if op.Phase != want {
					t.Fatal("copy step phase lost")
				}
			}
			if contents(t, task.data.Mapping.LocalRoot, copyPath) != "remote conflict" || contents(t, task.data.Mapping.LocalRoot, "file.txt") != "local conflict" || contents(t, f.remoteRoot, "file.txt") != "remote conflict" {
				t.Fatal("interrupted choice overwrote original or copy")
			}
			beforeWrites := f.probe.writes
			f.probe.failReadAt = 0
			f.probe.lostACK = false
			f.restart(t)
			for range 2 {
				if err := task.reconcile(t.Context()); err != nil {
					t.Fatal(err)
				}
			}
			r = f.saved(t)
			if f.probe.writes != beforeWrites {
				t.Fatal("interrupted KEEP_BOTH blindly replayed remote copy")
			}
			if phase == "after-local-copy" {
				if r.Operations[copyPath] == nil || r.Conflicts[copyPath] == nil {
					t.Fatal("partial copy failed to preserve uncertainty")
				}
			} else if r.Operations[copyPath] != nil || r.Baselines[copyPath] == nil {
				t.Fatal("matching copies failed to converge")
			}
			if _, err := task.worker.ResolveConflict(t.Context(), &ppb.ResolveConflictRequest{MappingId: "map_test", ConflictId: c.Id, ExpectedGeneration: c.Generation, Choice: ppb.ResolveConflictRequest_KEEP_BOTH, RequestId: "copy-choice"}); connect.CodeOf(err) != connect.CodeAborted {
				t.Fatal("unknown request replayed side effects", err)
			}
		})
	}
}

func TestArchivedMappingCannotDiscardRetainedOperationByIDReuse(t *testing.T) {
	f := newJournalFixture(t)
	task := f.task
	canonical, err := filepath.EvalSymlinks(task.data.Mapping.LocalRoot)
	if err != nil {
		t.Fatal(err)
	}
	task.data.Mapping.LocalRoot = canonical
	write(t, canonical, "file.txt", "retain archived snapshot")
	f.probe.lostACK = true
	if err := f.transfer(t, false); err == nil {
		t.Fatal("missing uncertain commit")
	}
	op := f.saved(t).Operations["file.txt"]
	if err := task.worker.archiveMapping("map_test"); err != nil {
		t.Fatal(err)
	}
	core := &coreProbe{Tree: f.probe}
	core.SetMapping(task.data.Mapping)
	task.worker.core = coreClient(t, t.Context(), core)
	if _, err := task.worker.configureMapping(t.Context(), task.data.Mapping); connect.CodeOf(err) != connect.CodeFailedPrecondition || err.Error() != "failed_precondition: MAPPING_ARCHIVED" {
		t.Fatal("archived ID reuse was not rejected", err)
	}
	s, err := task.worker.store.View()
	if err != nil {
		t.Fatal(err)
	}
	var archived record
	if err := s.Get(archivedMappingsBucket, "map_test", &archived); err != nil {
		t.Fatal(err)
	}
	if archived.Operations["file.txt"].ID != op.ID || contents(t, task.staging, op.Stage) != "retain archived snapshot" {
		t.Fatal("archived uncertain operation was lost")
	}
}

func TestPausedSyncStateCountsUnresolvedPathsBeforeConflictExists(t *testing.T) {
	f := newJournalFixture(t)
	task := f.task
	write(t, task.data.Mapping.LocalRoot, "file.txt", "uncertain upload")
	f.probe.lostACK = true
	if err := f.transfer(t, false); err == nil {
		t.Fatal("missing uncertain upload")
	}
	task.data.Mapping.SyncEnabled = false
	task.data.State = "paused"
	if err := task.persist(); err != nil {
		t.Fatal(err)
	}
	status, err := task.worker.GetSyncState(t.Context(), scopeFor(task.data.Mapping))
	if err != nil || status.State != "paused" || status.PendingFiles != 1 || status.ConflictCount != 0 || status.MappingRevision != task.data.Mapping.Revision {
		t.Fatal("paused unknown operation was advertised as no pending work", status, err)
	}
	task.data.LegacyUnverified["file.txt"] = true
	task.data.LegacyUnverified["legacy-only.txt"] = true
	source := task.data.Operations["file.txt"].Source
	target := task.data.Operations["file.txt"].Target
	if err := task.conflict("file.txt", source, target); err != nil {
		t.Fatal(err)
	}
	if err := task.conflict("conflict-only.txt", source, target); err != nil {
		t.Fatal(err)
	}
	status, err = task.worker.GetSyncState(t.Context(), scopeFor(task.data.Mapping))
	if err != nil || status.PendingFiles != 3 || status.ConflictCount != 2 {
		t.Fatal("pending status double-counted or omitted unresolved paths", status, err)
	}
}

func TestSyncStateWaiterRejectsCanceledWriterAfterLock(t *testing.T) {
	f := newJournalFixture(t)
	task := f.task
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	task.worker.lifetime = ctx
	task.worker.cancel = cancel
	task.op.Lock()
	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		close(started)
		_, err := task.worker.GetSyncState(t.Context(), scopeFor(task.data.Mapping))
		done <- err
	}()
	<-started
	select {
	case err := <-done:
		task.op.Unlock()
		t.Fatal("status bypassed serialized operation", err)
	case <-time.After(20 * time.Millisecond):
	}
	task.worker.store.Close()
	if err := task.worker.update(func(*storage.Snapshot) error { return nil }); !errors.Is(err, storage.ErrClosed) {
		task.op.Unlock()
		t.Fatal(err)
	}
	task.op.Unlock()
	select {
	case err := <-done:
		if connect.CodeOf(err) != connect.CodeFailedPrecondition {
			t.Fatal("waiting status returned data from a canceled writer", err)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled status waiter did not return")
	}
}
