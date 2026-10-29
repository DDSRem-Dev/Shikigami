package state

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func migrationOptions() Options {
	return Options{Migrations: map[int]func(*Snapshot) error{0: func(s *Snapshot) error {
		s.SchemaVersion = 1
		return s.Put("migration", "marker", map[string]bool{"migrated": true})
	}}}
}

func TestMigrationKeepsExactOldBytesBeyondNextCommit(t *testing.T) {
	for _, source := range []string{"state.json", "state.previous.json"} {
		t.Run(source, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "state")
			if err := EnsurePrivateDir(dir); err != nil {
				t.Fatal(err)
			}
			original := []byte("{\n  \"schema_version\": 0, \"revision\": 7,\n  \"buckets\": {\"old\": {\"record\": {\"value\": \"retain spacing and data\"}}},\n  \"requests\": {}\n}\n")
			if err := AtomicWrite(dir, source, original); err != nil {
				t.Fatal(err)
			}
			if source == "state.previous.json" {
				if err := AtomicWrite(dir, "state.json", []byte("broken primary")); err != nil {
					t.Fatal(err)
				}
			}
			s := openTest(t, dir, migrationOptions())
			if source == "state.json" {
				current, _ := os.ReadFile(filepath.Join(dir, source))
				if !bytes.Equal(current, original) {
					t.Fatal("Open unnecessarily rewrote the original schema")
				}
			}
			if err := s.Update(func(snapshot *Snapshot) error { return snapshot.Put("new", "record", map[string]int{"version": 1}) }); err != nil {
				t.Fatal(err)
			}
			backups, err := filepath.Glob(filepath.Join(dir, source+".pre-migration-v0-*.json"))
			if err != nil || len(backups) != 1 {
				t.Fatalf("missing independent rollback copy: %v %v", backups, err)
			}
			backup, err := ReadPrivateFile(backups[0])
			if err != nil || !bytes.Equal(backup, original) {
				t.Fatal("migration rollback copy lost original bytes")
			}
			info, _ := os.Stat(backups[0])
			if info.Mode().Perm() != 0600 {
				t.Fatal("rollback copy is not private")
			}
			if source == "state.previous.json" {
				corrupt, _ := filepath.Glob(filepath.Join(dir, "state.corrupt.*.json"))
				if len(corrupt) != 1 {
					t.Fatal("corrupt primary not preserved")
				}
				data, _ := os.ReadFile(corrupt[0])
				if string(data) != "broken primary" {
					t.Fatal("corrupt evidence modified")
				}
			}
		})
	}
}

func TestMigrationBackupFailureDoesNotReplaceValidPrimaryWithPrevious(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	if err := EnsurePrivateDir(dir); err != nil {
		t.Fatal(err)
	}
	original := []byte(`{"schema_version":0,"revision":9,"buckets":{},"requests":{}}`)
	if err := AtomicWrite(dir, "state.json", original); err != nil {
		t.Fatal(err)
	}
	if err := AtomicWrite(dir, "state.previous.json", []byte(`{"schema_version":1,"revision":1,"buckets":{},"requests":{}}`)); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(original)
	blocked := filepath.Join(dir, fmt.Sprintf("state.json.pre-migration-v0-%x.json", hash))
	if err := os.Mkdir(blocked, 0700); err != nil {
		t.Fatal(err)
	}
	if s, err := Open(dir, migrationOptions()); !errors.Is(err, ErrMigrationBackup) {
		if s != nil {
			s.Close()
		}
		t.Fatalf("want migration backup failure, got %v", err)
	}
	current, _ := os.ReadFile(filepath.Join(dir, "state.json"))
	if !bytes.Equal(current, original) {
		t.Fatal("valid old primary was overwritten after backup failure")
	}
	corrupt, _ := filepath.Glob(filepath.Join(dir, "state.corrupt.*.json"))
	if len(corrupt) != 0 {
		t.Fatal("valid old primary was misclassified as corrupt")
	}
}

func TestInvalidMigrationAndFutureSchemaCreateNoRollbackBackup(t *testing.T) {
	for _, future := range []bool{false, true} {
		t.Run(fmt.Sprint(future), func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "state")
			if err := EnsurePrivateDir(dir); err != nil {
				t.Fatal(err)
			}
			version := 0
			if future {
				version = 999
			}
			original := []byte(fmt.Sprintf(`{"schema_version":%d,"revision":1,"buckets":{},"requests":{}}`, version))
			if err := AtomicWrite(dir, "state.json", original); err != nil {
				t.Fatal(err)
			}
			opts := migrationOptions()
			opts.Validate = func(*Snapshot) error { return errors.New("application invariants failed") }
			if s, err := Open(dir, opts); err == nil {
				s.Close()
				t.Fatal("invalid state opened")
			}
			backups, _ := filepath.Glob(filepath.Join(dir, "*.pre-migration-*.json"))
			if len(backups) != 0 {
				t.Fatal("unvalidated migration was backed up as usable")
			}
			current, _ := os.ReadFile(filepath.Join(dir, "state.json"))
			if !bytes.Equal(current, original) {
				t.Fatal("invalid or future state modified")
			}
		})
	}
}

func TestSerializedStateSizeBoundary(t *testing.T) {
	state := &Snapshot{SchemaVersion: SchemaVersion, Buckets: map[string]map[string]json.RawMessage{}, Requests: map[string]RequestRecord{}}
	if err := state.Put("objects", "one", map[string]string{"content": "small fixture tests the real serialized limit"}); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := marshalSnapshot(state, len(encoded)); err != nil || !bytes.Equal(got, encoded) {
		t.Fatal("exact byte limit was rejected")
	}
	if _, err := marshalSnapshot(state, len(encoded)-1); !errors.Is(err, ErrStateTooLarge) {
		t.Fatalf("oversized snapshot accepted: %v", err)
	}
}
