package syncack_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	storage "github.com/DDSRem-Dev/Shikigami/pkg/plugin/state"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	"google.golang.org/protobuf/proto"
)

type journalOperation struct {
	CommitState string           `json:"commit_state"`
	ID          string           `json:"id"`
	Phase       string           `json:"phase"`
	Stage       string           `json:"stage"`
	Size        int64            `json:"size"`
	Source      *ppb.FileVersion `json:"source"`
	Target      *ppb.FileVersion `json:"target"`
	Result      *ppb.FileVersion `json:"result"`
}

type workerRecord struct {
	Operations map[string]*journalOperation `json:"operations"`
	Mapping    *ppb.Mapping                 `json:"mapping"`
	Baselines  map[string]*ppb.FileVersion  `json:"baselines"`
	Conflicts  map[string]*ppb.Conflict     `json:"conflicts"`
	State      string                       `json:"state"`
	Reason     string                       `json:"reason"`
}

// Read only the atomically published snapshot; never open a competing writer.
func (h *harness) record(mapping *ppb.Mapping) (workerRecord, error) {
	var result workerRecord
	data, err := os.ReadFile(filepath.Join(h.agentConfig.Agent.StateDir, "plugin-state", "sync", "ledger", "state.json"))
	if err != nil {
		return result, err
	}
	var s storage.Snapshot
	if err = json.Unmarshal(data, &s); err != nil {
		return result, err
	}
	if err = s.Get("sync_mappings", mapping.Id, &result); err != nil {
		return result, err
	}
	if !proto.Equal(mapping, result.Mapping) {
		return result, errors.New("persisted mapping intent changed")
	}
	return result, nil
}
func (h *harness) awaitBaseline(mapping *ppb.Mapping, content []byte) *ppb.FileVersion {
	h.t.Helper()
	var base *ppb.FileVersion
	h.await("persisted common baseline", func(context.Context) error {
		r, err := h.record(mapping)
		if err != nil {
			return err
		}
		b := r.Baselines["flow.txt"]
		if b == nil || !b.Exists || b.ContentHash != hash(content) || b.Mode != 0600 || b.Revision == "" {
			return errors.New("baseline not yet established")
		}
		base = proto.Clone(b).(*ppb.FileVersion)
		return nil
	})
	return base
}
func (h *harness) assertBaseline(mapping *ppb.Mapping, want *ppb.FileVersion) {
	h.t.Helper()
	r, err := h.record(mapping)
	if err != nil || !proto.Equal(r.Baselines["flow.txt"], want) {
		h.t.Fatal("uncertain commit changed the durable baseline", err)
	}
}
func (h *harness) noTransfers(mapping *ppb.Mapping, remote string) error {
	roots := map[string]string{remote: ".shiki-sync-transfer-", filepath.Join(h.agentConfig.Agent.StateDir, "plugin-state", "sync", "staging", mapping.Id): "transfer-"}
	for root, prefix := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), prefix) {
				return fmt.Errorf("private transfer remains %s", entry.Name())
			}
		}
	}
	return nil
}

func (h *harness) assertPrepared(mapping *ppb.Mapping, payload []byte) *journalOperation {
	h.t.Helper()
	r, err := h.record(mapping)
	if err != nil {
		h.t.Fatal(err)
	}
	op := r.Operations["flow.txt"]
	if len(r.Operations) != 1 || op == nil || op.ID == "" || op.Phase != "prepared" || op.CommitState != "possible" || op.Stage == "" || op.Result != nil || op.Size != int64(len(payload)) || op.Source.ContentHash != hash(payload) {
		h.t.Fatal("prepared operation missing or changed")
	}
	data, err := os.ReadFile(filepath.Join(h.agentConfig.Agent.StateDir, "plugin-state", "sync", "staging", mapping.Id, op.Stage))
	if err != nil || hash(data) != hash(payload) {
		h.t.Fatal("uncertain source snapshot was not retained", err)
	}
	return op
}
