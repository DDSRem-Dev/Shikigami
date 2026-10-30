package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/DDSRem-Dev/Shikigami/pkg/config"
	storage "github.com/DDSRem-Dev/Shikigami/pkg/plugin/state"
)

func TestSyncSnapshotOnlyReadsSelectedRecoveryEvidence(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	h := &helper{root: root, config: filepath.Join(root, "config.yaml")}
	cfg := config.DefaultClient()
	cfg.Agent.StateDir = filepath.Join(root, "state")
	if err := config.SaveClient(h.config, cfg); err != nil {
		t.Fatal(err)
	}
	ledger := filepath.Join(cfg.Agent.StateDir, "plugin-state", "sync", "ledger", "state.json")
	stageDir := filepath.Join(cfg.Agent.StateDir, "plugin-state", "sync", "staging", "map_test")
	for _, dir := range []string{filepath.Dir(ledger), stageDir} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	content := []byte("complete source snapshot")
	if err := os.WriteFile(filepath.Join(stageDir, "transfer-test"), content, 0600); err != nil {
		t.Fatal(err)
	}
	snapshot := storage.Snapshot{SchemaVersion: 2, Revision: 19, Buckets: map[string]map[string]json.RawMessage{"sync_mappings": {}}, Requests: map[string]storage.RequestRecord{"secret": {Fingerprint: "do-not-return"}}}
	record := map[string]any{
		"mapping":    map[string]any{"id": "map_test", "revision": "map-rev", "local_root": "do-not-return"},
		"state":      "active",
		"baselines":  map[string]any{"file.txt": &syncVersion{Exists: true, ContentHash: "old", Revision: "old-rev", Mode: 0600}, "private.txt": &syncVersion{ContentHash: "do-not-return"}},
		"operations": map[string]any{"file.txt": &syncOperation{ID: "fileop_test", Phase: "prepared", CommitState: "possible", SourcePath: "file.txt", TargetPath: "file.txt", SourceSide: "local", TargetSide: "remote", Source: &syncVersion{Exists: true, ContentHash: digest(content)}, Target: &syncVersion{Exists: true, ContentHash: "old"}, Stage: "transfer-test", Size: int64(len(content))}},
	}
	write := func() {
		t.Helper()
		if err := snapshot.Put("sync_mappings", "map_test", record); err != nil {
			t.Fatal(err)
		}
		data, err := json.Marshal(snapshot)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(ledger, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write()
	before, err := os.ReadFile(ledger)
	if err != nil {
		t.Fatal(err)
	}
	request := request{Op: "sync.snapshot", Args: json.RawMessage(`{"mapping_id":"map_test","path":"file.txt"}`)}
	value, err := h.syncSnapshot(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	got := value.(syncSnapshot)
	if got.SchemaVersion != 2 || got.SnapshotRevision != 19 || got.Baseline.ContentHash != "old" || got.Operation.Phase != "prepared" || got.Operation.StageFile.SHA256 != digest(content) || got.Operation.StageFile.Size != int64(len(content)) || !got.Operation.StageFile.Exists {
		t.Fatalf("bad recovery evidence: %+v", got)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte("do-not-return")) {
		t.Fatal("unrelated snapshot fields leaked")
	}
	after, err := os.ReadFile(ledger)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("read-only observer changed ledger")
	}
	entries, err := os.ReadDir(filepath.Dir(ledger))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatal("observer opened competing writer or created recovery files")
	}
	operations := record["operations"].(map[string]any)
	operation := operations["file.txt"].(*syncOperation)
	operation.Stage = "../escape"
	write()
	if _, err := h.syncSnapshot(t.Context(), request); err == nil {
		t.Fatal("accepted stage path traversal")
	}
	operation.Stage = "transfer-link"
	write()
	if err := os.Symlink(ledger, filepath.Join(stageDir, operation.Stage)); err != nil {
		t.Fatal(err)
	}
	if _, err := h.syncSnapshot(t.Context(), request); err == nil {
		t.Fatal("followed symlink stage")
	}
	delete(operations, "file.txt")
	write()
	value, err = h.syncSnapshot(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if value.(syncSnapshot).Operation != nil {
		t.Fatal("settled snapshot still has operation")
	}
}
