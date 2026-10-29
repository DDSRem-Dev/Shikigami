package client

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	storage "github.com/DDSRem-Dev/Shikigami/pkg/plugin/state"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
)

// Exercise each existing drain dependency, not a new implementation's state
// fields. The same test must compile and fail behaviorally before the fix.
func TestSyncWorkerStopHonorsCallerWhileDrainRemainsOwned(t *testing.T) {
	for _, blocked := range []string{"configuration", "watchers", "reconciliation", "mapping-feed", "file-operation"} {
		t.Run(blocked, func(t *testing.T) {
			f := newJournalFixture(t)
			w, task := f.task.worker, f.task
			// Retain a real unknown upload and its staged bytes through shutdown.
			write(t, task.data.Mapping.LocalRoot, "file.txt", "committed without ACK")
			f.probe.lostACK = true
			if err := f.transfer(t, false); connect.CodeOf(err) != connect.CodeUnavailable {
				t.Fatal(err)
			}
			before := f.saved(t)
			beforeJSON, err := json.Marshal(before)
			if err != nil {
				t.Fatal(err)
			}
			w.lifetime, w.cancel = context.WithCancel(t.Context())
			defer w.cancel()
			var release func()
			switch blocked {
			case "configuration":
				w.configure.Lock()
				release = w.configure.Unlock
			case "watchers":
				task.watches.Add(1)
				release = task.watches.Done
			case "reconciliation":
				w.done = make(chan struct{})
				release = func() { close(w.done) }
			case "mapping-feed":
				w.mappingsDone = make(chan struct{})
				release = func() { close(w.mappingsDone) }
			case "file-operation":
				task.op.Lock()
				release = task.op.Unlock
			}
			release = sync.OnceFunc(release)
			defer release()
			var pending []<-chan error
			for attempt := 0; attempt < 2; attempt++ {
				ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
				result := make(chan error, 1)
				go func() { result <- w.Stop(ctx) }()
				select {
				case err := <-result:
					if !errors.Is(err, context.DeadlineExceeded) {
						t.Errorf("Stop attempt %d reported completed drain while %s is blocked: %v", attempt, blocked, err)
					}
				case <-time.After(time.Second):
					t.Errorf("Stop attempt %d ignored caller deadline behind %s", attempt, blocked)
					pending = append(pending, result)
				}
				cancel()
			}
			if w.lifetime.Err() == nil {
				t.Error("Stop did not cancel work before waiting for drain")
			}
			if _, err := w.store.View(); err != nil {
				t.Errorf("ledger closed while an operation could still use it: %v", err)
			}
			release()
			for _, result := range pending {
				select {
				case <-result:
				case <-time.After(time.Second):
					t.Fatal("Stop did not return after the drain dependency was released")
				}
			}
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			if err := w.Stop(ctx); err != nil {
				t.Fatal("released drain failed", err)
			}
			if _, err := w.store.View(); !errors.Is(err, storage.ErrClosed) {
				t.Errorf("successful Stop left ledger open: %v", err)
			}
			if _, err := task.local.StatFile(t.Context(), &pb.StatFileRequest{ProjectId: "proj_test", Path: "file.txt"}); connect.CodeOf(err) != connect.CodeUnavailable {
				t.Errorf("successful Stop left file tree open: %v", err)
			}
			if stage, err := task.stageDir.CreateTemp(); !errors.Is(err, os.ErrClosed) {
				if stage != nil {
					stage.Close()
					task.stageDir.Remove(stage.Name())
				}
				t.Errorf("successful Stop left staging descriptor open: %v", err)
			}
			reopened, err := storage.Open(f.ledger, ledgerOptions())
			if err != nil {
				t.Fatal("successful Stop retained ledger writer ownership", err)
			}
			defer reopened.Close()
			snapshot, err := reopened.View()
			if err != nil {
				t.Fatal(err)
			}
			var after record
			if err := snapshot.Get(mappingsBucket, "map_test", &after); err != nil {
				t.Fatal(err)
			}
			afterJSON, err := json.Marshal(after)
			if err != nil || string(beforeJSON) != string(afterJSON) {
				t.Fatalf("shutdown changed unresolved journal/baseline: %v", err)
			}
			if got := contents(t, task.staging, before.Operations["file.txt"].Stage); got != "committed without ACK" {
				t.Fatal("shutdown removed retained source bytes", got)
			}
			if f.probe.writes != 1 || f.probe.deletes != 0 {
				t.Fatal("shutdown replayed unknown mutation")
			}
		})
	}
}

func TestSyncWorkerStopPreservesCleanupErrorAcrossWaiters(t *testing.T) {
	f := newJournalFixture(t)
	// The retained descriptor's close fails; unrelated owners still need to
	// close, and later Stop calls must not turn that failure into success.
	if err := f.task.stageDir.dir.Close(); err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		err := f.task.worker.Stop(ctx)
		cancel()
		if !errors.Is(err, os.ErrClosed) {
			t.Errorf("Stop attempt %d discarded staging close failure: %v", attempt, err)
		}
	}
	if _, err := f.task.worker.store.View(); !errors.Is(err, storage.ErrClosed) {
		t.Errorf("cleanup error prevented ledger release: %v", err)
	}
}
