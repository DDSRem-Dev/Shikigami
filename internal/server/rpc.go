// Package server composes authenticated remote RPCs. Local agent and NodeControl
// services are mounted on separate private listeners.
package server

import (
	"context"
	"net/http"
	"runtime"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/auth"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	"github.com/DDSRem-Dev/Shikigami/internal/plugin"
	"github.com/DDSRem-Dev/Shikigami/internal/server/monitor"
	"github.com/DDSRem-Dev/Shikigami/internal/server/project"
	"github.com/DDSRem-Dev/Shikigami/internal/server/session"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
	"github.com/DDSRem-Dev/Shikigami/pkg/protocol/version"
)

type Service struct {
	plugin.DaemonDataPlane
	Monitor          *monitor.Monitor
	Auth             *auth.Manager
	Projects         *project.Manager
	Sessions         *session.Manager
	Build            string
	StartedAt        time.Time
	ClaudeExecutable string
	Health           func() *pb.HealthStatus
	fileCalls        fileAdmission
	portCalls        portAdmission
}

func (s *Service) Handler() http.Handler {
	server := connect.NewServer(s.Auth.Interceptor(), s.fileCalls.interceptor(), s.portCalls.interceptor())
	rpc.RegisterShikiDaemonHandler(server, s)
	return connection.NewRPCHandler(server, s.Auth.HTTPMiddleware, firstRequestTransport, terminalTransport, plugin.WithStreamCancellation)
}

func (s *Service) NegotiateProtocol(ctx context.Context, offer *pb.ProtocolOffer) (*pb.ProtocolAgreement, error) {
	claims, ok := auth.FromContext(ctx)
	if !ok || claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, "authenticated negotiation required")
	}
	agreement, err := version.Negotiate(&pb.ProtocolOffer{Major: 1, Capabilities: s.capabilities()}, offer, s.Build)
	if err != nil {
		return nil, err
	}
	if s.Auth == nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, "daemon identity unavailable")
	}
	agreement.DaemonId, err = s.Auth.DaemonID()
	if err != nil {
		return nil, err
	}
	agreement.GrantedPermissions = claims.GrantedPermissions()
	return agreement, nil
}
func (s *Service) CreateProject(ctx context.Context, request *pb.CreateProjectRequest) (*pb.Project, error) {
	return s.Projects.Create(ctx, request)
}
func (s *Service) GetProject(ctx context.Context, request *pb.GetProjectRequest) (*pb.Project, error) {
	return s.Projects.Get(ctx, request)
}
func (s *Service) ListProjects(ctx context.Context, request *pb.ListProjectsRequest) (*pb.ListProjectsResponse, error) {
	return s.Projects.List(ctx, request)
}
func (s *Service) DeleteProject(ctx context.Context, request *pb.DeleteProjectRequest) (*pb.Empty, error) {
	return s.Projects.Delete(ctx, request)
}
func (s *Service) CreateSession(ctx context.Context, request *pb.CreateSessionRequest) (*pb.Session, error) {
	return s.Sessions.Create(ctx, request)
}
func (s *Service) GetSession(ctx context.Context, request *pb.GetSessionRequest) (*pb.Session, error) {
	return s.Sessions.Get(ctx, request)
}
func (s *Service) ListSessions(ctx context.Context, request *pb.ListSessionsRequest) (*pb.ListSessionsResponse, error) {
	return s.Sessions.List(ctx, request)
}
func (s *Service) UpdateSession(ctx context.Context, request *pb.UpdateSessionRequest) (*pb.Session, error) {
	return s.Sessions.Update(ctx, request)
}
func (s *Service) DeleteSession(ctx context.Context, request *pb.DeleteSessionRequest) (*pb.Empty, error) {
	return s.Sessions.Delete(ctx, request)
}
func (s *Service) StopClaude(ctx context.Context, request *pb.SessionActionRequest) (*pb.Session, error) {
	return s.Sessions.StopClaude(ctx, request)
}
func (s *Service) RestartClaude(ctx context.Context, request *pb.SessionActionRequest) (*pb.Session, error) {
	return s.Sessions.RestartClaude(ctx, request)
}

func (s *Service) ListProjectSessions(ctx context.Context, request *pb.ListProjectSessionsRequest) (*pb.ListProjectSessionsResponse, error) {
	if request == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, "project_id required")
	}
	if _, err := s.Projects.Get(ctx, &pb.GetProjectRequest{Id: request.ProjectId}); err != nil {
		return nil, err
	}
	all, err := s.Sessions.List(ctx, &pb.ListSessionsRequest{})
	if err != nil {
		return nil, err
	}
	result := new(pb.ListProjectSessionsResponse)
	for _, session := range all.Sessions {
		if session.ProjectId == request.ProjectId {
			result.Sessions = append(result.Sessions, session)
		}
	}
	return result, nil
}

func (s *Service) GetSystemInfo(context.Context, *pb.Empty) (*pb.SystemInfo, error) {
	id, err := s.Auth.DaemonID()
	if err != nil {
		return nil, err
	}
	uptime := int64(0)
	if !s.StartedAt.IsZero() {
		uptime = int64(time.Since(s.StartedAt).Seconds())
	}
	return &pb.SystemInfo{Os: runtime.GOOS, Arch: runtime.GOARCH, CpuCores: int32(runtime.NumCPU()), DaemonVersion: s.Build, ProtocolVersion: "1.0", Capabilities: s.capabilities(), DaemonId: id, Uptime: uptime}, nil
}
func (s *Service) HealthCheck(context.Context, *pb.Empty) (*pb.HealthStatus, error) {
	if s.Health != nil {
		return s.Health(), nil
	}
	return &pb.HealthStatus{Status: pb.HealthStatus_DEGRADED, Message: "core available; plugin and monitoring integration pending"}, nil
}
