// Package project manages persistent directory associations. Deleting a
// project never removes files; sessions and snapshots share the same store.
package project

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/server/storage"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
)

type Manager struct {
	store      *storage.Store
	base, user string
	baseAlias  string
	baseRoot   *os.Root
}

func New(store *storage.Store, base, user string) (*Manager, error) {
	if store == nil || user == "" || !filepath.IsAbs(base) {
		return nil, errors.New("project store, ordinary user and absolute base directory are required")
	}
	if err := os.MkdirAll(base, 0700); err != nil {
		return nil, err
	}
	canonical, root, err := OpenDirectory(base)
	if err != nil {
		return nil, err
	}
	return &Manager{store: store, base: canonical, baseAlias: filepath.Clean(base), baseRoot: root, user: user}, nil
}

// Close releases the pinned project-base handle; the shared state store remains
// owned by the daemon. A renamed/replaced base cannot redirect new directories.
func (m *Manager) Close() error { return m.baseRoot.Close() }

func (m *Manager) Create(ctx context.Context, request *pb.CreateProjectRequest) (*pb.Project, error) {
	if request == nil || request.RequestId == "" || len(request.RequestId) > 256 || strings.IndexByte(request.RequestId, 0) >= 0 {
		return nil, rpc(connect.CodeInvalidArgument, "request_id is required (maximum 256 bytes)")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var result *pb.Project
	var outcome error
	err := m.store.Update(func(state *storage.Snapshot) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		record, replay, err := state.Request("CreateProject", request.RequestId, request)
		if err != nil {
			return err
		}
		if replay {
			switch record.Status {
			case "succeeded":
				result = new(pb.Project)
				if err = json.Unmarshal(record.Result, result); err != nil {
					return err
				}
				if !storage.ValidResourceID(result.Id, "proj") || result.Id != record.ResourceID || result.User != m.user || result.Name != request.Name || !filepath.IsAbs(result.Path) || filepath.Clean(result.Path) != result.Path || result.CreatedAt <= 0 || len(result.SessionIds) != 0 {
					return errors.New("invalid persisted project request result")
				}
				return nil
			case "failed":
				outcome = recordedError(record)
				return nil
			default:
				outcome = rpc(connect.CodeFailedPrecondition, "project creation is pending recovery")
				return nil
			}
		}
		fail := func(err error) error {
			outcome = err
			return state.CompleteRequest("CreateProject", request.RequestId, "", safeMessage(err), connect.CodeOf(err).String())
		}
		if err = storage.ValidateName(request.Name); err != nil {
			return fail(rpc(connect.CodeInvalidArgument, "invalid project name"))
		}
		if !filepath.IsAbs(request.Path) || strings.IndexByte(request.Path, 0) >= 0 {
			return fail(rpc(connect.CodeInvalidArgument, "project path must be absolute"))
		}
		if len(request.Metadata) > 64 {
			return fail(rpc(connect.CodeResourceExhausted, "project metadata exceeds limit"))
		}
		metadataSize := 0
		for key, value := range request.Metadata {
			metadataSize += len(key) + len(value)
			if key == "" || len(key) > 128 {
				return fail(rpc(connect.CodeInvalidArgument, "invalid metadata key"))
			}
		}
		if metadataSize > 64<<10 {
			return fail(rpc(connect.CodeResourceExhausted, "project metadata exceeds limit"))
		}
		for id := range state.Buckets[storage.ProjectsBucket] {
			var existing pb.Project
			if err = state.Get(storage.ProjectsBucket, id, &existing); err != nil {
				return err
			}
			if existing.Name == request.Name {
				return fail(rpc(connect.CodeAlreadyExists, "project name already exists"))
			}
		}
		// Resolve and validate before changing the filesystem. For new paths,
		// every creation is confined to the configured project base via os.Root.
		path, exists, err := m.resolve(request.Path)
		if err != nil {
			return fail(err)
		}
		for id := range state.Buckets[storage.ProjectsBucket] {
			var existing pb.Project
			if err = state.Get(storage.ProjectsBucket, id, &existing); err != nil {
				return err
			}
			if storage.Within(existing.Path, path) || storage.Within(path, existing.Path) {
				return fail(rpc(connect.CodeAlreadyExists, "project roots must not overlap"))
			}
		}
		if !exists {
			if err = m.createDirectory(path); err != nil {
				return fail(err)
			}
		}
		canonical, root, err := OpenDirectory(path)
		if err != nil {
			return fail(filesystemError(err))
		}
		if err = root.Close(); err != nil {
			return fail(filesystemError(err))
		}
		if canonical != path {
			return fail(rpc(connect.CodeAborted, "project directory changed during creation"))
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		id := "proj_" + rand.Text()
		for state.Buckets[storage.ProjectsBucket][id] != nil {
			id = "proj_" + rand.Text()
		}
		result = &pb.Project{Id: id, Name: request.Name, Path: path, User: m.user, CreatedAt: time.Now().Unix(), Metadata: request.Metadata}
		if err = state.Put(storage.ProjectsBucket, id, result); err != nil {
			return err
		}
		return state.CompleteRequest("CreateProject", request.RequestId, id, result, "")
	})
	if err != nil {
		return nil, storeError(err)
	}
	if outcome != nil {
		return nil, outcome
	}
	// The result map must not alias a caller-owned request or store snapshot.
	data, err := json.Marshal(result)
	if err != nil {
		return nil, storeError(err)
	}
	copy := new(pb.Project)
	if err = json.Unmarshal(data, copy); err != nil {
		return nil, storeError(err)
	}
	return copy, nil
}

func (m *Manager) Get(ctx context.Context, request *pb.GetProjectRequest) (*pb.Project, error) {
	if request == nil || !storage.ValidResourceID(request.Id, "proj") {
		return nil, rpc(connect.CodeInvalidArgument, "valid project ID required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	state, err := m.store.View()
	if err != nil {
		return nil, storeError(err)
	}
	project := new(pb.Project)
	if err = state.Get(storage.ProjectsBucket, request.Id, project); errors.Is(err, os.ErrNotExist) {
		return nil, rpc(connect.CodeNotFound, "project not found")
	} else if err != nil {
		return nil, storeError(err)
	}
	if err = deriveSessions(state, project); err != nil {
		return nil, storeError(err)
	}
	return project, nil
}

func (m *Manager) List(ctx context.Context, request *pb.ListProjectsRequest) (*pb.ListProjectsResponse, error) {
	if request != nil && request.User != "" && request.User != m.user {
		return nil, rpc(connect.CodePermissionDenied, "user filter must match daemon user")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	state, err := m.store.View()
	if err != nil {
		return nil, storeError(err)
	}
	result := &pb.ListProjectsResponse{}
	for id := range state.Buckets[storage.ProjectsBucket] {
		project := new(pb.Project)
		if err = state.Get(storage.ProjectsBucket, id, project); err != nil {
			return nil, storeError(err)
		}
		if err = deriveSessions(state, project); err != nil {
			return nil, storeError(err)
		}
		result.Projects = append(result.Projects, project)
	}
	sort.Slice(result.Projects, func(i, j int) bool {
		a, b := result.Projects[i], result.Projects[j]
		if a.CreatedAt != b.CreatedAt {
			return a.CreatedAt > b.CreatedAt
		}
		return a.Id < b.Id
	})
	return result, nil
}

func (m *Manager) Delete(ctx context.Context, request *pb.DeleteProjectRequest) (*pb.Empty, error) {
	if request == nil || !storage.ValidResourceID(request.Id, "proj") {
		return nil, rpc(connect.CodeInvalidArgument, "valid project ID required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	err := m.store.Update(func(state *storage.Snapshot) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		// Missing projects are already deleted, so retrying deletion is safe.
		if state.Buckets[storage.ProjectsBucket][request.Id] == nil {
			return nil
		}
		for id := range state.Buckets[storage.SessionsBucket] {
			var session pb.Session
			if err := state.Get(storage.SessionsBucket, id, &session); err != nil {
				return err
			}
			if session.ProjectId == request.Id {
				return rpc(connect.CodeFailedPrecondition, "project still contains sessions")
			}
		}
		for id := range state.Buckets[storage.SnapshotsBucket] {
			var snapshot pb.Snapshot
			if err := state.Get(storage.SnapshotsBucket, id, &snapshot); err != nil {
				return err
			}
			if snapshot.ProjectId == request.Id {
				return rpc(connect.CodeFailedPrecondition, "project still contains configuration snapshots")
			}
		}
		delete(state.Buckets[storage.ProjectsBucket], request.Id)
		return nil
	})
	if err != nil {
		return nil, storeError(err)
	}
	return &pb.Empty{}, nil
}

func deriveSessions(state *storage.Snapshot, project *pb.Project) error {
	project.SessionIds = nil
	for id := range state.Buckets[storage.SessionsBucket] {
		var session pb.Session
		if err := state.Get(storage.SessionsBucket, id, &session); err != nil {
			return err
		}
		if session.ProjectId == project.Id {
			project.SessionIds = append(project.SessionIds, id)
		}
	}
	sort.Strings(project.SessionIds)
	return nil
}

func rpc(code connect.Code, message string) error { return connect.NewError(code, message) }
func safeMessage(err error) string {
	var rpcErr *connect.Error
	if errors.As(err, &rpcErr) {
		return rpcErr.Message()
	}
	return "project operation failed"
}
func recordedError(record storage.RequestRecord) error {
	code := connect.CodeInternal
	switch record.ErrorCode {
	case "invalid_argument":
		code = connect.CodeInvalidArgument
	case "already_exists":
		code = connect.CodeAlreadyExists
	case "permission_denied":
		code = connect.CodePermissionDenied
	case "resource_exhausted":
		code = connect.CodeResourceExhausted
	case "failed_precondition":
		code = connect.CodeFailedPrecondition
	case "aborted":
		code = connect.CodeAborted
	case "not_found":
		code = connect.CodeNotFound
	case "unavailable":
		code = connect.CodeUnavailable
	}
	var message string
	if json.Unmarshal(record.Result, &message) != nil {
		message = "previous project creation failed"
	}
	return rpc(code, message)
}
func storeError(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if errors.Is(err, storage.ErrRequestMismatch) {
		return rpc(connect.CodeInvalidArgument, "request_id reused with different parameters")
	}
	var rpcErr *connect.Error
	if errors.As(err, &rpcErr) {
		return err
	}
	return connect.NewError(connect.CodeUnavailable, "project state could not be committed or read").WithCause(err)
}
func filesystemError(err error) error {
	if errors.Is(err, os.ErrPermission) {
		return rpc(connect.CodePermissionDenied, "project directory is not accessible to daemon user")
	}
	if errors.Is(err, os.ErrNotExist) {
		return rpc(connect.CodeNotFound, "project directory disappeared")
	}
	return connect.NewError(connect.CodeFailedPrecondition, "project directory could not be opened or created").WithCause(err)
}
