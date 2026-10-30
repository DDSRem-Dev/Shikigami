package main

import (
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/DDSRem-Dev/Shikigami/pkg/config"
	storage "github.com/DDSRem-Dev/Shikigami/pkg/plugin/state"
)

// These views intentionally include only recovery evidence for one path. Never
// return the raw snapshot, request cache, other mappings, or credential files.
type syncVersion struct {
	Exists      bool   `json:"exists"`
	ContentHash string `json:"content_hash"`
	Mode        uint32 `json:"mode"`
	Revision    string `json:"revision"`
}
type syncOperation struct {
	ID          string       `json:"id"`
	Phase       string       `json:"phase"`
	CommitState string       `json:"commit_state"`
	SourcePath  string       `json:"source_path"`
	TargetPath  string       `json:"target_path"`
	SourceSide  string       `json:"source_side"`
	TargetSide  string       `json:"target_side"`
	Source      *syncVersion `json:"source"`
	Target      *syncVersion `json:"target"`
	Result      *syncVersion `json:"result"`
	Size        int64        `json:"size"`
	Stage       string       `json:"stage"`
	StageFile   fileInfo     `json:"stage_file"`
}
type syncSnapshot struct {
	SchemaVersion    int            `json:"schema_version"`
	SnapshotRevision uint64         `json:"snapshot_revision"`
	MappingID        string         `json:"mapping_id"`
	MappingRevision  string         `json:"mapping_revision"`
	State            string         `json:"state"`
	Path             string         `json:"path"`
	Baseline         *syncVersion   `json:"baseline"`
	Operation        *syncOperation `json:"operation"`
}

func (h *helper) syncSnapshot(ctx context.Context, r request) (any, error) {
	a, err := args[struct {
		MappingID string `json:"mapping_id"`
		Path      string `json:"path"`
	}](r)
	if err != nil || !strings.HasPrefix(a.MappingID, "map_") || filepath.Base(a.MappingID) != a.MappingID || !fs.ValidPath(a.Path) || a.Path == "." {
		return nil, invalid()
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	cfg, err := config.LoadClient(h.config)
	if err != nil {
		return nil, err
	}
	ledger, err := h.path(filepath.Join(cfg.Agent.StateDir, "plugin-state", "sync", "ledger", "state.json"))
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(h.root)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	f, err := root.OpenFile(ledger, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	const maxSnapshot = 16 << 20
	if !info.Mode().IsRegular() || info.Size() > maxSnapshot {
		return nil, invalid()
	}
	data, err := io.ReadAll(io.LimitReader(f, maxSnapshot+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxSnapshot {
		return nil, invalid()
	}
	var snapshot storage.Snapshot
	if err = json.Unmarshal(data, &snapshot); err != nil {
		return nil, err
	}
	if snapshot.SchemaVersion != 2 {
		return nil, invalid()
	}
	var record struct {
		Mapping struct {
			ID       string `json:"id"`
			Revision string `json:"revision"`
		} `json:"mapping"`
		State      string                    `json:"state"`
		Baselines  map[string]*syncVersion   `json:"baselines"`
		Operations map[string]*syncOperation `json:"operations"`
	}
	if err = snapshot.Get("sync_mappings", a.MappingID, &record); err != nil {
		return nil, err
	}
	if record.Mapping.ID != a.MappingID {
		return nil, invalid()
	}
	result := syncSnapshot{SchemaVersion: snapshot.SchemaVersion, SnapshotRevision: snapshot.Revision, MappingID: a.MappingID, MappingRevision: record.Mapping.Revision, State: record.State, Path: a.Path, Baseline: record.Baselines[a.Path], Operation: record.Operations[a.Path]}
	if op := result.Operation; op != nil && op.Stage != "" {
		if filepath.Base(op.Stage) != op.Stage || !strings.HasPrefix(op.Stage, "transfer-") {
			return nil, invalid()
		}
		stage, err := h.path(filepath.Join(cfg.Agent.StateDir, "plugin-state", "sync", "staging", a.MappingID, op.Stage))
		if err != nil {
			return nil, err
		}
		op.StageFile, err = statFile(root, stage)
		if err != nil {
			return nil, err
		}
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	return result, nil
}
