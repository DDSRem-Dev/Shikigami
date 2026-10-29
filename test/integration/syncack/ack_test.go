package syncack_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
	"github.com/DDSRem-Dev/Shikigami/test/fixtures/resources"
	"google.golang.org/protobuf/proto"
)

func TestCommittedUploadLostACK(t *testing.T) {
	root, binaries := ackArtifacts(t)
	for index, name := range []string{"equal_after_commit", "remote_edited_before_recovery", "remote_reverted_to_baseline", "local_reverted_to_baseline"} {
		edited := name != "equal_after_commit"
		t.Run(name, func(t *testing.T) {
			var gate *ackGate
			// Keep runtime roots short enough for both CoreAPI and go-plugin's
			// generated native Unix-socket names on macOS.
			h := startHosts(t, filepath.Join(root, fmt.Sprint(index)), binaries, func(_ context.Context, target, cert, key string) string {
				gate = newACKGate(t, target, cert, key)
				return strings.TrimPrefix(gate.server.URL, "https://")
			})
			for _, side := range []string{"server", "client"} {
				h.manage(side, "sync", pb.ManagePluginRequest_ENABLE, nil)
				h.ready(side)
			}
			oldClient, serverPID := h.workerPID("client"), h.workerPID("server")
			mapping, remote := h.mapping()
			before := h.awaitBaseline(mapping, commonContents)
			payload := make([]byte, 768<<10)
			if _, err := rand.Read(payload); err != nil {
				t.Fatal(err)
			}
			gate.armed.Store(true)
			h.write(mapping.LocalRoot, payload)
			var committed commitObservation
			select {
			case committed = <-gate.committed:
			case <-h.ctx.Done():
				t.Fatal("no intercepted upstream commit", h.ctx.Err())
			}
			if committed.err != nil {
				t.Fatal("server did not produce a successful commit response", committed.err)
			}
			info := committed.info
			if info.Path != "flow.txt" || info.ContentHash != hash(payload) || info.Size != int64(len(payload)) || info.Mode != 0600 || info.Revision == before.Revision {
				t.Fatal("commit ACK does not describe expected new contents", info)
			}
			h.assertFile(remote, payload)
			h.assertFile(mapping.LocalRoot, payload)
			h.assertBaseline(mapping, before)
			prepared := h.assertPrepared(mapping, payload)
			if gate.ackBytes.Load() != 0 {
				t.Fatal("committed ACK already reached caller")
			}
			t.Logf("commit held: payload_bytes=%d payload_sha256=%s upstream_ack_bytes=%d downstream_ack_bytes=%d remote_published=true old_baseline_preserved=true", len(payload), hash(payload), committed.wireBytes, gate.ackBytes.Load())
			gate.releaseACK()
			select {
			case <-gate.dropped:
			case <-h.ctx.Done():
				t.Fatal("ACK reset did not complete")
			}
			var failureState, failureReason string
			h.await("worker persisted unknown commit failure", func(context.Context) error {
				record, err := h.record(mapping)
				if err != nil {
					return err
				}
				if record.Reason == "" || record.State == "active" {
					return errors.New("worker has not recorded ACK failure")
				}
				if !proto.Equal(record.Baselines["flow.txt"], before) {
					return errors.New("ACK loss advanced baseline")
				}
				failureState, failureReason = record.State, record.Reason
				return nil
			})
			select {
			case procedure := <-gate.blocked:
				if procedure != rpc.ShikiDaemonListFilesProcedure && procedure != rpc.ShikiDaemonStatFileProcedure {
					t.Fatal("recovery attempted mutation before reobservation", procedure)
				}
			case <-h.ctx.Done():
				t.Fatal("worker did not attempt reobservation")
			}
			localContents, remoteContents := payload, payload
			if name == "local_reverted_to_baseline" {
				localContents = commonContents
				h.write(mapping.LocalRoot, localContents)
			} else if edited {
				remoteContents = bytes.Repeat([]byte("external-editor-after-server-commit\n"), 8192)
				if name == "remote_reverted_to_baseline" {
					remoteContents = commonContents
				}
				h.write(remote, remoteContents)
			}
			h.assertBaseline(mapping, before)
			h.manage("client", "sync", pb.ManagePluginRequest_RESTART, nil)
			h.ready("client")
			resources.Reaped(t, oldClient)
			newClient := h.workerPID("client")
			if newClient == oldClient || h.workerPID("server") != serverPID {
				t.Fatal("wrong worker replaced")
			}
			h.assertBaseline(mapping, before)
			reloaded := h.assertPrepared(mapping, payload)
			if reloaded.ID != prepared.ID || reloaded.Stage != prepared.Stage {
				t.Fatal("restart replaced the uncertain journal")
			}
			h.assertFile(mapping.LocalRoot, localContents)
			h.assertFile(remote, remoteContents)
			if gate.writes.Load() != 1 || gate.ackBytes.Load() != 0 {
				t.Fatal("unexpected write or delivered ACK before recovery")
			}
			gate.releaseRecovery()
			var conflictGeneration string
			if edited {
				conflict := h.awaitConflict(mapping, before, localContents, remoteContents)
				if name != "local_reverted_to_baseline" && conflict.Remote.Revision == info.Revision {
					t.Fatal("conflict retained a stale pre-edit remote revision")
				}
				conflictGeneration = conflict.Generation
				h.readConflict(mapping, conflict, ppb.ReadConflictFileRequest_LOCAL, localContents)
				h.readConflict(mapping, conflict, ppb.ReadConflictFileRequest_REMOTE, remoteContents)
				h.assertBaseline(mapping, before)
			} else {
				after := h.awaitBaseline(mapping, payload)
				if after.Revision != info.Revision {
					t.Fatal("reobserved baseline does not match committed revision")
				}
				h.state(mapping, "active")
				conflicts, err := h.local.ListConflicts(h.ctx, scope(mapping))
				if err != nil || len(conflicts.GetConflicts()) != 0 {
					t.Fatal("equal recovery produced conflict", err)
				}
			}
			h.assertFile(mapping.LocalRoot, localContents)
			h.assertFile(remote, remoteContents)
			gate.mu.Lock()
			operations := append([]string(nil), gate.recovery...)
			gate.mu.Unlock()
			if len(operations) == 0 || (operations[0] != rpc.ShikiDaemonListFilesProcedure && operations[0] != rpc.ShikiDaemonStatFileProcedure) {
				t.Fatal("recovery did not begin with remote observation", operations)
			}
			if gate.writes.Load() != 1 || gate.ackBytes.Load() != 0 {
				t.Fatal("uncertain commit was blindly replayed or ACK forwarded")
			}
			if h.workerPID("client") != newClient || h.workerPID("server") != serverPID {
				t.Fatal("recovery replaced a worker unexpectedly")
			}
			if edited {
				h.assertPrepared(mapping, payload)
				list, err := h.local.ListConflicts(h.ctx, scope(mapping))
				if err != nil || len(list.GetConflicts()) != 1 {
					t.Fatal("retained recovery conflict missing", err)
				}
				c := list.Conflicts[0]
				if _, err := h.local.ResolveConflict(h.ctx, &ppb.ResolveConflictRequest{MappingId: mapping.Id, ConflictId: c.Id, ExpectedGeneration: c.Generation, Choice: ppb.ResolveConflictRequest_KEEP_REMOTE, RequestId: h.id()}); err != nil {
					t.Fatal("explicit recovery choice", err)
				}
				h.awaitBaseline(mapping, remoteContents)
				h.assertFile(mapping.LocalRoot, remoteContents)
			}
			record, err := h.record(mapping)
			if err != nil || len(record.Operations) != 0 {
				t.Fatal("resolved operation journal was not cleared", err)
			}
			h.await("private transfer cleanup", func(context.Context) error { return h.noTransfers(mapping, remote) })
			t.Logf("recovered: edited_remote=%v ack_bytes_delivered=0 write_rpc_count=%d client_restarted=true old_client_reaped=true server_unchanged=true failure_state=%s failure_reason=%s first_recovery_rpc=%s local_sha256=%s remote_sha256=%s conflict_generation=%s", edited, gate.writes.Load(), failureState, failureReason, operations[0], hash(localContents), hash(remoteContents), conflictGeneration)
		})
	}
}

func (h *harness) awaitConflict(mapping *ppb.Mapping, base *ppb.FileVersion, local, remote []byte) *ppb.Conflict {
	h.t.Helper()
	var result *ppb.Conflict
	h.await("reobserved conflict", func(ctx context.Context) error {
		h.assertFile(mapping.LocalRoot, local)
		h.assertFile(filepath.Join(h.daemonConfig.ProjectsDir, "ack"), remote)
		list, err := h.local.ListConflicts(ctx, scope(mapping))
		if err != nil {
			return err
		}
		if len(list.Conflicts) != 1 {
			return fmt.Errorf("conflict count %d", len(list.Conflicts))
		}
		c := list.Conflicts[0]
		if c.Path != "flow.txt" || c.Generation == "" || c.Id == "" || !proto.Equal(c.Base, base) || c.Local.ContentHash != hash(local) || c.Remote.ContentHash != hash(remote) || c.Local.Mode != 0600 || c.Remote.Mode != 0600 {
			return errors.New("conflict does not retain all three actual versions")
		}
		result = proto.Clone(c).(*ppb.Conflict)
		return nil
	})
	h.state(mapping, "conflicted")
	return result
}
func (h *harness) readConflict(mapping *ppb.Mapping, c *ppb.Conflict, side ppb.ReadConflictFileRequest_Side, want []byte) {
	h.t.Helper()
	stream, err := h.local.ReadConflictFile(h.ctx, &ppb.ReadConflictFileRequest{MappingId: mapping.Id, ConflictId: c.Id, ExpectedGeneration: c.Generation, Side: side})
	if err != nil {
		h.t.Fatal(err)
	}
	defer stream.Close()
	first, err := stream.Receive()
	if err != nil || first.GetInfo() == nil || first.GetInfo().ContentHash != hash(want) {
		h.t.Fatal("conflict metadata", err)
	}
	var data []byte
	for {
		frame, err := stream.Receive()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			h.t.Fatal(err)
		}
		data = append(data, frame.GetChunk()...)
	}
	if !bytes.Equal(data, want) {
		h.t.Fatal("conflict review content corrupted")
	}
}
func hash(data []byte) string { return fmt.Sprintf("%x", sha256.Sum256(data)) }

var commonContents = []byte("common durable baseline before uncertain commit\n")

func scope(m *ppb.Mapping) *ppb.Scope {
	return &ppb.Scope{ServerId: m.ServerId, ProjectId: m.ProjectId, MappingId: m.Id}
}
func (h *harness) write(root string, data []byte) {
	h.t.Helper()
	if err := os.WriteFile(filepath.Join(root, "flow.txt"), data, 0600); err != nil {
		h.t.Fatal(err)
	}
}
func (h *harness) assertFile(root string, want []byte) {
	h.t.Helper()
	got, err := os.ReadFile(filepath.Join(root, "flow.txt"))
	if err != nil || !bytes.Equal(got, want) {
		h.t.Fatal("unexpected public file contents", root, err)
	}
}
func (h *harness) mapping() (*ppb.Mapping, string) {
	h.t.Helper()
	p, err := h.remote.CreateProject(h.ctx, &pb.CreateProjectRequest{Name: "ack", Path: filepath.Join(h.daemonConfig.ProjectsDir, "ack"), RequestId: h.id()})
	if err != nil {
		h.t.Fatal(err)
	}
	local := filepath.Join(h.root, "local")
	if err := os.Mkdir(local, 0700); err != nil {
		h.t.Fatal(err)
	}
	h.write(local, commonContents)
	h.write(p.Path, commonContents)
	m, err := h.local.PutMapping(h.ctx, &apb.PutMappingRequest{RequestId: h.id(), Mapping: &ppb.Mapping{ServerId: h.serverID, ProjectId: p.Id, LocalRoot: local, SyncEnabled: true}})
	if err != nil {
		h.t.Fatal(err)
	}
	h.state(m, "active")
	return m, p.Path
}
func (h *harness) state(m *ppb.Mapping, want string) {
	h.await("sync state "+want, func(ctx context.Context) error {
		s, err := h.local.GetSyncState(ctx, scope(m))
		if err != nil {
			return err
		}
		if s.State != want {
			return fmt.Errorf("%s/%s", s.State, s.ReasonCode)
		}
		return nil
	})
}
func (h *harness) workerPID(side string) int {
	h.t.Helper()
	pids := resources.Children(h.t, filepath.Join(h.root, side, "plugins", "sync.race"), 1)
	h.pids = append(h.pids, pids[0])
	return pids[0]
}
