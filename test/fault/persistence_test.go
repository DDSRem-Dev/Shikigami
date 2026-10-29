package fault_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/DDSRem-Dev/Shikigami/internal/auth"
	state "github.com/DDSRem-Dev/Shikigami/pkg/plugin/state"
)

// A failed backup rename precedes the primary commit. It must neither report
// success nor advance live state; after repairing this isolated obstruction,
// the same writer may retry without losing the original transaction.
func TestBackupRenameFailurePreservesPrimaryAndRetry(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	store, err := state.Open(dir, state.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	update := func(value string) error {
		return store.Update(func(s *state.Snapshot) error { return s.Put("values", "one", value) })
	}
	if err := update("original"); err != nil {
		t.Fatal(err)
	}
	primary := readBytes(t, filepath.Join(dir, "state.json"))
	previous := filepath.Join(dir, "state.previous.json")
	if err := os.Remove(previous); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(previous, 0700); err != nil {
		t.Fatal(err)
	}
	if err := update("rejected"); err == nil {
		t.Fatal("backup rename obstruction reported commit success")
	}
	if !bytes.Equal(primary, readBytes(t, filepath.Join(dir, "state.json"))) {
		t.Fatal("backup failure changed primary state")
	}
	snapshot, err := store.View()
	if err != nil {
		t.Fatal(err)
	}
	var value string
	if err := snapshot.Get("values", "one", &value); err != nil || value != "original" {
		t.Fatalf("backup failure advanced live state: %q %v", value, err)
	}
	noStateTemps(t, dir)
	if err := os.Remove(previous); err != nil {
		t.Fatal(err)
	}
	if err := update("retried"); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := state.Open(dir, state.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	snapshot, err = reopened.View()
	if err != nil {
		t.Fatal(err)
	}
	if err := snapshot.Get("values", "one", &value); err != nil || value != "retried" {
		t.Fatalf("repaired retry did not persist: %q %v", value, err)
	}
}

func TestFailedRevocationWriteDoesNotUndoPriorRevocation(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "auth")
	manager, err := auth.Open(dir, "fault-user")
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	first, firstInfo, err := manager.Create("fault-user", []string{"session.attach"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	second, secondInfo, err := manager.Create("fault-user", []string{"session.attach"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Revoke(firstInfo.ID); err != nil {
		t.Fatal(err)
	}
	primary := readBytes(t, filepath.Join(dir, "state.json"))
	previous := filepath.Join(dir, "state.previous.json")
	if err := os.Remove(previous); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(previous, 0700); err != nil {
		t.Fatal(err)
	}
	if err := manager.Revoke(secondInfo.ID); err == nil {
		t.Fatal("uncommitted revocation was reported as successful")
	}
	if !bytes.Equal(primary, readBytes(t, filepath.Join(dir, "state.json"))) {
		t.Fatal("failed revocation changed the primary registry")
	}
	if _, err := manager.Verify(first); !errors.Is(err, auth.ErrUnauthenticated) {
		t.Fatal("prior committed revocation was lost", err)
	}
	// The second revocation failed before commit. Do not falsely claim that
	// the still-valid credential was revoked; the caller must repair and retry.
	if _, err := manager.Verify(second); err != nil {
		t.Fatal("precommit failure unexpectedly changed unrelated committed state", err)
	}
	noStateTemps(t, dir)
	if err := os.Remove(previous); err != nil {
		t.Fatal(err)
	}
	if err := manager.Revoke(secondInfo.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Verify(second); !errors.Is(err, auth.ErrUnauthenticated) {
		t.Fatal("successful retry did not revoke credential", err)
	}
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
	// The rolling backup predates the second revocation. Even though it is
	// structurally valid, it must not be used when the primary is damaged.
	damaged := []byte("interrupted-primary")
	if err := os.WriteFile(filepath.Join(dir, "state.json"), damaged, 0600); err != nil {
		t.Fatal(err)
	}
	if reopened, err := auth.Open(dir, "fault-user"); err == nil {
		reopened.Close()
		t.Fatal("credential registry restored a revoked token from the old backup")
	}
	if !bytes.Equal(damaged, readBytes(t, filepath.Join(dir, "state.json"))) {
		t.Fatal("credential recovery overwrote damaged primary evidence")
	}
}

func readBytes(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func noStateTemps(t *testing.T, dir string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, ".state-*"))
	if err != nil || len(matches) != 0 {
		t.Fatalf("state temporary files leaked: %d %v", len(matches), err)
	}
}
