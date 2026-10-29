package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"connectrpc.com/connect/v2"
	host "github.com/DDSRem-Dev/Shikigami/internal/plugin"
	"github.com/DDSRem-Dev/Shikigami/pkg/config"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	schema "github.com/DDSRem-Dev/Shikigami/pkg/plugin/config"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	prpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin/pluginconnect"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

func (r *Resources) openPlugins(cfg *config.Client, build string, runtimeOptions RuntimeOptions) error {
	options := host.SupervisorOptions{StateDir: filepath.Join(cfg.Agent.StateDir, "plugin-intent"), Validate: func(id string, values *structpb.Struct) error {
		if id == "sync" {
			return schema.ValidateSyncClient(values)
		}
		_, err := schema.Parse(values, "client")
		if err != nil {
			return rpcError(connect.CodeInvalidArgument, "INVALID_PORT_CONFIG")
		}
		return nil
	}, Changed: func(id string) { r.publish("plugin", id, "", "") }}
	options.Config = func(id string, values *structpb.Struct) (host.Config, error) {
		manifest, err := host.ResolveBuiltinManifest(cfg.PluginsDir, id, "client", build)
		if err != nil {
			return host.Config{}, err
		}
		core := &pluginCore{resources: r, id: id}
		core.RemoteCore.Resolve = func(ctx context.Context, scope *ppb.Scope) (rpc.ShikiDaemonClient, error) {
			if err := core.authorize(ctx, scope); err != nil {
				return nil, err
			}
			client, err := r.Resolve(ctx, scope.ServerId)
			if err != nil {
				return nil, err
			}
			return rpc.NewShikiDaemonClient(client), nil
		}
		capabilities := []string{"core.projects.read", "core.sessions.read", "core.mappings.read", "core.events.write", "core.events.read", "core.config.read", "core.log", "file.read", "file.write", "file.watch"}
		scopes := []*ppb.Scope{{}}
		if id == "port" {
			capabilities = []string{"port.list", "port.forward", "core.config.read", "core.log"}
			scopes = nil
			core.RemoteCore.AuthorizeForward = core.authorizeForward
		}
		return host.Config{Manifest: manifest, RunDir: filepath.Join(cfg.Agent.StateDir, "plugin-run"), StateDir: filepath.Join(cfg.Agent.StateDir, "plugin-state", id), HostInstanceID: r.service.Registry.InstanceID(), Values: values, Core: core, CredentialLifetime: runtimeOptions.PluginCredentialLifetime, Policy: host.Policy{Side: "client", Capabilities: capabilities, Scopes: scopes, AuthorizeScope: core.authorize}}, nil
	}
	options.Ready = func(ctx context.Context, id string, lifecycle sdk.Lifecycle) error {
		if id == "port" {
			return r.restoreForwards(ctx, lifecycle)
		}
		if id != "sync" {
			return nil
		}
		mappings, err := r.service.ListMappings(ctx, &apb.ListMappingsRequest{})
		if err != nil {
			return err
		}
		for _, mapping := range mappings.Mappings {
			if err := r.configureMapping(ctx, lifecycle.Sync(), mapping); err != nil {
				return err
			}
		}
		return nil
	}
	manager, err := host.NewSupervisor(r.ctx, options)
	if err != nil {
		return err
	}
	r.service.plugins.Store(manager)
	manager.Start()
	return nil
}

type pluginCore struct {
	host.RemoteCore
	resources *Resources
	id        string
}

var _ prpc.CoreAPIHandler = (*pluginCore)(nil)

func (c *pluginCore) mapping(scope *ppb.Scope) (*ppb.Mapping, error) {
	if scope == nil || scope.MappingId == "" || scope.SessionId != "" {
		return nil, rpcError(connect.CodePermissionDenied, "PLUGIN_SCOPE_DENIED")
	}
	snapshot, err := c.resources.store.View()
	if err != nil {
		return nil, stateError(err)
	}
	mapping := new(ppb.Mapping)
	if err := snapshot.Get("mappings", scope.MappingId, mapping); errors.Is(err, os.ErrNotExist) {
		return nil, rpcError(connect.CodeNotFound, "MAPPING_NOT_FOUND")
	} else if err != nil {
		return nil, stateError(err)
	}
	if mapping.ServerId != scope.ServerId || mapping.ProjectId != scope.ProjectId {
		return nil, rpcError(connect.CodePermissionDenied, "PLUGIN_SCOPE_DENIED")
	}
	return mapping, nil
}
func (c *pluginCore) authorize(ctx context.Context, scope *ppb.Scope) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if c.id == "port" {
		return c.authorizePort(scope)
	}
	if c.id != "sync" {
		return rpcError(connect.CodePermissionDenied, "PLUGIN_SCOPE_DENIED")
	}
	_, err := c.mapping(scope)
	return err
}
func (c *pluginCore) GetMapping(_ context.Context, scope *ppb.Scope) (*ppb.Mapping, error) {
	return c.mapping(scope)
}
func (c *pluginCore) ListMappings(ctx context.Context, scope *ppb.Scope) (*ppb.MappingList, error) {
	if !proto.Equal(scope, &ppb.Scope{}) {
		return nil, rpcError(connect.CodePermissionDenied, "PLUGIN_SCOPE_DENIED")
	}
	return c.resources.service.ListMappings(ctx, &apb.ListMappingsRequest{})
}
func (c *pluginCore) GetProject(ctx context.Context, scope *ppb.Scope) (*pb.Project, error) {
	if err := c.authorize(ctx, scope); err != nil {
		return nil, err
	}
	client, err := c.RemoteCore.Resolve(ctx, scope)
	if err != nil {
		return nil, err
	}
	return client.GetProject(FreshRemoteContext(ctx), &pb.GetProjectRequest{Id: scope.ProjectId})
}
func (c *pluginCore) GetConfig(_ context.Context, _ *ppb.ConfigRequest) (*ppb.PluginConfig, error) {
	manager := c.resources.service.plugins.Load()
	if manager == nil {
		return nil, unavailableWorker()
	}
	values, revision, err := manager.Settings(c.id)
	if err != nil {
		return nil, err
	}
	return &ppb.PluginConfig{Values: values, Revision: revision}, nil
}
func (c *pluginCore) Log(_ context.Context, record *pb.LogRecord) (*pb.Empty, error) {
	manager := c.resources.service.plugins.Load()
	if manager == nil {
		return nil, unavailableWorker()
	}
	if err := manager.RecordLog(c.id, record); err != nil {
		return nil, err
	}
	return &pb.Empty{}, nil
}
func (c *pluginCore) PublishEvent(ctx context.Context, request *ppb.PublishEventRequest) (*pb.Empty, error) {
	if request == nil || request.Event == nil || request.ExpectedHostInstanceId != c.resources.service.Registry.InstanceID() {
		return nil, rpcError(connect.CodeFailedPrecondition, "OLD_PLUGIN_EVENT")
	}
	event := request.Event
	mapping, err := c.mapping(event.Scope)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if event.EventId == "" || len(event.EventId) > 128 {
		return nil, rpcError(connect.CodeInvalidArgument, "INVALID_PLUGIN_EVENT")
	}
	// Resource mutation and publication share a local commit boundary. An
	// event may have passed its first scope check before removal/revision change.
	// Nothing in this critical section calls a worker or waits for the network.
	c.resources.mu.Lock()
	defer c.resources.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c.resources.ctx.Err() != nil {
		return nil, rpcError(connect.CodeUnavailable, "AGENT_STOPPING")
	}
	current, err := c.mapping(event.Scope)
	if err != nil {
		return nil, err
	}
	if current.Revision != mapping.Revision {
		return nil, rpcError(connect.CodeAborted, "STALE_SYNC_EVENT")
	}
	switch payload := event.Payload.(type) {
	case *ppb.Event_SyncState:
		if event.Type != "sync" || payload.SyncState == nil || payload.SyncState.MappingId != mapping.Id || payload.SyncState.MappingRevision != mapping.Revision {
			return nil, rpcError(connect.CodeAborted, "STALE_SYNC_EVENT")
		}
		switch payload.SyncState.State {
		case "paused", "reconciling", "active", "conflicted", "offline", "auth_required", "error":
			c.resources.syncStates.Store(mapping.Id, proto.Clone(payload.SyncState).(*ppb.SyncState))
		default:
			return nil, rpcError(connect.CodeInvalidArgument, "INVALID_SYNC_STATE")
		}
	case *ppb.Event_Conflict:
		if event.Type != "conflict" || payload.Conflict == nil || payload.Conflict.MappingId != mapping.Id {
			return nil, rpcError(connect.CodeInvalidArgument, "INVALID_CONFLICT_EVENT")
		}
	default:
		return nil, rpcError(connect.CodeInvalidArgument, "UNSUPPORTED_PLUGIN_EVENT")
	}
	c.resources.publish(event.Type, mapping.Id, mapping.ServerId, mapping.Revision)
	return &pb.Empty{}, nil
}

func (c *pluginCore) SubscribeEvents(ctx context.Context, request *ppb.SubscribeRequest, out prpc.CoreAPISubscribeEventsServerStream) error {
	if request == nil || !proto.Equal(request.Scope, &ppb.Scope{}) {
		return rpcError(connect.CodePermissionDenied, "GLOBAL_PLUGIN_SUBSCRIPTION_REQUIRED")
	}
	accepted := map[string]bool{"rescan_required": true}
	if len(request.EventTypes) == 0 {
		accepted["mapping"], accepted["server"] = true, true
	}
	for _, kind := range request.EventTypes {
		if kind != "mapping" && kind != "server" {
			return rpcError(connect.CodeInvalidArgument, "UNSUPPORTED_EVENT_TYPE")
		}
		accepted[kind] = true
	}
	subscription, stop := c.resources.service.Events.subscribe()
	defer stop()
	for {
		event, err := subscription.receive(ctx)
		if err != nil {
			return err
		}
		if !accepted[event.Type] {
			continue
		}
		scope := &ppb.Scope{ServerId: event.ServerId}
		if event.Type == "mapping" {
			scope.MappingId = event.ResourceId
		}
		message := &ppb.Event{EventId: fmt.Sprintf("%s_%d", event.InstanceId, event.Sequence), Type: event.Type, Scope: scope, Timestamp: time.Now().Unix(), SourceInstanceId: event.InstanceId, Payload: &ppb.Event_RescanRequired{RescanRequired: true}}
		if err := out.Send(message); err != nil {
			return err
		}
	}
}
func (s *Service) activeSyncTasks() uint32 {
	resources := s.resources.Load()
	manager := s.plugins.Load()
	if resources == nil || manager == nil {
		return 0
	}
	list, err := manager.List()
	if err != nil {
		return 0
	}
	ready := false
	for _, status := range list.Plugins {
		if status.PluginId == "sync" && status.Enabled && status.State == "ready" {
			ready = true
		}
	}
	if !ready {
		return 0
	}
	snapshot, err := resources.store.View()
	if err != nil {
		return 0
	}
	var count uint32
	resources.syncStates.Range(func(key, value any) bool {
		state, ok := value.(*ppb.SyncState)
		if !ok || state.State == "paused" {
			return true
		}
		mapping := new(ppb.Mapping)
		if snapshot.Get("mappings", key.(string), mapping) == nil && mapping.SyncEnabled && mapping.Revision == state.MappingRevision {
			count++
		}
		return true
	})
	return count
}
