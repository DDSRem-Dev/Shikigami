package state

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestServiceSchemaMigrationAndOldWriterRejection(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "ledger")
	old := openTest(t, dir, Options{})
	if err := old.Update(func(s *Snapshot) error { return s.Put("records", "one", map[string]string{"value": "retained"}) }); err != nil {
		t.Fatal(err)
	}
	old.Close()
	original, err := os.ReadFile(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	opts := Options{SchemaVersion: 2, DisableRecovery: true, Migrations: map[int]func(*Snapshot) error{1: func(s *Snapshot) error {
		s.SchemaVersion = 2
		return s.Put("migration", "v2", map[string]bool{"done": true})
	}}}
	upgraded := openTest(t, dir, opts)
	backups, err := filepath.Glob(filepath.Join(dir, "state.json.pre-migration-v1-*.json"))
	if err != nil || len(backups) != 1 {
		t.Fatal("missing exact migration backup", backups, err)
	}
	backup, err := os.ReadFile(backups[0])
	if err != nil || !bytes.Equal(original, backup) {
		t.Fatal("migration source changed", err)
	}
	if err := upgraded.Update(func(s *Snapshot) error {
		if s.SchemaVersion != 2 {
			return errors.New("target schema not loaded")
		}
		s.SchemaVersion = 1
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	upgraded.Close()
	current, err := os.ReadFile(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var state Snapshot
	if err := json.Unmarshal(current, &state); err != nil || state.SchemaVersion != 2 {
		t.Fatal("update downgraded service schema", err)
	}
	if _, err := Open(dir, Options{}); !errors.Is(err, ErrFutureSchema) {
		t.Fatal("default old writer admitted new ledger", err)
	}
	after, _ := os.ReadFile(filepath.Join(dir, "state.json"))
	if !bytes.Equal(after, current) {
		t.Fatal("old writer rewrote future schema")
	}
	reopened := openTest(t, dir, Options{SchemaVersion: 2, DisableRecovery: true})
	snapshot, _ := reopened.View()
	if snapshot.Buckets["records"]["one"] == nil || snapshot.Buckets["migration"]["v2"] == nil {
		t.Fatal("migration lost records")
	}
}

func TestServiceSchemaTargetFailsBeforeCreatingState(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "negative")
	if _, err := Open(dir, Options{SchemaVersion: -1}); err == nil {
		t.Fatal("negative target accepted")
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("invalid option created state", err)
	}
	for _, test := range []struct {
		name    string
		migrate func(*Snapshot) error
	}{
		{"missing", nil}, {"skips-version", func(s *Snapshot) error { s.SchemaVersion = 3; return nil }},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "state")
			old := openTest(t, dir, Options{})
			old.Close()
			before, _ := os.ReadFile(filepath.Join(dir, "state.json"))
			_, err := Open(dir, Options{SchemaVersion: 2, DisableRecovery: true, Migrations: map[int]func(*Snapshot) error{1: test.migrate}})
			if err == nil {
				t.Fatal("invalid migration admitted")
			}
			after, _ := os.ReadFile(filepath.Join(dir, "state.json"))
			if !bytes.Equal(before, after) {
				t.Fatal("failed migration rewrote original")
			}
		})
	}
}

func TestFutureSchemaWithNewFieldsNeverRecoversOldBackup(t *testing.T) {
	for _, disableRecovery := range []bool{false, true} {
		t.Run(fmt.Sprint(disableRecovery), func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "state")
			old := openTest(t, dir, Options{})
			if err := old.Update(func(s *Snapshot) error { return s.Put("records", "old", map[string]bool{"retained": true}) }); err != nil {
				t.Fatal(err)
			}
			old.Close()
			primary := filepath.Join(dir, "state.json")
			future := []byte(`{"schema_version":3,"revision":99,"buckets":{},"requests":{},"new_contract":{"required":true}}`)
			if err := os.WriteFile(primary, future, 0600); err != nil {
				t.Fatal(err)
			}
			backup, err := os.ReadFile(filepath.Join(dir, "state.previous.json"))
			if err != nil {
				t.Fatal(err)
			}
			opened, err := Open(dir, Options{DisableRecovery: disableRecovery})
			if opened != nil {
				opened.Close()
			}
			if !errors.Is(err, ErrFutureSchema) {
				t.Errorf("future state must refuse recovery: %v", err)
			}
			after, readErr := os.ReadFile(primary)
			if readErr != nil || !bytes.Equal(after, future) {
				t.Error("future primary changed", readErr)
			}
			afterBackup, readErr := os.ReadFile(filepath.Join(dir, "state.previous.json"))
			if readErr != nil || !bytes.Equal(afterBackup, backup) {
				t.Error("old backup changed", readErr)
			}
			preserved, err := filepath.Glob(filepath.Join(dir, "state.corrupt.*"))
			if err != nil || len(preserved) != 0 {
				t.Error("future primary misclassified as corrupt", preserved, err)
			}
		})
	}
}
