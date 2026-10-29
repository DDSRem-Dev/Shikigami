package client

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func testStagingDirectory(t *testing.T) (*stagingDirectory, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "staging")
	s, err := openStagingDirectory(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, path
}

func createStage(t *testing.T, s *stagingDirectory, content string) string {
	t.Helper()
	f, err := s.CreateTemp()
	if err != nil {
		t.Fatal(err)
	}
	name := f.Name()
	if !validStage(name) || filepath.Base(name) != name {
		t.Fatal("staging file did not return a safe basename", name)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
	if err := f.Sync(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.Sync(); err != nil {
		t.Fatal(err)
	}
	return name
}

func TestPinnedStagingRetainsEvidenceAndUsesFreshInventory(t *testing.T) {
	s, path := testStagingDirectory(t)
	retained := createStage(t, s, "durable source")
	orphan := createStage(t, s, "unreferenced")
	for range 2 {
		if err := s.Cleanup(map[string]bool{retained: true}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Join(path, orphan)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("orphan was not removed", err)
	}
	f, err := s.Open(retained)
	if err != nil {
		t.Fatal(err)
	}
	info, err := f.Stat()
	f.Close()
	if err != nil || info.Mode().Perm() != 0600 || info.Size() != int64(len("durable source")) {
		t.Fatal("retained stage changed", info, err)
	}
	late := createStage(t, s, "later orphan")
	if err := s.Cleanup(map[string]bool{retained: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(path, late)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("cleanup reused an exhausted directory cursor", err)
	}
	if err := s.Remove(retained); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateTemp(); !errors.Is(err, os.ErrClosed) {
		t.Fatal("closed staging helper remained usable", err)
	}
}

func TestPinnedStagingRejectsDirectoryReplacementWithoutBlocking(t *testing.T) {
	for _, replacement := range []string{"fifo", "symlink", "directory"} {
		t.Run(replacement, func(t *testing.T) {
			s, path := testStagingDirectory(t)
			name := createStage(t, s, "original source")
			original := path + "-original"
			if err := os.Rename(path, original); err != nil {
				t.Fatal(err)
			}
			outside := filepath.Join(t.TempDir(), "outside")
			if err := os.Mkdir(outside, 0700); err != nil {
				t.Fatal(err)
			}
			var err error
			switch replacement {
			case "fifo":
				err = unix.Mkfifo(path, 0700)
			case "symlink":
				err = os.Symlink(outside, path)
			case "directory":
				err = os.Mkdir(path, 0700)
				outside = path
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(outside, name), []byte("replacement sentinel"), 0600); err != nil {
				t.Fatal(err)
			}
			checks := []func() error{
				func() error {
					f, err := s.CreateTemp()
					if f != nil {
						f.Close()
					}
					return err
				},
				func() error {
					f, err := s.Open(name)
					if f != nil {
						f.Close()
					}
					return err
				},
				s.Sync,
				func() error { return s.Remove(name) },
				func() error { return s.Cleanup(nil) },
			}
			for index, check := range checks {
				done := make(chan error, 1)
				go func() { done <- check() }()
				select {
				case err := <-done:
					if err == nil {
						t.Fatalf("operation %d accepted replaced directory", index)
					}
				case <-time.After(time.Second):
					t.Fatalf("operation %d blocked on replacement", index)
				}
			}
			for file, expected := range map[string]string{
				filepath.Join(original, name): "original source",
				filepath.Join(outside, name):  "replacement sentinel",
			} {
				data, err := os.ReadFile(file)
				if err != nil || string(data) != expected {
					t.Fatal("replacement changed evidence", file, err)
				}
			}
		})
	}
}

func TestPinnedStagingRejectsSpecialFilesAndTraversal(t *testing.T) {
	for _, kind := range []string{"fifo", "symlink", "directory", "permissions"} {
		t.Run(kind, func(t *testing.T) {
			s, path := testStagingDirectory(t)
			name := "transfer-unsafe"
			target := filepath.Join(t.TempDir(), "sentinel")
			if err := os.WriteFile(target, []byte("keep"), 0600); err != nil {
				t.Fatal(err)
			}
			var err error
			switch kind {
			case "fifo":
				err = unix.Mkfifo(filepath.Join(path, name), 0600)
			case "symlink":
				err = os.Symlink(target, filepath.Join(path, name))
			case "directory":
				err = os.Mkdir(filepath.Join(path, name), 0700)
			case "permissions":
				err = os.WriteFile(filepath.Join(path, name), nil, 0644)
			}
			if err != nil {
				t.Fatal(err)
			}
			if f, err := s.Open(name); err == nil {
				f.Close()
				t.Fatal("unsafe retained stage accepted")
			}
			if err := s.Remove(name); err == nil {
				t.Fatal("unsafe stage removal accepted")
			}
			if err := s.Cleanup(nil); err == nil {
				t.Fatal("unsafe stage cleanup accepted")
			}
			data, err := os.ReadFile(target)
			if err != nil || string(data) != "keep" {
				t.Fatal("outside sentinel changed", err)
			}
		})
	}
	s, _ := testStagingDirectory(t)
	for _, name := range []string{"../transfer-outside", "/transfer-outside", "transfer-../child"} {
		if f, err := s.Open(name); err == nil {
			f.Close()
			t.Fatal("traversal accepted", name)
		}
		if err := s.Remove(name); err == nil {
			t.Fatal("traversal removal accepted", name)
		}
	}
}

func TestPinnedStagingInventoryLimitPreservesOrphans(t *testing.T) {
	s, path := testStagingDirectory(t)
	orphan := createStage(t, s, "preserve on incomplete inventory")
	for i := 0; i < maxStagingEntries; i++ {
		f, err := os.OpenFile(filepath.Join(path, fmt.Sprintf("other-%05d", i)), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Cleanup(nil); err == nil {
		t.Fatal("oversized staging inventory accepted")
	}
	if data, err := os.ReadFile(filepath.Join(path, orphan)); err != nil || string(data) != "preserve on incomplete inventory" {
		t.Fatal("cleanup mutated files before inventory limit verification", err)
	}
}
