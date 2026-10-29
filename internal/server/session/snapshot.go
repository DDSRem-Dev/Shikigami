package session

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"sort"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/server/storage"
	"github.com/DDSRem-Dev/Shikigami/pkg/config"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	"google.golang.org/protobuf/proto"
)

const snapshotSchema = 1
const restoreNamespace = "snapshot.restore"

func (m *Manager) CreateSnapshot(parent context.Context, request *pb.CreateSnapshotRequest) (*pb.Snapshot, error) {
	if request == nil || !storage.ValidResourceID(request.SessionId, "sess") {
		return nil, rpcError(connect.CodeInvalidArgument, "valid source session ID required")
	}
	if len(request.Description) > 4096 {
		return nil, rpcError(connect.CodeInvalidArgument, "snapshot description exceeds 4096 bytes")
	}
	m.operations.Lock()
	defer m.operations.Unlock()
	ctx, release, e := m.operationContext(parent)
	if e != nil {
		return nil, e
	}
	defer release()
	var result *pb.Snapshot
	e = m.store.Update(func(state *storage.Snapshot) error {
		if e := ctx.Err(); e != nil {
			return e
		}
		source := new(pb.Session)
		if e := state.Get(storage.SessionsBucket, request.SessionId, source); e != nil {
			return e
		}
		project := new(pb.Project)
		if e := state.Get(storage.ProjectsBucket, source.ProjectId, project); e != nil {
			return e
		}
		directory, e := sessionDirectory(project.Path, source.WorkingDir)
		if e != nil {
			return e
		}
		relative, e := filepath.Rel(project.Path, directory)
		if e != nil {
			return e
		}
		relative = filepath.ToSlash(relative)
		if !config.ValidTemplateDirectory(relative) {
			return rpcError(connect.CodeFailedPrecondition, "snapshot working directory is outside its project")
		}
		autoStart := source.AutoStartClaude == nil || source.GetAutoStartClaude()
		result = &pb.Snapshot{Id: "snap_" + rand.Text(), ProjectId: source.ProjectId, SessionId: source.Id, SessionName: source.Name, CreatedAt: time.Now().Unix(), Description: request.Description, SessionDescription: source.Description, WorkingDir: relative, Tags: append([]string(nil), source.Tags...), Metadata: config.SnapshotMetadata(source.Metadata), AutoStartClaude: &autoStart, SchemaVersion: snapshotSchema}
		for state.Buckets[storage.SnapshotsBucket][result.Id] != nil {
			result.Id = "snap_" + rand.Text()
		}
		return state.Put(storage.SnapshotsBucket, result.Id, result)
	})
	if e != nil {
		return nil, storageError(e)
	}
	return result, nil
}
func (m *Manager) ListSnapshots(ctx context.Context, request *pb.ListSnapshotsRequest) (*pb.ListSnapshotsResponse, error) {
	if request == nil {
		request = &pb.ListSnapshotsRequest{}
	}
	if request.ProjectId != "" && !storage.ValidResourceID(request.ProjectId, "proj") || request.SessionId != "" && !storage.ValidResourceID(request.SessionId, "sess") {
		return nil, rpcError(connect.CodeInvalidArgument, "invalid project or source session ID")
	}
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	state, e := m.store.View()
	if e != nil {
		return nil, storageError(e)
	}
	result := new(pb.ListSnapshotsResponse)
	for id := range state.Buckets[storage.SnapshotsBucket] {
		snapshot := new(pb.Snapshot)
		if e := state.Get(storage.SnapshotsBucket, id, snapshot); e != nil {
			return nil, storageError(e)
		}
		if (request.ProjectId == "" || snapshot.ProjectId == request.ProjectId) && (request.SessionId == "" || snapshot.SessionId == request.SessionId) {
			result.Snapshots = append(result.Snapshots, snapshot)
		}
	}
	sort.Slice(result.Snapshots, func(i, j int) bool {
		a, b := result.Snapshots[i], result.Snapshots[j]
		if a.CreatedAt != b.CreatedAt {
			return a.CreatedAt > b.CreatedAt
		}
		return a.Id < b.Id
	})
	return result, nil
}
func (m *Manager) DeleteSnapshot(parent context.Context, request *pb.DeleteSnapshotRequest) (*pb.Empty, error) {
	if request == nil || !storage.ValidResourceID(request.SnapshotId, "snap") {
		return nil, rpcError(connect.CodeInvalidArgument, "valid snapshot ID required")
	}
	m.operations.Lock()
	defer m.operations.Unlock()
	ctx, release, e := m.operationContext(parent)
	if e != nil {
		return nil, e
	}
	defer release()
	e = m.store.Update(func(state *storage.Snapshot) error {
		if e := ctx.Err(); e != nil {
			return e
		}
		delete(state.Buckets[storage.SnapshotsBucket], request.SnapshotId)
		return nil
	})
	if e != nil {
		return nil, storageError(e)
	}
	return &pb.Empty{}, nil
}

// Restore reserves an immutable creation plan with its request fingerprint,
// then delegates tmux creation and crash recovery to the normal Create path.
// Pending plans survive deletion of the source snapshot; completed retries do
// not need the snapshot, session, or working directory to remain present.
func (m *Manager) RestoreSnapshot(parent context.Context, request *pb.RestoreSnapshotRequest) (*pb.Session, error) {
	if request == nil || !storage.ValidResourceID(request.SnapshotId, "snap") || request.RequestId == "" || len(request.RequestId) > 128 {
		return nil, rpcError(connect.CodeInvalidArgument, "valid snapshot ID and request_id required")
	}
	if e := storage.ValidateName(request.NewSessionName); e != nil {
		return nil, rpcError(connect.CodeInvalidArgument, "valid new session name required")
	}
	request = proto.Clone(request).(*pb.RestoreSnapshotRequest)
	ctx, release, e := m.operationContext(parent)
	if e != nil {
		return nil, e
	}
	defer release()
	var create *pb.CreateSessionRequest
	var completed *pb.Session
	var previousError error
	e = m.store.Update(func(state *storage.Snapshot) error {
		if e := ctx.Err(); e != nil {
			return e
		}
		record, replay, e := state.Request(restoreNamespace, request.RequestId, request)
		if e != nil {
			return e
		}
		if replay {
			if record.Status == "succeeded" {
				completed = new(pb.Session)
				return json.Unmarshal(record.Result, completed)
			}
			if record.Status == "failed" {
				code := connect.CodeFailedPrecondition
				if e = code.UnmarshalText([]byte(record.ErrorCode)); e != nil {
					return e
				}
				previousError = rpcError(code, "original snapshot restore failed")
				return nil
			}
			create = new(pb.CreateSessionRequest)
			return json.Unmarshal(record.Result, create)
		}
		snapshot := new(pb.Snapshot)
		if e = state.Get(storage.SnapshotsBucket, request.SnapshotId, snapshot); e != nil {
			return e
		}
		if snapshot.SchemaVersion != snapshotSchema {
			return rpcError(connect.CodeFailedPrecondition, "unsupported snapshot schema version")
		}
		if !config.ValidTemplateDirectory(snapshot.WorkingDir) {
			return rpcError(connect.CodeFailedPrecondition, "invalid snapshot working directory")
		}
		project := new(pb.Project)
		if e = state.Get(storage.ProjectsBucket, snapshot.ProjectId, project); e != nil {
			return e
		}
		id := sha256.Sum256([]byte(request.RequestId))
		autoStart := snapshot.AutoStartClaude == nil || snapshot.GetAutoStartClaude()
		create = &pb.CreateSessionRequest{ProjectId: snapshot.ProjectId, Name: request.NewSessionName, WorkingDir: snapshot.WorkingDir, Description: snapshot.SessionDescription, Tags: append([]string(nil), snapshot.Tags...), Metadata: config.SnapshotMetadata(snapshot.Metadata), AutoStartClaude: &autoStart, RequestId: "restore_" + hex.EncodeToString(id[:])}
		record.Result, e = json.Marshal(create)
		if e != nil {
			return e
		}
		state.Requests[restoreNamespace+"\x00"+request.RequestId] = record
		return nil
	})
	if e != nil {
		return nil, storageError(e)
	}
	if completed != nil || previousError != nil {
		return completed, previousError
	}
	result, createErr := m.Create(ctx, create)
	code := ""
	if createErr != nil {
		switch connect.CodeOf(createErr) {
		case connect.CodeInvalidArgument, connect.CodeNotFound, connect.CodePermissionDenied, connect.CodeAlreadyExists, connect.CodeFailedPrecondition, connect.CodeResourceExhausted:
			code = connect.CodeOf(createErr).String()
		default:
			return nil, createErr
		}
	}
	e = m.store.Update(func(state *storage.Snapshot) error {
		record, _, e := state.Request(restoreNamespace, request.RequestId, request)
		if e != nil {
			return e
		}
		if record.Status != "pending" {
			return nil
		}
		id := ""
		if result != nil {
			id = result.Id
		}
		return state.CompleteRequest(restoreNamespace, request.RequestId, id, result, code)
	})
	if e != nil {
		return nil, storageError(e)
	}
	return result, createErr
}
