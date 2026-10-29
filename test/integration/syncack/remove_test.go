package syncack_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"connectrpc.com/connect/v2"
	storage "github.com/DDSRem-Dev/Shikigami/pkg/plugin/state"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	"github.com/DDSRem-Dev/Shikigami/test/fixtures/resources"
	"google.golang.org/protobuf/proto"
)

func TestRemoveMappingRequiresExplicitPendingArchive(t *testing.T) {
	root, binaries := ackArtifacts(t)
	for index, scenario := range []string{"prepared", "legacy", "clean"} {
		t.Run(scenario, func(t *testing.T) {
			var gate *ackGate
			h := startHosts(t, filepath.Join(root, fmt.Sprint(index)), binaries, func(_ context.Context, target, cert, key string) string {
				gate = newACKGate(t, target, cert, key)
				return strings.TrimPrefix(gate.server.URL, "https://")
			})
			for _, side := range []string{"server", "client"} {
				h.manage(side, "sync", pb.ManagePluginRequest_ENABLE, nil)
				h.ready(side)
				h.workerPID(side)
			}
			mapping, remote := h.mapping()
			h.awaitBaseline(mapping, commonContents)
			payload := commonContents
			var prepared *journalOperation
			if scenario == "prepared" {
				payload = bytes.Repeat([]byte("committed without ACK\n"), 20000)
				gate.armed.Store(true)
				h.write(mapping.LocalRoot, payload)
				select {
				case commit := <-gate.committed:
					if commit.err != nil {
						t.Fatal(commit.err)
					}
				case <-h.ctx.Done():
					t.Fatal("commit gate not reached", h.ctx.Err())
				}
				prepared = h.assertPrepared(mapping, payload)
				gate.releaseACK()
				select {
				case <-gate.dropped:
				case <-h.ctx.Done():
					t.Fatal("ACK not dropped", h.ctx.Err())
				}
			}
			oldRevision := mapping.Revision
			paused := proto.Clone(mapping).(*ppb.Mapping)
			paused.SyncEnabled = false
			var err error
			mapping, err = h.local.PutMapping(h.ctx, &apb.PutMappingRequest{Mapping: paused, ExpectedRevision: oldRevision, RequestId: h.id()})
			if err != nil {
				t.Fatal("pause mapping", err)
			}
			h.state(mapping, "paused")
			ledger := filepath.Join(h.agentConfig.Agent.StateDir, "plugin-state", "sync", "ledger", "state.json")
			if scenario == "legacy" {
				old := h.workerPID("client")
				h.manage("client", "sync", pb.ManagePluginRequest_DISABLE, nil)
				resources.Reaped(t, old)
				data, err := os.ReadFile(ledger)
				if err != nil {
					t.Fatal(err)
				}
				var state storage.Snapshot
				if err := json.Unmarshal(data, &state); err != nil {
					t.Fatal(err)
				}
				state.SchemaVersion = 1
				var fields map[string]json.RawMessage
				if err := json.Unmarshal(state.Buckets["sync_mappings"][mapping.Id], &fields); err != nil {
					t.Fatal(err)
				}
				delete(fields, "operations")
				delete(fields, "legacy_unverified")
				if err := state.Put("sync_mappings", mapping.Id, fields); err != nil {
					t.Fatal(err)
				}
				data, err = json.Marshal(state)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(ledger, data, 0600); err != nil {
					t.Fatal(err)
				}
				h.manage("client", "sync", pb.ManagePluginRequest_ENABLE, nil)
				h.ready("client")
				h.workerPID("client")
				h.state(mapping, "paused")
			}
			conflicts, err := h.local.ListConflicts(h.ctx, scope(mapping))
			if err != nil || len(conflicts.GetConflicts()) != 0 {
				t.Fatal("fixture must have no reported conflicts", err)
			}
			request := &apb.RemoveMappingRequest{MappingId: mapping.Id, ExpectedRevision: mapping.Revision, RequestId: h.id()}
			if scenario != "clean" {
				if _, err := h.local.RemoveMapping(h.ctx, request); connect.CodeOf(err) != connect.CodeFailedPrecondition {
					t.Fatal("mapping with unresolved journal was removed without archive_pending", err)
				}
				status, err := h.local.GetSyncState(h.ctx, scope(mapping))
				if err != nil || status.PendingFiles != 1 || status.ConflictCount != 0 || status.State != "paused" || status.MappingRevision != mapping.Revision {
					t.Fatal("pending journal not visible through paused worker status", status, err)
				}
				request.ArchivePending = true
				request.RequestId = h.id()
			} else {
				stale := proto.Clone(request).(*apb.RemoveMappingRequest)
				stale.ExpectedRevision = oldRevision
				stale.RequestId = h.id()
				if _, err := h.local.RemoveMapping(h.ctx, stale); connect.CodeOf(err) != connect.CodeAborted {
					t.Fatal("stale mapping revision was accepted", err)
				}
			}
			if _, err := h.local.RemoveMapping(h.ctx, request); err != nil {
				t.Fatal("authorized removal", err)
			}
			if _, err := h.local.RemoveMapping(h.ctx, request); err != nil {
				t.Fatal("completed removal replay", err)
			}
			reused := proto.Clone(request).(*apb.RemoveMappingRequest)
			reused.ArchivePending = !request.ArchivePending
			if _, err := h.local.RemoveMapping(h.ctx, reused); connect.CodeOf(err) != connect.CodeInvalidArgument {
				t.Fatal("completed removal request ID reused with different parameters", err)
			}
			data, err := os.ReadFile(ledger)
			if err != nil {
				t.Fatal(err)
			}
			var state storage.Snapshot
			if err := json.Unmarshal(data, &state); err != nil {
				t.Fatal(err)
			}
			var archived struct {
				Operations       map[string]*journalOperation `json:"operations"`
				LegacyUnverified map[string]bool              `json:"legacy_unverified"`
			}
			if err := state.Get("sync_archived_mappings", mapping.Id, &archived); err != nil {
				t.Fatal("worker did not preserve archive", err)
			}
			if scenario == "prepared" {
				op := archived.Operations["flow.txt"]
				if op == nil || op.ID != prepared.ID || op.Stage != prepared.Stage {
					t.Fatal("archive lost uncertain operation")
				}
				stage, err := os.ReadFile(filepath.Join(h.agentConfig.Agent.StateDir, "plugin-state", "sync", "staging", mapping.Id, op.Stage))
				if err != nil || !bytes.Equal(stage, payload) {
					t.Fatal("archive lost source stage", err)
				}
			} else if scenario == "legacy" && !archived.LegacyUnverified["flow.txt"] {
				t.Fatal("archive lost legacy review marker")
			}
			h.assertFile(mapping.LocalRoot, payload)
			h.assertFile(remote, payload)
			t.Logf("scenario=%s zero_conflicts=true explicit_archive_required=%v completed_replay=true files_and_journal_preserved=true", scenario, scenario != "clean")
		})
	}
}
