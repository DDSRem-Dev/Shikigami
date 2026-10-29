package client

import (
	"bytes"
	"context"
	"encoding/json"
	"path"
	"sort"

	"connectrpc.com/connect/v2"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	limits "github.com/DDSRem-Dev/Shikigami/pkg/plugin/limits"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	"github.com/DDSRem-Dev/Shikigami/plugins/sync/reconcile"
	"google.golang.org/protobuf/proto"
)

func (t *task) ignore(ctx context.Context) (*reconcile.Ignore, error) {
	return t.local.Ignore(ctx, nil)
}
func manifest(ctx context.Context, files sdk.FileService, project string, ignore *reconcile.Ignore) (map[string]*pb.FileInfo, error) {
	found := map[string]*pb.FileInfo{}
	directories := []string{"."}
	entries := 0
	for len(directories) > 0 {
		directory := directories[0]
		directories = directories[1:]
		token := ""
		seen := map[string]bool{}
		for {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			wirePath := directory
			if wirePath == "." {
				wirePath = ""
			}
			page, err := files.ListFiles(ctx, &pb.ListFilesRequest{ProjectId: project, Path: wirePath, PageSize: 100, PageToken: token})
			if err != nil {
				return nil, err
			}
			if page == nil {
				return nil, fail(connect.CodeDataLoss, "FILE_LIST_MISSING")
			}
			for _, info := range page.Files {
				entries++
				if entries > maxFiles {
					return nil, fail(connect.CodeResourceExhausted, "SCAN_ENTRY_LIMIT")
				}
				if info == nil || !validPath(info.Path) || path.Dir(info.Path) != directory {
					return nil, fail(connect.CodeDataLoss, "FILE_LIST_PATH_INVALID")
				}
				if ignore.Ignored(info.Path, info.IsDir) {
					continue
				}
				if info.IsDir {
					directories = append(directories, info.Path)
				} else {
					if found[info.Path] != nil {
						return nil, fail(connect.CodeDataLoss, "FILE_LIST_DUPLICATE")
					}
					found[info.Path] = info
				}
			}
			token = page.NextPageToken
			if token == "" {
				break
			}
			if seen[token] {
				return nil, fail(connect.CodeDataLoss, "FILE_PAGE_REPEATED")
			}
			seen[token] = true
		}
	}
	return found, nil
}
func listedObservation(ctx context.Context, files sdk.FileService, project, name string, listed map[string]*pb.FileInfo, maxBytes int64) (*ppb.FileVersion, error) {
	if info := listed[name]; info != nil {
		if info.Size > maxBytes {
			return nil, fail(connect.CodeResourceExhausted, "FILE_TOO_LARGE")
		}
		v := &ppb.FileVersion{Exists: true, ContentHash: info.ContentHash, Mode: info.Mode, Revision: info.Revision}
		if _, err := reconcile.Decide(v, v, v); err != nil {
			return nil, fail(connect.CodeDataLoss, "INVALID_FILE_OBSERVATION")
		}
		return v, nil
	}
	v, _, err := observation(ctx, files, project, name, maxBytes)
	return v, err
}
func (t *task) reconcile(ctx context.Context) (result error) {
	before, _ := json.Marshal(t.data)
	defer func() {
		if result != nil {
			t.data.State, t.data.Reason = classify(result)
		}
		after, _ := json.Marshal(t.data)
		if !bytes.Equal(before, after) {
			if err := t.persist(); err != nil {
				result = err
				return
			}
			t.publish(ctx)
		}
	}()
	ignore, err := t.ignore(ctx)
	if err != nil {
		return err
	}
	project := t.data.Mapping.ProjectId
	left, err := manifest(ctx, t.local, project, ignore)
	if err != nil {
		return err
	}
	right, err := manifest(ctx, t.remote, project, ignore)
	if err != nil {
		return err
	}
	paths := map[string]bool{}
	for name := range left {
		paths[name] = true
	}
	for name := range right {
		paths[name] = true
	}
	for name := range t.data.Baselines {
		paths[name] = true
	}
	for name := range t.data.LegacyUnverified {
		paths[name] = true
	}
	for name := range t.data.Operations {
		paths[name] = true
	}
	for name := range t.data.Conflicts {
		paths[name] = true
	}
	if len(paths) > maxFiles {
		return fail(connect.CodeResourceExhausted, "BASELINE_ENTRY_LIMIT")
	}
	ordered := make([]string, 0, len(paths))
	for name := range paths {
		ordered = append(ordered, name)
	}
	sort.Strings(ordered)
	skipped := false
	for _, name := range ordered {
		if err = ctx.Err(); err != nil {
			return err
		}
		if t.data.Operations[name] != nil {
			if err = t.recoverOperation(ctx, name, !ignore.Ignored(name, false) && !ignore.Ignored(t.data.Operations[name].SourcePath, false)); err != nil {
				return err
			}
			continue
		}
		if t.data.LegacyUnverified[name] {
			if err = t.recoverLegacy(ctx, name); err != nil {
				return err
			}
			continue
		}
		if ignore.Ignored(name, false) {
			delete(t.data.Conflicts, name)
			continue
		}
		local, err := listedObservation(ctx, t.local, project, name, left, t.worker.maxFileBytes)
		if connect.CodeOf(err) == connect.CodeResourceExhausted {
			skipped = true
			continue
		}
		if err != nil {
			return err
		}
		remote, err := listedObservation(ctx, t.remote, project, name, right, t.worker.maxFileBytes)
		if connect.CodeOf(err) == connect.CodeResourceExhausted {
			skipped = true
			continue
		}
		if err != nil {
			return err
		}
		base := t.data.Baselines[name]
		action, err := reconcile.Decide(base, local, remote)
		if err != nil {
			return fail(connect.CodeDataLoss, "INVALID_FILE_OBSERVATION")
		}
		switch action {
		case reconcile.Equal:
			t.acceptCommonBaseline(name, remote)
		case reconcile.Initial, reconcile.Conflict:
			if err = t.conflict(name, local, remote); err != nil {
				return err
			}
		case reconcile.Upload, reconcile.Download:
			source, destination := sdk.FileService(t.local), t.remote
			observed, target := local, remote
			if action == reconcile.Download {
				source, destination = t.remote, t.local
				observed, target = remote, local
			}
			if err = t.transfer(ctx, source, destination, name, name, observed, target); err != nil {
				return err
			}
			if err = t.establish(ctx, name); err != nil {
				return err
			}
			if err = t.persist(); err != nil {
				return err
			}
		}
	}
	t.data.State = "active"
	t.data.Reason = ""
	if skipped {
		t.data.State = "error"
		t.data.Reason = "FILE_TOO_LARGE_SKIPPED"
	}
	if len(t.data.Conflicts) > 0 {
		t.data.State = "conflicted"
		t.data.Reason = "CONFLICTS_PENDING"
		for _, c := range t.data.Conflicts {
			if c.Base == nil {
				t.data.Reason = "INITIAL_SYNC_CHOICE_REQUIRED"
				break
			}
		}
	}
	return nil
}
func (t *task) conflict(name string, local, remote *ppb.FileVersion) error {
	base := t.data.Baselines[name]
	generation, err := reconcile.Generation(t.data.Mapping.Id, name, base, local, remote)
	if err != nil {
		return err
	}
	conflictID := id("conf_")
	if previous := t.data.Conflicts[name]; previous != nil {
		conflictID = previous.Id
	}
	c := &ppb.Conflict{Id: conflictID, MappingId: t.data.Mapping.Id, Path: name, Base: base, Local: local, Remote: remote, Generation: generation}
	t.data.Conflicts[name] = proto.Clone(c).(*ppb.Conflict)
	return nil
}
func (t *task) establish(ctx context.Context, name string) error {
	project := t.data.Mapping.ProjectId
	local, _, err := observation(ctx, t.local, project, name, t.worker.maxFileBytes)
	if err != nil {
		return err
	}
	remote, _, err := observation(ctx, t.remote, project, name, t.worker.maxFileBytes)
	if err != nil {
		return err
	}
	if !reconcile.Same(local, remote) {
		if err = t.conflict(name, local, remote); err != nil {
			return err
		}
		return fail(connect.CodeAborted, "POST_COMMIT_CHANGED")
	}
	t.acceptCommonBaseline(name, remote)
	return nil
}
func (t *task) publish(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	_, _ = t.worker.core.PublishEvent(ctx, &ppb.PublishEventRequest{ExpectedHostInstanceId: t.worker.host, Event: &ppb.Event{EventId: id("event_"), Type: "sync", Scope: scopeFor(t.data.Mapping), SourceInstanceId: t.worker.instance, Payload: &ppb.Event_SyncState{SyncState: t.data.status()}}})
}
func (w *Worker) ListConflicts(_ context.Context, scope *ppb.Scope) (*ppb.ConflictList, error) {
	t, err := w.get(scope.GetMappingId())
	if err != nil {
		return nil, err
	}
	t.op.Lock()
	defer t.op.Unlock()
	current, err := w.get(scope.GetMappingId())
	if err != nil {
		return nil, err
	}
	if current != t {
		return nil, fail(connect.CodeFailedPrecondition, "MAPPING_CHANGED")
	}

	if scope.ServerId != "" && scope.ServerId != t.data.Mapping.ServerId || scope.ProjectId != "" && scope.ProjectId != t.data.Mapping.ProjectId {
		return nil, fail(connect.CodePermissionDenied, "MAPPING_SCOPE_MISMATCH")
	}
	out := new(ppb.ConflictList)
	for _, c := range t.data.Conflicts {
		out.Conflicts = append(out.Conflicts, proto.Clone(c).(*ppb.Conflict))
	}
	sort.Slice(out.Conflicts, func(i, j int) bool { return out.Conflicts[i].Path < out.Conflicts[j].Path })
	if proto.Size(out) > limits.MaxMessageBytes {
		return nil, fail(connect.CodeResourceExhausted, "CONFLICT_LIST_TOO_LARGE")
	}
	return out, nil
}
func (t *task) findConflict(conflictID string) (*ppb.Conflict, error) {
	for _, c := range t.data.Conflicts {
		if c.Id == conflictID {
			return c, nil
		}
	}
	return nil, fail(connect.CodeNotFound, "CONFLICT_NOT_FOUND")
}
func (t *task) validateConflict(ctx context.Context, conflictID, generation string) (*ppb.Conflict, error) {
	c, err := t.findConflict(conflictID)
	if err != nil {
		return nil, err
	}
	if generation == "" || c.Generation != generation {
		return nil, fail(connect.CodeAborted, "CONFLICT_GENERATION_CHANGED")
	}
	local, _, err := observation(ctx, t.local, t.data.Mapping.ProjectId, c.Path, t.worker.maxFileBytes)
	if err != nil {
		return nil, err
	}
	remote, _, err := observation(ctx, t.remote, t.data.Mapping.ProjectId, c.Path, t.worker.maxFileBytes)
	if err != nil {
		return nil, err
	}
	fresh, err := reconcile.Generation(t.data.Mapping.Id, c.Path, c.Base, local, remote)
	if err != nil {
		return nil, err
	}
	if fresh != generation {
		if err = t.conflict(c.Path, local, remote); err != nil {
			return nil, err
		}
		if err = t.persist(); err != nil {
			return nil, err
		}
		return nil, fail(connect.CodeAborted, "CONFLICT_GENERATION_CHANGED")
	}
	return proto.Clone(c).(*ppb.Conflict), nil
}
func (w *Worker) ReadConflictFile(ctx context.Context, in *ppb.ReadConflictFileRequest, send func(*pb.FileData) error) error {
	t, err := w.get(in.GetMappingId())
	if err != nil {
		return err
	}
	ctx, cancel := t.operationContext(ctx, false)
	defer cancel()
	t.op.Lock()
	defer t.op.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	c, err := t.validateConflict(ctx, in.ConflictId, in.ExpectedGeneration)
	if err != nil {
		return err
	}
	files := sdk.FileService(t.local)
	version := c.Local
	switch in.Side {
	case ppb.ReadConflictFileRequest_LOCAL:
	case ppb.ReadConflictFileRequest_REMOTE:
		files = t.remote
		version = c.Remote
	default:
		return fail(connect.CodeInvalidArgument, "CONFLICT_SIDE_REQUIRED")
	}
	if !version.Exists {
		return fail(connect.CodeNotFound, "CONFLICT_SIDE_DELETED")
	}
	if err = files.ReadFile(ctx, &pb.ReadFileRequest{ProjectId: t.data.Mapping.ProjectId, Path: c.Path, ExpectedRevision: version.Revision}, send); err != nil {
		return err
	}
	_, err = t.validateConflict(ctx, in.ConflictId, in.ExpectedGeneration)
	return err
}
