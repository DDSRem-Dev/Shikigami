package server

import (
	"context"

	"connectrpc.com/connect/v2"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
)

func (s *Service) ListProjectDirectories(ctx context.Context, request *pb.ListProjectDirectoriesRequest) (*pb.ListProjectDirectoriesResponse, error) {
	if s.Projects == nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, "project directory picker unavailable")
	}
	return s.Projects.ListDirectories(ctx, request)
}

func (s *Service) GetSessionLogs(ctx context.Context, request *pb.SessionLogsRequest) (*pb.SessionLogs, error) {
	if s.Sessions == nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, "session diagnostics unavailable")
	}
	return s.Sessions.GetLogs(ctx, request)
}
