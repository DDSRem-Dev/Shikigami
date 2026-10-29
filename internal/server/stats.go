package server

import (
	"connectrpc.com/connect/v2"
	"context"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
)

func (s *Service) GetStats(ctx context.Context, request *pb.StatsRequest) (*pb.StatsSnapshot, error) {
	if s.Monitor == nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, "resource monitoring is unavailable")
	}
	return s.Monitor.Snapshot(ctx, request)
}
func (s *Service) WatchStats(ctx context.Context, request *pb.StatsRequest, stream rpc.ShikiDaemonWatchStatsServerStream) error {
	if s.Monitor == nil {
		return connect.NewError(connect.CodeFailedPrecondition, "resource monitoring is unavailable")
	}
	return s.Monitor.Watch(ctx, request, stream.Send)
}
