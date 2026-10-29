package state

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func openTest(t *testing.T, dir string, opts Options) *Store {
	t.Helper()
	s, err := Open(dir, opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	return s
}

func TestAtomicRequestReservationAndRestart(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	s := openTest(t, dir, Options{})
	var wg sync.WaitGroup
	for range 24 {
		wg.Go(func() {
			err := s.Update(func(state *Snapshot) error {
				_, replay, err := state.Request("CreateProject", "request-1", map[string]string{"name": "app"})
				if err != nil {
					return err
				}
				if replay {
					return nil
				}
				if err := state.Put("projects", "proj_one", map[string]string{"name": "app"}); err != nil {
					return err
				}
				return state.CompleteRequest("CreateProject", "request-1", "proj_one", map[string]string{"id": "proj_one"}, "")
			})
			if err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s = openTest(t, dir, Options{})
	state, err := s.View()
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Buckets["projects"]) != 1 {
		t.Fatal("duplicate project reservation")
	}
	record, replay, err := state.Request("CreateProject", "request-1", map[string]string{"name": "app"})
	if err != nil || !replay || record.ResourceID != "proj_one" || record.Status != "succeeded" {
		t.Fatalf("lost idempotent result: %+v %v", record, err)
	}
	_, _, err = state.Request("CreateProject", "request-1", map[string]string{"name": "different"})
	if !errors.Is(err, ErrRequestMismatch) {
		t.Fatalf("want request mismatch, got %v", err)
	}
	state.Buckets["projects"]["proj_one"] = json.RawMessage(`{"name":"mutated"}`)
	fresh, _ := s.View()
	if string(fresh.Buckets["projects"]["proj_one"]) == string(state.Buckets["projects"]["proj_one"]) {
		t.Fatal("view aliases live state")
	}
}

func TestRecoveryPreservesCorruptionAndVerifiedPrevious(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	s := openTest(t, dir, Options{})
	for _, name := range []string{"first", "second"} {
		if err := s.Update(func(state *Snapshot) error { return state.Put("projects", name, map[string]string{"name": name}) }); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "state.json"), []byte("truncated"), 0600); err != nil {
		t.Fatal(err)
	}
	s = openTest(t, dir, Options{})
	if !s.RecoveredFromBackup() {
		t.Fatal("backup not reported")
	}
	state, _ := s.View()
	if len(state.Buckets["projects"]) != 1 || state.Buckets["projects"]["first"] == nil {
		t.Fatal("did not restore previous transaction")
	}
	files, err := filepath.Glob(filepath.Join(dir, "state.corrupt.*.json"))
	if err != nil || len(files) != 1 {
		t.Fatal("corrupt evidence missing")
	}
	data, _ := os.ReadFile(files[0])
	if string(data) != "truncated" {
		t.Fatal("corrupt bytes changed")
	}
}

func TestFutureSchemaDoesNotFallBackOrMutate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	s := openTest(t, dir, Options{})
	if err := s.Update(func(state *Snapshot) error { return nil }); err != nil {
		t.Fatal(err)
	}
	s.Close()
	data := []byte(`{"schema_version":999,"revision":99,"buckets":{},"requests":{}}`)
	if err := os.WriteFile(filepath.Join(dir, "state.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	_, err := Open(dir, Options{})
	if !errors.Is(err, ErrFutureSchema) {
		t.Fatalf("got %v", err)
	}
	after, _ := os.ReadFile(filepath.Join(dir, "state.json"))
	if string(data) != string(after) {
		t.Fatal("future schema overwritten")
	}
}

func TestLockPermissionsAndFailedTransaction(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	s := openTest(t, dir, Options{})
	if other, err := Open(dir, Options{}); !errors.Is(err, ErrLocked) {
		if other != nil {
			other.Close()
		}
		t.Fatalf("second writer admitted: %v", err)
	}
	marker := errors.New("transaction failed")
	err := s.Update(func(state *Snapshot) error { state.Put("projects", "lost", map[string]string{"x": "y"}); return marker })
	if !errors.Is(err, marker) {
		t.Fatal(err)
	}
	state, _ := s.View()
	if len(state.Buckets) != 0 {
		t.Fatal("failed transaction leaked")
	}
	s.Close()
	if err := os.Chmod(filepath.Join(dir, "state.json"), 0644); err != nil {
		t.Fatal(err)
	}
	if other, err := Open(dir, Options{DisableRecovery: true}); err == nil {
		other.Close()
		t.Fatal("world-readable state accepted")
	}
}

func TestExplicitMigration(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	if err := EnsurePrivateDir(dir); err != nil {
		t.Fatal(err)
	}
	if err := AtomicWrite(dir, "state.json", []byte(`{"schema_version":0,"revision":2,"buckets":{},"requests":{}}`)); err != nil {
		t.Fatal(err)
	}
	s := openTest(t, dir, Options{Migrations: map[int]func(*Snapshot) error{0: func(state *Snapshot) error {
		state.SchemaVersion = 1
		return state.Put("migration", "marker", map[string]bool{"done": true})
	}}})
	state, _ := s.View()
	if state.SchemaVersion != 1 || state.Buckets["migration"]["marker"] == nil {
		t.Fatal("migration not applied")
	}
}

func TestNoValidBackupFailsWithoutOverwriting(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	if err := EnsurePrivateDir(dir); err != nil {
		t.Fatal(err)
	}
	if err := AtomicWrite(dir, "state.json", []byte("bad")); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(dir, Options{}); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("got %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "state.json"))
	if string(data) != "bad" {
		t.Fatal("evidence overwritten")
	}
}
