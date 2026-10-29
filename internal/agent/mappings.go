package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	"github.com/DDSRem-Dev/Shikigami/internal/server/storage"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	agentpb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	pluginpb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
	"google.golang.org/protobuf/proto"
)

func normalizeMapping(m *pluginpb.Mapping) error {
	if m == nil || !validID(m.ServerId, "srv_") || !storage.ValidResourceID(m.ProjectId, "proj") {
		return rpcError(connect.CodeInvalidArgument, "INVALID_MAPPING_SCOPE")
	}
	if !filepath.IsAbs(m.LocalRoot) {
		return rpcError(connect.CodeInvalidArgument, "ABSOLUTE_LOCAL_ROOT_REQUIRED")
	}
	root, err := filepath.EvalSymlinks(m.LocalRoot)
	if err != nil {
		return rpcError(connect.CodeInvalidArgument, "LOCAL_ROOT_UNAVAILABLE")
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return rpcError(connect.CodeInvalidArgument, "LOCAL_ROOT_NOT_DIRECTORY")
	}
	m.LocalRoot = filepath.Clean(root)
	for path, name := range m.SessionHints {
		if path == "" || filepath.IsAbs(path) || filepath.Clean(path) != path || path == ".." || strings.HasPrefix(path, "../") || strings.ContainsAny(path, "\\\x00") || storage.ValidateName(name) != nil {
			return rpcError(connect.CodeInvalidArgument, "INVALID_SESSION_HINT")
		}
	}
	return nil
}
func (s *Service) ListMappings(_ context.Context, in *agentpb.ListMappingsRequest) (*pluginpb.MappingList, error) {
	r, err := s.requireResources()
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	snapshot, err := r.store.View()
	if err != nil {
		return nil, stateError(err)
	}
	if in.GetServerId() != "" {
		if _, err = storedServer(snapshot, in.ServerId); err != nil {
			return nil, err
		}
	}
	out := new(pluginpb.MappingList)
	for id := range snapshot.Buckets["mappings"] {
		m := new(pluginpb.Mapping)
		if err = snapshot.Get("mappings", id, m); err != nil {
			return nil, stateError(err)
		}
		if in.GetServerId() == "" || m.ServerId == in.ServerId {
			out.Mappings = append(out.Mappings, m)
		}
	}
	sort.Slice(out.Mappings, func(i, j int) bool { return out.Mappings[i].Id < out.Mappings[j].Id })
	if proto.Size(out) > connection.MaxMessageBytes {
		return nil, rpcError(connect.CodeResourceExhausted, "MAPPING_LIST_TOO_LARGE")
	}
	return out, nil
}
func (s *Service) PutMapping(ctx context.Context, in *agentpb.PutMappingRequest) (*pluginpb.Mapping, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	r, err := s.requireResources()
	if err != nil {
		return nil, err
	}
	if in == nil || in.Mapping == nil {
		return nil, rpcError(connect.CodeInvalidArgument, "MAPPING_REQUIRED")
	}
	encoded, err := requestBytes(in.RequestId, in)
	if err != nil {
		return nil, err
	}
	defer clear(encoded)
	// Replay precedes filesystem and network validation: a committed request
	// remains replayable after its source root or remote server is unavailable.
	r.mu.Lock()
	if r.ctx.Err() != nil {
		r.mu.Unlock()
		return nil, rpcError(connect.CodeUnavailable, "AGENT_STOPPING")
	}
	snapshot, err := r.store.View()
	if err != nil {
		r.mu.Unlock()
		return nil, stateError(err)
	}
	record, replay, err := snapshot.Request("PutMapping", in.RequestId, encoded)
	if err != nil {
		r.mu.Unlock()
		return nil, stateError(err)
	}
	out := new(pluginpb.Mapping)
	if replay {
		err = json.Unmarshal(record.Result, out)
		current := new(pluginpb.Mapping)
		stillCurrent := err == nil && snapshot.Get("mappings", out.Id, current) == nil && proto.Equal(current, out)
		r.mu.Unlock()
		if err != nil {
			return nil, stateError(err)
		}
		// The durable result predates worker acknowledgement. Retry applying
		// the current intent, but never replay a superseded configuration.
		if stillCurrent {
			err = s.applyMapping(ctx, out)
		}
		return out, err
	}
	r.mu.Unlock()
	m := proto.Clone(in.Mapping).(*pluginpb.Mapping)
	if err := normalizeMapping(m); err != nil {
		return nil, err
	}
	if err := validateMappingChange(snapshot, m, in.ExpectedRevision); err != nil {
		return nil, err
	}
	if m.SyncEnabled {
		manager := s.plugins.Load()
		if manager == nil {
			return nil, unavailableWorker()
		}
		if err := manager.Call(ctx, "sync", sdk.SyncCapability, func(context.Context, sdk.Lifecycle) error { return nil }); err != nil {
			return nil, err
		}
	}
	// Authentication and project/capability queries may call a remote daemon.
	// Hold neither Resources.mu nor the storage transaction lock here. The
	// returned record includes any identity pin and is revalidated at commit.
	client, verified, err := r.ResolveSnapshot(ctx, m.ServerId)
	if err != nil {
		return nil, err
	}
	if m.SyncEnabled {
		agreement, err := rpc.NewShikiDaemonClient(client).NegotiateProtocol(FreshRemoteContext(ctx), &pb.ProtocolOffer{Major: 1, Capabilities: []string{sdk.FileCapability}, RequiredCapabilities: []string{sdk.FileCapability}})
		if err != nil {
			return nil, safeRemoteError(err)
		}
		if agreement == nil || agreement.Major != 1 || agreement.Minor != 0 || !slices.Contains(agreement.Capabilities, sdk.FileCapability) {
			return nil, rpcError(connect.CodeFailedPrecondition, "SYNC_CAPABILITY_UNAVAILABLE")
		}
	}
	project, err := rpc.NewShikiDaemonClient(client).GetProject(FreshRemoteContext(ctx), &pb.GetProjectRequest{Id: m.ProjectId})
	if err != nil {
		return nil, safeRemoteError(err)
	}
	if project == nil || project.Id != m.ProjectId {
		return nil, rpcError(connect.CodeDataLoss, "PROJECT_ID_MISMATCH")
	}
	r.mu.Lock()
	unlock := sync.OnceFunc(r.mu.Unlock)
	defer unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	replay, err = r.mutate("PutMapping", in.RequestId, in, out, func(snapshot *storage.Snapshot) error {
		if err := validateMappingChange(snapshot, m, in.ExpectedRevision); err != nil {
			return err
		}
		current, err := storedServer(snapshot, m.ServerId)
		if err != nil {
			return err
		}
		if !proto.Equal(current, verified) {
			return rpcError(connect.CodeAborted, "SERVER_CHANGED")
		}
		proto.Merge(out, m)
		if out.Id == "" {
			out.Id = newID("map_")
		}
		out.Revision = newID("rev_")
		if out.SyncEnabled {
			if err := snapshot.Put("mapping_sync", out.Id, struct {
				Used bool `json:"used"`
			}{true}); err != nil {
				return err
			}
		}
		return snapshot.Put("mappings", out.Id, out)
	})
	if err == nil && !replay {
		r.publish("mapping", out.Id, out.ServerId, out.Revision)
	}
	unlock()
	if err == nil {
		err = s.applyMapping(ctx, out)
	}
	return out, err
}

// validateMappingChange is repeated at commit after the remote preflight.
// Concurrent changes cannot reuse an earlier revision or root-overlap check.
func validateMappingChange(snapshot *storage.Snapshot, m *pluginpb.Mapping, expected string) error {
	if m.Id == "" {
		if expected != "" {
			return rpcError(connect.CodeAborted, "REVISION_CONFLICT")
		}
	} else {
		old := new(pluginpb.Mapping)
		if snapshot.Get("mappings", m.Id, old) != nil {
			return rpcError(connect.CodeNotFound, "MAPPING_NOT_FOUND")
		}
		if err := checkRevision(old.Revision, expected); err != nil {
			return err
		}
		if old.ServerId != m.ServerId || old.ProjectId != m.ProjectId || old.LocalRoot != m.LocalRoot {
			return rpcError(connect.CodeInvalidArgument, "MAPPING_IDENTITY_IMMUTABLE")
		}
	}
	for id := range snapshot.Buckets["mappings"] {
		other := new(pluginpb.Mapping)
		if err := snapshot.Get("mappings", id, other); err != nil {
			return stateError(err)
		}
		if id != m.Id && (storage.Within(other.LocalRoot, m.LocalRoot) || storage.Within(m.LocalRoot, other.LocalRoot)) {
			return rpcError(connect.CodeAlreadyExists, "MAPPING_ROOT_OVERLAP")
		}
	}
	return nil
}
func (s *Service) RemoveMapping(ctx context.Context, in *agentpb.RemoveMappingRequest) (*pb.Empty, error) {
	r, err := s.requireResources()
	if err != nil {
		return nil, err
	}
	if in == nil {
		return nil, rpcError(connect.CodeInvalidArgument, "MAPPING_REQUIRED")
	}
	if !in.ArchivePending {
		snapshot, err := r.store.View()
		if err != nil {
			return nil, stateError(err)
		}
		if _, used := snapshot.Buckets["mapping_sync"][in.MappingId]; used {
			mapping := new(pluginpb.Mapping)
			if err := snapshot.Get("mappings", in.MappingId, mapping); err != nil {
				return nil, stateError(err)
			}
			if err := checkRevision(mapping.Revision, in.ExpectedRevision); err != nil {
				return nil, err
			}
			if mapping.SyncEnabled {
				return nil, rpcError(connect.CodeFailedPrecondition, "MAPPING_MUST_BE_PAUSED")
			}
			// Query outside the resources lock: the worker may call CoreAPI.
			// A matching paused revision proves its cancellation and in-flight
			// operations have settled. The transaction below rechecks that same
			// revision, so a concurrent resume/reconfigure cannot reuse this check.
			scope := &pluginpb.Scope{ServerId: mapping.ServerId, ProjectId: mapping.ProjectId, MappingId: mapping.Id}
			status, err := s.GetSyncState(ctx, scope)
			if err != nil {
				return nil, rpcError(connect.CodeFailedPrecondition, "CANNOT_VERIFY_PENDING_SYNC_REQUIRE_ARCHIVE")
			}
			if err := checkMappingRemovalState(mapping, status); err != nil {
				return nil, err
			}
		}
	}
	r.mu.Lock()
	unlock := sync.OnceFunc(r.mu.Unlock)
	defer unlock()
	out := new(pb.Empty)
	serverID := ""
	replay, err := r.mutate("RemoveMapping", in.GetRequestId(), in, out, func(snapshot *storage.Snapshot) error {
		m := new(pluginpb.Mapping)
		if snapshot.Get("mappings", in.MappingId, m) != nil {
			return rpcError(connect.CodeNotFound, "MAPPING_NOT_FOUND")
		}
		if err := checkRevision(m.Revision, in.ExpectedRevision); err != nil {
			return err
		}
		if m.SyncEnabled {
			return rpcError(connect.CodeFailedPrecondition, "MAPPING_MUST_BE_PAUSED")
		}
		serverID = m.ServerId
		if err := snapshot.Put("archived_mappings", m.Id, struct {
			Mapping    *pluginpb.Mapping `json:"mapping"`
			ArchivedAt int64             `json:"archived_at"`
		}{m, time.Now().Unix()}); err != nil {
			return err
		}
		delete(snapshot.Buckets["mappings"], m.Id)
		delete(snapshot.Buckets["mapping_sync"], m.Id)
		return nil
	})
	if err == nil {
		// Cache ownership ends with the desired mapping, even when waiting for
		// the worker to finish subsequently times out. Late writes recheck under
		// this same lock before admitting runtime state.
		r.syncStates.Delete(in.MappingId)
		if !replay {
			r.publish("mapping", in.MappingId, serverID, "")
		}
	}
	unlock()
	if err == nil {
		err = s.awaitMappingStopped(ctx, in.MappingId)
	}
	return out, err
}

func checkMappingRemovalState(mapping *pluginpb.Mapping, status *pluginpb.SyncState) error {
	if status == nil || status.MappingId != mapping.Id || status.MappingRevision != mapping.Revision || status.State != "paused" {
		return rpcError(connect.CodeFailedPrecondition, "CANNOT_VERIFY_PENDING_SYNC_REQUIRE_ARCHIVE")
	}
	if status.PendingFiles != 0 || status.ConflictCount != 0 {
		return rpcError(connect.CodeFailedPrecondition, "PENDING_SYNC_REQUIRE_ARCHIVE")
	}
	return nil
}

func (s *Service) applyMapping(ctx context.Context, mapping *pluginpb.Mapping) error {
	resources, err := s.requireResources()
	if err != nil {
		return err
	}
	snapshot, err := resources.store.View()
	if err != nil {
		return stateError(err)
	}
	current := new(pluginpb.Mapping)
	if err := snapshot.Get("mappings", mapping.Id, current); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return stateError(err)
	}
	if !proto.Equal(current, mapping) {
		return nil
	}
	manager := s.plugins.Load()
	if manager == nil {
		if mapping.SyncEnabled {
			return unavailableWorker()
		}
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		list, err := manager.List()
		if err != nil {
			return err
		}
		for _, status := range list.Plugins {
			if status.PluginId != "sync" {
				continue
			}
			if status.State == "ready" && status.Enabled {
				return manager.Call(ctx, "sync", sdk.SyncCapability, func(call context.Context, lifecycle sdk.Lifecycle) error {
					if resources := s.resources.Load(); resources != nil {
						return resources.configureMapping(call, lifecycle.Sync(), mapping)
					}
					return rpcError(connect.CodeFailedPrecondition, "AGENT_STATE_UNAVAILABLE")
				})
			}
			if status.State == "stopped" || status.State == "failed" || status.State == "incompatible" {
				if mapping.SyncEnabled {
					return unavailableWorker()
				}
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// configureMapping is shared by startup restoration and foreground changes.
// The worker may call CoreAPI, so it always runs without the resources lock.
func (r *Resources) configureMapping(ctx context.Context, worker sdk.SyncClient, mapping *pluginpb.Mapping) error {
	status, err := worker.ConfigureMapping(ctx, mapping)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if r.ctx.Err() != nil {
		return rpcError(connect.CodeUnavailable, "AGENT_STOPPING")
	}
	snapshot, err := r.store.View()
	if err != nil {
		return stateError(err)
	}
	current := new(pluginpb.Mapping)
	if err := snapshot.Get("mappings", mapping.Id, current); errors.Is(err, os.ErrNotExist) {
		return nil // A deleted mapping cannot own a cached worker response.
	} else if err != nil {
		return stateError(err)
	}
	if current.Revision != mapping.Revision {
		return nil // Another operation already superseded this configuration.
	}
	if status == nil || status.MappingId != mapping.Id || status.MappingRevision != mapping.Revision {
		return rpcError(connect.CodeDataLoss, "INVALID_SYNC_STATE")
	}
	switch status.State {
	case "paused", "reconciling", "active", "conflicted", "offline", "auth_required", "error":
	default:
		return rpcError(connect.CodeDataLoss, "INVALID_SYNC_STATE")
	}
	r.syncStates.Store(mapping.Id, proto.Clone(status).(*pluginpb.SyncState))
	return nil
}

func (s *Service) awaitMappingStopped(ctx context.Context, id string) error {
	manager := s.plugins.Load()
	if manager == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		list, err := manager.List()
		if err != nil {
			return err
		}
		for _, status := range list.Plugins {
			if status.PluginId != "sync" {
				continue
			}
			if status.State == "stopped" || status.State == "failed" || status.State == "incompatible" {
				return nil
			}
			if status.State == "ready" {
				err := manager.Call(ctx, "sync", sdk.SyncCapability, func(call context.Context, lifecycle sdk.Lifecycle) error {
					_, err := lifecycle.Sync().GetSyncState(call, &pluginpb.Scope{MappingId: id})
					return err
				})
				if connect.CodeOf(err) == connect.CodeNotFound {
					return nil
				}
				if err != nil && connect.CodeOf(err) != connect.CodeFailedPrecondition {
					return err
				}
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
