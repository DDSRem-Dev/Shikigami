package client

import (
	"context"
	"encoding/json"
	"errors"
	"path"
	"strings"

	"connectrpc.com/connect/v2"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	storage "github.com/DDSRem-Dev/Shikigami/pkg/plugin/state"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	"google.golang.org/protobuf/proto"
)

func (w *Worker) ResolveConflict(ctx context.Context, in *ppb.ResolveConflictRequest) (*ppb.SyncState, error) {
	if in == nil || in.RequestId == "" || len(in.RequestId) > 128 || strings.ContainsAny(in.RequestId, "\r\n\x00") {
		return nil, fail(connect.CodeInvalidArgument, "REQUEST_ID_REQUIRED")
	}
	if in.Choice != ppb.ResolveConflictRequest_KEEP_LOCAL && in.Choice != ppb.ResolveConflictRequest_KEEP_REMOTE && in.Choice != ppb.ResolveConflictRequest_KEEP_BOTH {
		return nil, fail(connect.CodeInvalidArgument, "CONFLICT_CHOICE_REQUIRED")
	}
	t, err := w.get(in.MappingId)
	if err != nil {
		return nil, err
	}
	ctx, cancel := t.operationContext(ctx, true)
	defer cancel()
	t.op.Lock()
	defer t.op.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	namespace := "resolve/" + in.MappingId
	parameters, err := proto.MarshalOptions{Deterministic: true}.Marshal(in)
	if err != nil {
		return nil, err
	}
	snapshot, err := w.store.View()
	if err != nil {
		return nil, err
	}
	previous, replay, err := snapshot.Request(namespace, in.RequestId, parameters)
	if errors.Is(err, storage.ErrRequestMismatch) {
		return nil, fail(connect.CodeInvalidArgument, "REQUEST_ID_REUSED")
	}
	if err != nil {
		return nil, err
	}
	if replay {
		if previous.Status != "succeeded" {
			return nil, fail(connect.CodeAborted, "RESULT_UNKNOWN_RECONCILE_REQUIRED")
		}
		out := new(ppb.SyncState)
		if err = json.Unmarshal(previous.Result, out); err != nil {
			return nil, err
		}
		return out, nil
	}
	if !t.data.Mapping.SyncEnabled {
		return nil, fail(connect.CodeFailedPrecondition, "MAPPING_PAUSED")
	}
	conflict, err := t.validateConflict(ctx, in.ConflictId, in.ExpectedGeneration)
	if err != nil {
		return nil, err
	}
	copyPath := ""
	if in.Choice == ppb.ResolveConflictRequest_KEEP_BOTH && conflict.Remote.Exists {
		copyPath = path.Join(path.Dir(conflict.Path), path.Base(conflict.Path)+".conflict-"+id(""))
		if len(path.Base(copyPath)) > 240 {
			return nil, fail(connect.CodeInvalidArgument, "CONFLICT_COPY_NAME_TOO_LONG")
		}
	}
	// Commit an intent before touching either filesystem. A process death leaves
	// an uncertain result that is rescanned, never a blindly replayed mutation.
	err = w.update(func(snapshot *storage.Snapshot) error {
		if len(snapshot.Requests) >= 10000 {
			return fail(connect.CodeResourceExhausted, "REQUEST_HISTORY_LIMIT")
		}
		if _, _, err := snapshot.Request(namespace, in.RequestId, parameters); err != nil {
			return err
		}
		if copyPath != "" {
			t.data.Copies[copyPath] = conflict.Path
		}
		return snapshot.Put(mappingsBucket, in.MappingId, &t.data)
	})
	if err != nil {
		return nil, err
	}
	if copyPath != "" {
		empty := &ppb.FileVersion{}
		err = t.transfer(ctx, t.remote, t.local, conflict.Path, copyPath, conflict.Remote, empty)
		if err == nil {
			err = t.transfer(ctx, t.remote, t.remote, conflict.Path, copyPath, conflict.Remote, empty)
		}
		if err == nil {
			err = t.establish(ctx, copyPath)
		}
	}
	if err == nil {
		source, destination := sdk.FileService(t.local), t.remote
		observed, target := conflict.Local, conflict.Remote
		if in.Choice == ppb.ResolveConflictRequest_KEEP_REMOTE {
			source, destination = t.remote, t.local
			observed, target = conflict.Remote, conflict.Local
		}
		err = t.transfer(ctx, source, destination, conflict.Path, conflict.Path, observed, target)
		if err == nil {
			err = t.establish(ctx, conflict.Path)
		}
	}
	errorCode := ""
	if err != nil {
		t.data.State, t.data.Reason = classify(err)
		errorCode = "RESULT_UNKNOWN_RECONCILE_REQUIRED"
	} else {
		t.data.State = "active"
		t.data.Reason = ""
		if len(t.data.Conflicts) > 0 {
			t.data.State = "conflicted"
			t.data.Reason = "CONFLICTS_PENDING"
		}
	}
	out := t.data.status()
	saveErr := w.update(func(snapshot *storage.Snapshot) error {
		if err := snapshot.Put(mappingsBucket, in.MappingId, &t.data); err != nil {
			return err
		}
		return snapshot.CompleteRequest(namespace, in.RequestId, conflict.Id, out, errorCode)
	})
	if saveErr != nil {
		return nil, saveErr
	}
	if cleanupErr := cleanStaging(t.stageDir, t.data.Operations); cleanupErr != nil {
		return nil, cleanupErr
	}
	t.publish(ctx)
	w.notify()
	if err != nil {
		return nil, err
	}
	return out, nil
}
