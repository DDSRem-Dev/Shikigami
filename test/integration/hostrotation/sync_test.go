package hostrotation_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	"google.golang.org/protobuf/proto"
)

type syncWork struct {
	active, paused           *ppb.Mapping
	remoteRoot, pausedRemote string
	conflict                 *ppb.Conflict
}

func (h *harness) write(root, name string, data []byte) {
	h.t.Helper()
	if err := os.WriteFile(filepath.Join(root, name), data, 0600); err != nil {
		h.t.Fatal(err)
	}
}

func (h *harness) mapping(name string, enabled bool) (*ppb.Mapping, string) {
	h.t.Helper()
	project, err := h.remote.CreateProject(h.ctx, &pb.CreateProjectRequest{Name: name, Path: filepath.Join(h.daemonConfig.ProjectsDir, name), RequestId: h.id()})
	if err != nil {
		h.t.Fatal(err)
	}
	local := filepath.Join(h.root, "files-"+name)
	if err := os.Mkdir(local, 0700); err != nil {
		h.t.Fatal(err)
	}
	for _, root := range []string{local, project.Path} {
		h.write(root, "flow.txt", bytes.Repeat([]byte("common-baseline\n"), 4096))
		h.write(root, "conflict.txt", []byte("common-conflict-base"))
	}
	mapping, err := h.local.PutMapping(h.ctx, &apb.PutMappingRequest{RequestId: h.id(), Mapping: &ppb.Mapping{ServerId: h.serverID, ProjectId: project.Id, LocalRoot: local, SyncEnabled: enabled}})
	if err != nil {
		h.t.Fatal(err)
	}
	return mapping, project.Path
}

func scope(mapping *ppb.Mapping) *ppb.Scope {
	return &ppb.Scope{ServerId: mapping.ServerId, ProjectId: mapping.ProjectId, MappingId: mapping.Id}
}

func (h *harness) mappingState(mapping *ppb.Mapping, want string) {
	h.await("mapping "+want, func(ctx context.Context) error {
		status, err := h.local.GetSyncState(ctx, scope(mapping))
		if err != nil {
			return err
		}
		if status.State != want {
			return fmt.Errorf("%s/%s", status.State, status.ReasonCode)
		}
		return nil
	})
}

func (h *harness) setEnabled(mapping *ppb.Mapping, enabled bool) *ppb.Mapping {
	h.t.Helper()
	next := proto.Clone(mapping).(*ppb.Mapping)
	next.SyncEnabled = enabled
	result, err := h.local.PutMapping(h.ctx, &apb.PutMappingRequest{Mapping: next, ExpectedRevision: mapping.Revision, RequestId: h.id()})
	if err != nil {
		h.t.Fatal(err)
	}
	return result
}

func (h *harness) expectFile(mapping *ppb.Mapping, root, name string, want []byte) {
	h.t.Helper()
	h.await("file hash and common-baseline commit", func(ctx context.Context) error {
		got, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			return err
		}
		if sha256.Sum256(got) != sha256.Sum256(want) {
			return errors.New("file hash has not converged")
		}
		// Worker status is serialized behind reconciliation; destination bytes
		// alone precede the remote ACK and common-baseline commit.
		status, err := h.local.GetSyncState(ctx, scope(mapping))
		if err != nil {
			return err
		}
		if status.State != "active" {
			return fmt.Errorf("sync state %s/%s", status.State, status.ReasonCode)
		}
		return nil
	})
}

func (h *harness) prepareSync() *syncWork {
	w := new(syncWork)
	w.active, w.remoteRoot = h.mapping("active", true)
	h.mappingState(w.active, "active")
	before := bytes.Repeat([]byte("acknowledged-before-expiry\n"), 4096)
	h.write(w.active.LocalRoot, "flow.txt", before)
	h.expectFile(w.active, w.remoteRoot, "flow.txt", before)
	w.active = h.setEnabled(w.active, false)
	h.mappingState(w.active, "paused")
	h.write(w.active.LocalRoot, "conflict.txt", []byte("local-retained-conflict"))
	h.write(w.remoteRoot, "conflict.txt", []byte("remote-retained-conflict"))
	w.active = h.setEnabled(w.active, true)
	h.await("durable conflict", func(ctx context.Context) error {
		list, err := h.local.ListConflicts(ctx, scope(w.active))
		if err != nil {
			return err
		}
		if len(list.Conflicts) != 1 || list.Conflicts[0].Path != "conflict.txt" {
			return errors.New("conflict not observed")
		}
		w.conflict = proto.Clone(list.Conflicts[0]).(*ppb.Conflict)
		return nil
	})
	w.paused, w.pausedRemote = h.mapping("paused", true)
	h.mappingState(w.paused, "active")
	w.paused = h.setEnabled(w.paused, false)
	h.mappingState(w.paused, "paused")
	h.write(w.paused.LocalRoot, "flow.txt", []byte("paused-local-must-not-upload"))
	return w
}

func (h *harness) verifySync(w *syncWork) {
	h.t.Helper()
	h.mappingState(w.active, "conflicted")
	conflicts, err := h.local.ListConflicts(h.ctx, scope(w.active))
	if err != nil || len(conflicts.GetConflicts()) != 1 || !proto.Equal(conflicts.Conflicts[0], w.conflict) {
		h.t.Fatal("rotation changed conflict review state", err)
	}
	for path, want := range map[string]string{filepath.Join(w.active.LocalRoot, "conflict.txt"): "local-retained-conflict", filepath.Join(w.remoteRoot, "conflict.txt"): "remote-retained-conflict"} {
		data, err := os.ReadFile(path)
		if err != nil || string(data) != want {
			h.t.Fatal("rotation overwrote conflict", err)
		}
	}
	h.mappingState(w.paused, "paused")
	paused, err := os.ReadFile(filepath.Join(w.pausedRemote, "flow.txt"))
	if err != nil || !bytes.Equal(paused, bytes.Repeat([]byte("common-baseline\n"), 4096)) {
		h.t.Fatal("paused mapping resumed", err)
	}
	if _, err := h.local.ResolveConflict(h.ctx, &ppb.ResolveConflictRequest{MappingId: w.active.Id, ConflictId: w.conflict.Id, ExpectedGeneration: w.conflict.Generation, Choice: ppb.ResolveConflictRequest_KEEP_LOCAL, RequestId: h.id()}); err != nil {
		h.t.Fatal("explicit conflict resolution", err)
	}
	h.expectFile(w.active, w.remoteRoot, "conflict.txt", []byte("local-retained-conflict"))
	upload := bytes.Repeat([]byte("fresh-host-rotation-upload\n"), 8192)
	h.write(w.active.LocalRoot, "flow.txt", upload)
	h.expectFile(w.active, w.remoteRoot, "flow.txt", upload)
	download := bytes.Repeat([]byte("fresh-host-rotation-download\n"), 8192)
	h.write(w.remoteRoot, "flow.txt", download)
	h.expectFile(w.active, w.active.LocalRoot, "flow.txt", download)
	h.t.Logf("%s rotation: conflict_generation_preserved=true paused_preserved=true upload_bytes=%d download_bytes=%d hashes_verified=true", h.side, len(upload), len(download))
}
