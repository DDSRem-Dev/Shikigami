package server

import (
	"context"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
)

func (s *Service) CreateSnapshot(ctx context.Context, request *pb.CreateSnapshotRequest) (*pb.Snapshot, error) {
	return s.Sessions.CreateSnapshot(ctx, request)
}
func (s *Service) ListSnapshots(ctx context.Context, request *pb.ListSnapshotsRequest) (*pb.ListSnapshotsResponse, error) {
	return s.Sessions.ListSnapshots(ctx, request)
}
func (s *Service) RestoreSnapshot(ctx context.Context, request *pb.RestoreSnapshotRequest) (*pb.Session, error) {
	return s.Sessions.RestoreSnapshot(ctx, request)
}
func (s *Service) DeleteSnapshot(ctx context.Context, request *pb.DeleteSnapshotRequest) (*pb.Empty, error) {
	return s.Sessions.DeleteSnapshot(ctx, request)
}
