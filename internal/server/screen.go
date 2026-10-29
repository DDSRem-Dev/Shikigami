package server

import (
	"connectrpc.com/connect/v2"
	"context"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
)

func (s *Service) GetSessionScreen(ctx context.Context, request *pb.GetSessionRequest) (*pb.SessionScreen, error) {
	if s.Sessions == nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, "session screen capture unavailable")
	}
	return s.Sessions.GetScreen(ctx, request)
}
