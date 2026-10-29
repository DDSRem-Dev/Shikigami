package server

import (
	"context"

	"connectrpc.com/connect/v2"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	prpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin/pluginconnect"
)

// daemonPluginCore exposes only host-owned metadata. The server worker performs
// file/port operations itself; it receives no Remote* callback capability.
type daemonPluginCore struct {
	prpc.UnimplementedCoreAPIHandler
	service *Service
	id      string
}

func (c *daemonPluginCore) authorizeScope(ctx context.Context, scope *ppb.Scope) error {
	if scope == nil || scope.ServerId != "" || scope.MappingId != "" {
		return connect.NewError(connect.CodePermissionDenied, "SERVER_PLUGIN_SCOPE_REQUIRED")
	}
	if scope.ProjectId != "" {
		if _, e := c.service.Projects.Get(ctx, &pb.GetProjectRequest{Id: scope.ProjectId}); e != nil {
			return e
		}
	}
	if scope.SessionId != "" {
		if c.service.Sessions == nil {
			return connect.NewError(connect.CodeFailedPrecondition, "sessions are unavailable")
		}
		session, e := c.service.Sessions.Get(ctx, &pb.GetSessionRequest{Id: scope.SessionId})
		if e != nil {
			return e
		}
		if scope.ProjectId != "" && session.ProjectId != scope.ProjectId {
			return connect.NewError(connect.CodePermissionDenied, "SESSION_PROJECT_MISMATCH")
		}
		if _, e = c.service.Projects.Get(ctx, &pb.GetProjectRequest{Id: session.ProjectId}); e != nil {
			return e
		}
	}
	return nil
}
func (c *daemonPluginCore) GetProject(ctx context.Context, scope *ppb.Scope) (*pb.Project, error) {
	if scope.GetProjectId() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, "project_id required")
	}
	if e := c.authorizeScope(ctx, scope); e != nil {
		return nil, e
	}
	return c.service.Projects.Get(ctx, &pb.GetProjectRequest{Id: scope.ProjectId})
}
func (c *daemonPluginCore) ListProjects(ctx context.Context, scope *ppb.Scope) (*pb.ListProjectsResponse, error) {
	if e := c.authorizeScope(ctx, scope); e != nil {
		return nil, e
	}
	if scope.ProjectId != "" {
		project, e := c.service.Projects.Get(ctx, &pb.GetProjectRequest{Id: scope.ProjectId})
		if e != nil {
			return nil, e
		}
		return &pb.ListProjectsResponse{Projects: []*pb.Project{project}}, nil
	}
	if scope.SessionId != "" {
		session, e := c.service.Sessions.Get(ctx, &pb.GetSessionRequest{Id: scope.SessionId})
		if e != nil {
			return nil, e
		}
		project, e := c.service.Projects.Get(ctx, &pb.GetProjectRequest{Id: session.ProjectId})
		if e != nil {
			return nil, e
		}
		return &pb.ListProjectsResponse{Projects: []*pb.Project{project}}, nil
	}
	return c.service.Projects.List(ctx, &pb.ListProjectsRequest{})
}
func (c *daemonPluginCore) GetSession(ctx context.Context, scope *ppb.Scope) (*pb.Session, error) {
	if scope.GetSessionId() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, "session_id required")
	}
	if e := c.authorizeScope(ctx, scope); e != nil {
		return nil, e
	}
	return c.service.Sessions.Get(ctx, &pb.GetSessionRequest{Id: scope.SessionId})
}
func (c *daemonPluginCore) ListSessions(ctx context.Context, scope *ppb.Scope) (*pb.ListSessionsResponse, error) {
	if c.id == "port" {
		return c.portSessions(ctx, scope)
	}
	if e := c.authorizeScope(ctx, scope); e != nil {
		return nil, e
	}
	if c.service.Sessions == nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, "sessions are unavailable")
	}
	if scope.SessionId != "" {
		session, e := c.service.Sessions.Get(ctx, &pb.GetSessionRequest{Id: scope.SessionId})
		if e != nil {
			return nil, e
		}
		return &pb.ListSessionsResponse{Sessions: []*pb.Session{session}}, nil
	}
	if scope.ProjectId != "" {
		result, err := c.service.ListProjectSessions(ctx, &pb.ListProjectSessionsRequest{ProjectId: scope.ProjectId})
		if err != nil {
			return nil, err
		}
		return &pb.ListSessionsResponse{Sessions: result.Sessions}, nil
	}
	return c.service.Sessions.List(ctx, &pb.ListSessionsRequest{})
}
func (c *daemonPluginCore) GetConfig(ctx context.Context, request *ppb.ConfigRequest) (*ppb.PluginConfig, error) {
	if e := c.authorizeScope(ctx, request.GetScope()); e != nil {
		return nil, e
	}
	if c.service.Supervisor == nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, "plugin supervisor unavailable")
	}
	values, revision, e := c.service.Supervisor.Settings(c.id)
	if e != nil {
		return nil, e
	}
	return &ppb.PluginConfig{Values: values, Revision: revision}, nil
}
func (c *daemonPluginCore) Log(_ context.Context, record *pb.LogRecord) (*pb.Empty, error) {
	if c.service.Supervisor == nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, "plugin supervisor unavailable")
	}
	if e := c.service.Supervisor.RecordLog(c.id, record); e != nil {
		return nil, e
	}
	return &pb.Empty{}, nil
}

// Port plugins receive only fresh pane roots matched to committed session IDs.
func (c *daemonPluginCore) portSessions(ctx context.Context, scope *ppb.Scope) (*pb.ListSessionsResponse, error) {
	if e := c.authorizeScope(ctx, scope); e != nil {
		return nil, e
	}
	if c.service.Sessions == nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, "SESSIONS_UNAVAILABLE")
	}
	list, e := c.service.Sessions.List(ctx, &pb.ListSessionsRequest{})
	if e != nil {
		return nil, e
	}
	roots, e := c.service.Sessions.SampleTargets(ctx, scope.GetSessionId())
	if e != nil {
		return nil, e
	}
	verified := map[string]int32{}
	for _, root := range roots {
		verified[root.SessionID] = root.PID
	}
	out := &pb.ListSessionsResponse{}
	for _, s := range list.Sessions {
		if scope.GetSessionId() != "" && s.Id != scope.SessionId || scope.GetProjectId() != "" && s.ProjectId != scope.ProjectId {
			continue
		}
		s.ClaudePid = verified[s.Id]
		out.Sessions = append(out.Sessions, s)
	}
	return out, nil
}
