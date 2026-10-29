package state

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestAtomicWriteRejectsReplacedDirectory(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "verified")
	moved := filepath.Join(root, "moved")
	outside := filepath.Join(root, "outside")
	for _, dir := range []string{directory, outside} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(outside, "state.json"), []byte("outside-original"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := EnsurePrivateDir(directory); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(directory, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, directory); err != nil {
		t.Fatal(err)
	}
	err := AtomicWrite(directory, "state.json", []byte("new-state"))
	data, readErr := os.ReadFile(filepath.Join(outside, "state.json"))
	if readErr != nil || !bytes.Equal(data, []byte("outside-original")) {
		t.Fatalf("replacement redirected state write: %q, %v (write error %v)", data, readErr, err)
	}
	if err == nil {
		t.Fatal("replacement directory was accepted")
	}
	if _, err := os.Stat(filepath.Join(moved, "state.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("failed write modified original directory", err)
	}
}

func TestStoreRejectsDirectoryReplacementWithoutUsingAnotherWriter(t *testing.T) {
	for _, kind := range []string{"symlink", "directory", "fifo", "during-update"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, "first")
			old := filepath.Join(root, "first-old")
			otherDir := filepath.Join(root, "other")
			first := openTest(t, dir, Options{})
			second := openTest(t, otherDir, Options{})
			if err := first.Update(func(s *Snapshot) error { return s.Put("data", "id", "first") }); err != nil {
				t.Fatal(err)
			}
			if err := second.Update(func(s *Snapshot) error { return s.Put("data", "id", "other") }); err != nil {
				t.Fatal(err)
			}
			original, err := os.ReadFile(filepath.Join(dir, "state.json"))
			if err != nil {
				t.Fatal(err)
			}
			other, err := os.ReadFile(filepath.Join(otherDir, "state.json"))
			if err != nil {
				t.Fatal(err)
			}
			replace := func() {
				t.Helper()
				if err := os.Rename(dir, old); err != nil {
					t.Fatal(err)
				}
				switch kind {
				case "directory":
					if err := os.Rename(otherDir, dir); err != nil {
						t.Fatal(err)
					}
				case "fifo":
					if err := syscall.Mkfifo(dir, 0600); err != nil {
						t.Fatal(err)
					}
				default:
					if err := os.Symlink(otherDir, dir); err != nil {
						t.Fatal(err)
					}
				}
			}
			if kind != "during-update" {
				replace()
			}
			called := false
			err = first.Update(func(s *Snapshot) error {
				called = true
				if kind == "during-update" {
					replace()
				}
				return s.Put("data", "id", "unauthorized")
			})
			if err == nil {
				t.Fatal("replaced state directory accepted a transaction")
			}
			if called != (kind == "during-update") {
				t.Fatal("callback admitted after replacement", called)
			}
			if _, err := first.View(); !errors.Is(err, ErrClosed) {
				t.Fatal("changed-directory writer remained active", err)
			}
			otherPath := otherDir
			if kind == "directory" {
				otherPath = dir
			}
			got, err := os.ReadFile(filepath.Join(otherPath, "state.json"))
			if err != nil || !bytes.Equal(got, other) {
				t.Fatal("another writer's primary changed", err)
			}
			got, err = os.ReadFile(filepath.Join(old, "state.json"))
			if err != nil || !bytes.Equal(got, original) {
				t.Fatal("original primary changed", err)
			}
			reopened := openTest(t, old, Options{})
			snapshot, err := reopened.View()
			if err != nil {
				t.Fatal(err)
			}
			var value string
			if err := snapshot.Get("data", "id", &value); err != nil || value != "first" {
				t.Fatal("original lock/recovery failed", value, err)
			}
		})
	}
}

func TestMigrationCannotRedirectBackupIntoAnotherStateDirectory(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "first")
	moved := filepath.Join(root, "first-old")
	other := filepath.Join(root, "other")
	initial := openTest(t, dir, Options{})
	initial.Close()
	second := openTest(t, other, Options{})
	_ = second
	original, err := os.ReadFile(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	otherBytes, err := os.ReadFile(filepath.Join(other, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	opts := Options{SchemaVersion: 2, Migrations: map[int]func(*Snapshot) error{1: func(s *Snapshot) error {
		if err := os.Rename(dir, moved); err != nil {
			return err
		}
		if err := os.Symlink(other, dir); err != nil {
			return err
		}
		s.SchemaVersion = 2
		return nil
	}}}
	store, err := Open(dir, opts)
	if store != nil {
		store.Close()
		t.Fatal("migration admitted a replaced directory")
	}
	if !errors.Is(err, ErrMigrationBackup) {
		t.Fatal("migration backup error not preserved", err)
	}
	got, err := os.ReadFile(filepath.Join(other, "state.json"))
	if err != nil || !bytes.Equal(got, otherBytes) {
		t.Fatal("other state changed", err)
	}
	backups, err := filepath.Glob(filepath.Join(other, "*.pre-migration-*"))
	if err != nil || len(backups) != 0 {
		t.Fatal("migration wrote outside locked directory", backups, err)
	}
	got, err = os.ReadFile(filepath.Join(moved, "state.json"))
	if err != nil || !bytes.Equal(got, original) {
		t.Fatal("migration changed original state", err)
	}
	reopened := openTest(t, moved, Options{})
	reopened.Close()
}

func TestAtomicWriteRejectsNonBasenames(t *testing.T) {
	for _, name := range []string{"", ".", "..", "/", "../outside", "child/file"} {
		if err := AtomicWrite(t.TempDir(), name, []byte("value")); err == nil {
			t.Errorf("accepted %q", name)
		}
	}
}
