package agent

import (
	"testing"

	"connectrpc.com/connect/v2"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	"google.golang.org/protobuf/proto"
)

func TestMappingRemovalRequiresCurrentPausedWorkerEvidence(t *testing.T) {
	mapping := &ppb.Mapping{Id: "map_current", Revision: "rev_paused"}
	clean := &ppb.SyncState{MappingId: mapping.Id, MappingRevision: mapping.Revision, State: "paused"}
	for _, scenario := range []struct {
		name   string
		change func(*ppb.SyncState) *ppb.SyncState
	}{
		{"missing", func(*ppb.SyncState) *ppb.SyncState { return nil }},
		{"different-mapping", func(s *ppb.SyncState) *ppb.SyncState { s.MappingId = "map_other"; return s }},
		{"stale-revision", func(s *ppb.SyncState) *ppb.SyncState { s.MappingRevision = "rev_prior"; return s }},
		{"not-settled", func(s *ppb.SyncState) *ppb.SyncState { s.State = "active"; return s }},
		{"unresolved-journal-without-conflict", func(s *ppb.SyncState) *ppb.SyncState { s.PendingFiles = 1; return s }},
		{"conflict-even-with-zero-pending", func(s *ppb.SyncState) *ppb.SyncState { s.ConflictCount = 1; return s }},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			state := scenario.change(proto.Clone(clean).(*ppb.SyncState))
			if err := checkMappingRemovalState(mapping, state); connect.CodeOf(err) != connect.CodeFailedPrecondition {
				t.Fatal("unverified or pending worker state authorized removal", err)
			}
		})
	}
	if err := checkMappingRemovalState(mapping, clean); err != nil {
		t.Fatal("current clean paused worker rejected", err)
	}
}
