package agent

import (
	"context"
	"errors"
	"io"
	"strconv"

	"connectrpc.com/connect/v2"
	host "github.com/DDSRem-Dev/Shikigami/internal/plugin"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	arpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent/agentconnect"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

func (s *Service) pluginTarget(ctx context.Context, target *apb.PluginTarget) (*host.Supervisor, rpc.ShikiDaemonClient, error) {
	if target == nil {
		return nil, nil, rpcError(connect.CodeInvalidArgument, "PLUGIN_TARGET_REQUIRED")
	}
	r, err := s.requireResources()
	if err != nil {
		return nil, nil, err
	}
	switch target.Side {
	case apb.PluginTarget_LOCAL:
		if target.ServerId != "" {
			return nil, nil, rpcError(connect.CodeInvalidArgument, "LOCAL_PLUGIN_CANNOT_SPECIFY_SERVER")
		}
		manager := s.plugins.Load()
		if manager == nil {
			return nil, nil, unavailableWorker()
		}
		return manager, nil, nil
	case apb.PluginTarget_REMOTE:
		if target.ServerId == "" {
			return nil, nil, rpcError(connect.CodeInvalidArgument, "REMOTE_PLUGIN_REQUIRES_SERVER")
		}
		client, err := r.Resolve(ctx, target.ServerId)
		if err != nil {
			return nil, nil, err
		}
		return nil, rpc.NewShikiDaemonClient(client), nil
	default:
		return nil, nil, rpcError(connect.CodeInvalidArgument, "PLUGIN_SIDE_REQUIRED")
	}
}
func (s *Service) ListPlugins(ctx context.Context, target *apb.PluginTarget) (*pb.ListPluginsResponse, error) {
	local, remote, err := s.pluginTarget(ctx, target)
	if err != nil {
		return nil, err
	}
	if local != nil {
		return local.List()
	}
	result, err := remote.ListPlugins(FreshRemoteContext(ctx), &pb.ListPluginsRequest{})
	return result, safeRemoteError(err)
}
func (s *Service) ManagePlugin(ctx context.Context, request *apb.ManagePluginRequest) (*pb.PluginStatus, error) {
	if request == nil || request.Request == nil {
		return nil, rpcError(connect.CodeInvalidArgument, "PLUGIN_REQUEST_REQUIRED")
	}
	local, remote, err := s.pluginTarget(ctx, request.Target)
	if err != nil {
		return nil, err
	}
	if local != nil {
		return local.Manage(ctx, request.Request)
	}
	result, err := remote.ManagePlugin(FreshRemoteContext(ctx), request.Request)
	return result, safeRemoteError(err)
}
func (s *Service) WatchPluginLogs(ctx context.Context, request *apb.PluginLogsRequest, out arpc.AgentWatchPluginLogsServerStream) error {
	if request == nil || request.Request == nil {
		return rpcError(connect.CodeInvalidArgument, "PLUGIN_REQUEST_REQUIRED")
	}
	local, remote, err := s.pluginTarget(ctx, request.Target)
	if err != nil {
		return err
	}
	if local != nil {
		return local.WatchLogs(ctx, request.Request, out.Send)
	}
	stream, err := remote.WatchPluginLogs(FreshRemoteContext(ctx), request.Request)
	if err != nil {
		return safeRemoteError(err)
	}
	defer stream.Close()
	for {
		message, err := stream.Receive()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return safeRemoteError(err)
		}
		if err := out.Send(message); err != nil {
			return err
		}
	}
}
func (s *Service) syncCall(ctx context.Context, scope *ppb.Scope, fn func(context.Context, sdk.SyncClient) error) error {
	r, err := s.requireResources()
	if err != nil {
		return err
	}
	core := &pluginCore{resources: r, id: "sync"}
	if err := core.authorize(ctx, scope); err != nil {
		return err
	}
	manager := s.plugins.Load()
	if manager == nil {
		return unavailableWorker()
	}
	return manager.Call(ctx, "sync", sdk.SyncCapability, func(call context.Context, lifecycle sdk.Lifecycle) error { return fn(call, lifecycle.Sync()) })
}
func (s *Service) scopeForMapping(id string) (*ppb.Scope, error) {
	r, err := s.requireResources()
	if err != nil {
		return nil, err
	}
	snapshot, err := r.store.View()
	if err != nil {
		return nil, stateError(err)
	}
	mapping := new(ppb.Mapping)
	if err := snapshot.Get("mappings", id, mapping); err != nil {
		return nil, rpcError(connect.CodeNotFound, "MAPPING_NOT_FOUND")
	}
	return &ppb.Scope{ServerId: mapping.ServerId, ProjectId: mapping.ProjectId, MappingId: mapping.Id}, nil
}
func (s *Service) GetSyncState(ctx context.Context, scope *ppb.Scope) (result *ppb.SyncState, err error) {
	err = s.syncCall(ctx, scope, func(call context.Context, worker sdk.SyncClient) error {
		result, err = worker.GetSyncState(call, scope)
		return err
	})
	return
}
func (s *Service) ListConflicts(ctx context.Context, scope *ppb.Scope) (result *ppb.ConflictList, err error) {
	err = s.syncCall(ctx, scope, func(call context.Context, worker sdk.SyncClient) error {
		result, err = worker.ListConflicts(call, scope)
		return err
	})
	return
}
func (s *Service) ResolveConflict(ctx context.Context, request *ppb.ResolveConflictRequest) (result *ppb.SyncState, err error) {
	if request == nil {
		return nil, rpcError(connect.CodeInvalidArgument, "CONFLICT_REQUEST_REQUIRED")
	}
	scope, err := s.scopeForMapping(request.MappingId)
	if err != nil {
		return nil, err
	}
	err = s.syncCall(ctx, scope, func(call context.Context, worker sdk.SyncClient) error {
		result, err = worker.ResolveConflict(call, request)
		return err
	})
	return
}
func (s *Service) ReadConflictFile(ctx context.Context, request *ppb.ReadConflictFileRequest, out arpc.AgentReadConflictFileServerStream) error {
	if request == nil {
		return rpcError(connect.CodeInvalidArgument, "CONFLICT_REQUEST_REQUIRED")
	}
	scope, err := s.scopeForMapping(request.MappingId)
	if err != nil {
		return err
	}
	return s.syncCall(ctx, scope, func(call context.Context, worker sdk.SyncClient) error {
		stream, err := worker.ReadConflictFile(call, request)
		if err != nil {
			return err
		}
		defer stream.Close()
		for {
			message, err := stream.Receive()
			if errors.Is(err, io.EOF) {
				return nil
			}
			if err != nil {
				return err
			}
			if err := out.Send(message); err != nil {
				return err
			}
		}
	})
}

// InvokePluginAction implements AgentActionsHandler. It decodes the struct
// payload, routes to the local plugin supervisor, and returns the action result
// struct. Remote-side invocation is not yet supported.
func (s *Service) InvokePluginAction(ctx context.Context, req *structpb.Struct) (*structpb.Struct, error) {
	if req == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, "ACTION_REQUEST_REQUIRED")
	}
	fields := req.GetFields()

	pluginID := ""
	if v, ok := fields["plugin_id"]; ok {
		pluginID = v.GetStringValue()
	}
	if pluginID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, "PLUGIN_ID_REQUIRED")
	}

	side := ""
	if v, ok := fields["side"]; ok {
		side = v.GetStringValue()
	}
	if side == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, "PLUGIN_SIDE_REQUIRED")
	}

	if command, ok := fields["command_name"]; ok {
		if side == "remote" {
			return nil, connect.NewError(connect.CodeUnimplemented, "remote plugin action invocation not yet supported")
		}
		if side != "local" || command.GetStringValue() == "" {
			return nil, connect.NewError(connect.CodeInvalidArgument, "PLUGIN_COMMAND_REQUIRED")
		}
		manager := s.plugins.Load()
		if manager == nil {
			return nil, unavailableWorker()
		}
		manifest := manager.PluginManifest(pluginID)
		if manifest == nil {
			return nil, connect.NewError(connect.CodeFailedPrecondition, "PLUGIN_MANIFEST_UNAVAILABLE")
		}
		var declaration *sdk.UICLICommand
		for i := range manifest.UI.CLICommands {
			if manifest.UI.CLICommands[i].Name == command.GetStringValue() {
				declaration = &manifest.UI.CLICommands[i]
				break
			}
		}
		if declaration == nil {
			return nil, connect.NewError(connect.CodeNotFound, "PLUGIN_COMMAND_NOT_FOUND")
		}
		if actionID := fields["action_id"].GetStringValue(); actionID != "" && actionID != declaration.ActionID {
			return nil, connect.NewError(connect.CodeInvalidArgument, "PLUGIN_COMMAND_ACTION_MISMATCH")
		}
		req = proto.Clone(req).(*structpb.Struct)
		req.Fields["action_id"] = structpb.NewStringValue(declaration.ActionID)
		if err := validateCommandFlags(req, declaration); err != nil {
			return nil, err
		}
	}
	actionReq, err := sdk.UnmarshalActionRequest(req)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err.Error())
	}

	switch side {
	case "local":
		manager := s.plugins.Load()
		if manager == nil {
			return nil, unavailableWorker()
		}
		var result *sdk.ActionResult
		if callErr := manager.Call(ctx, pluginID, sdk.UIActionCapability, func(callCtx context.Context, lc sdk.Lifecycle) error {
			result, err = lc.InvokeAction(callCtx, actionReq)
			return err
		}); callErr != nil {
			return nil, callErr
		}
		out, marshalErr := sdk.MarshalActionResult(result)
		if marshalErr != nil {
			return nil, connect.NewError(connect.CodeInternal, marshalErr.Error())
		}
		return out, nil
	case "remote":
		return nil, connect.NewError(connect.CodeUnimplemented, "remote plugin action invocation not yet supported")
	default:
		return nil, connect.NewError(connect.CodeInvalidArgument, "PLUGIN_SIDE_REQUIRED")
	}
}

func validateCommandFlags(request *structpb.Struct, command *sdk.UICLICommand) error {
	value, present := request.GetFields()["flags"]
	flags := value.GetStructValue()
	if present && flags == nil {
		return connect.NewError(connect.CodeInvalidArgument, "INVALID_PLUGIN_COMMAND_FLAG")
	}
	if flags == nil {
		flags = &structpb.Struct{Fields: map[string]*structpb.Value{}}
		request.Fields["flags"] = structpb.NewStructValue(flags)
	}
	for name, value := range flags.GetFields() {
		var declaration *sdk.UICLIFlag
		for i := range command.Flags {
			if command.Flags[i].Name == name {
				declaration = &command.Flags[i]
				break
			}
		}
		text, ok := value.GetKind().(*structpb.Value_StringValue)
		if declaration == nil || !ok {
			return connect.NewError(connect.CodeInvalidArgument, "INVALID_PLUGIN_COMMAND_FLAG")
		}
		switch declaration.Type {
		case "bool":
			if _, err := strconv.ParseBool(text.StringValue); err != nil {
				return connect.NewError(connect.CodeInvalidArgument, "INVALID_PLUGIN_COMMAND_FLAG")
			}
		case "int":
			if _, err := strconv.ParseInt(text.StringValue, 10, 64); err != nil {
				return connect.NewError(connect.CodeInvalidArgument, "INVALID_PLUGIN_COMMAND_FLAG")
			}
		}
	}
	for _, flag := range command.Flags {
		if _, present := flags.Fields[flag.Name]; !present {
			value := flag.Default
			if value == "" {
				switch flag.Type {
				case "bool":
					value = "false"
				case "int":
					value = "0"
				}
			}
			flags.Fields[flag.Name] = structpb.NewStringValue(value)
		}
	}
	return nil
}
