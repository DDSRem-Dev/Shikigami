package state

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestPrimaryWriteFailureClosesWriterAndPreservesPrevious(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	s := openTest(t, dir, Options{})
	if err := s.Update(func(snapshot *Snapshot) error { return snapshot.Put("records", "id", "original") }); err != nil {
		t.Fatal(err)
	}
	primary := filepath.Join(dir, "state.json")
	if err := os.Rename(primary, filepath.Join(dir, "original.json")); err != nil {
		t.Fatal(err)
	}
	// A directory makes the primary rename fail on both supported platforms,
	// after the previous snapshot has already been written successfully.
	if err := os.Mkdir(primary, 0700); err != nil {
		t.Fatal(err)
	}
	if err := s.Update(func(snapshot *Snapshot) error { return snapshot.Put("records", "id", "uncommitted") }); err == nil {
		t.Fatal("primary rename failure reported success")
	}
	if _, err := s.View(); !errors.Is(err, ErrClosed) {
		t.Fatalf("failed writer continued serving stale memory: %v", err)
	}
	called := false
	if err := s.Update(func(*Snapshot) error { called = true; return nil }); !errors.Is(err, ErrClosed) || called {
		t.Fatalf("failed writer admitted another transaction: %v, callback=%t", err, called)
	}
	previous, err := readSnapshot(filepath.Join(dir, "state.previous.json"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	var value string
	if err := previous.Get("records", "id", &value); err != nil || value != "original" {
		t.Fatalf("previous committed value lost: %q %v", value, err)
	}
	// Repair the injected filesystem fault, then prove the failed store released
	// its exclusive lock and reopening reads the last committed data.
	if err := os.Remove(primary); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(dir, "original.json"), primary); err != nil {
		t.Fatal(err)
	}
	reopened := openTest(t, dir, Options{})
	snapshot, err := reopened.View()
	if err != nil {
		t.Fatal(err)
	}
	if err := snapshot.Get("records", "id", &value); err != nil || value != "original" {
		t.Fatalf("reopen exposed uncommitted value: %q %v", value, err)
	}
}

func TestRequestNamespacesAndPendingResultsSurviveRestart(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	s := openTest(t, dir, Options{})
	for _, namespace := range []string{"method-a", "method-b"} {
		if err := s.Update(func(snapshot *Snapshot) error {
			_, replay, err := snapshot.Request(namespace, "shared-id", map[string]string{"scope": namespace})
			if replay {
				t.Fatal("request ID leaked across method namespaces")
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := openTest(t, dir, Options{})
	snapshot, err := reopened.View()
	if err != nil {
		t.Fatal(err)
	}
	for _, namespace := range []string{"method-a", "method-b"} {
		record, replay, err := snapshot.Request(namespace, "shared-id", map[string]string{"scope": namespace})
		if err != nil || !replay || record.Status != "pending" {
			t.Fatalf("pending intent lost after restart: %+v %t %v", record, replay, err)
		}
		if _, _, err := snapshot.Request(namespace, "shared-id", map[string]string{"scope": "different"}); !errors.Is(err, ErrRequestMismatch) {
			t.Fatalf("request replay crossed parameter scope: %v", err)
		}
	}
}
