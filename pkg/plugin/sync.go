package plugin

import (
	"connectrpc.com/connect/v2"
	"context"
	"github.com/DDSRem-Dev/Shikigami/pkg/plugin/limits"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

type syncClient struct {
	owner *lifecycleClient
	stub  ppb.SyncWorkerClient
}

func (c *syncClient) context(ctx context.Context, id string) context.Context {
	return metadata.AppendToOutgoingContext(c.owner.context(ctx), "shiki-mapping-id", id)
}
func (c *syncClient) ConfigureMapping(ctx context.Context, r *ppb.Mapping) (*ppb.SyncState, error) {
	v, e := c.stub.ConfigureMapping(c.context(ctx, r.GetId()), r)
	return v, FromGRPC(e)
}
func (c *syncClient) GetSyncState(ctx context.Context, r *ppb.Scope) (*ppb.SyncState, error) {
	v, e := c.stub.GetSyncState(c.context(ctx, r.GetMappingId()), r)
	return v, FromGRPC(e)
}
func (c *syncClient) ListConflicts(ctx context.Context, r *ppb.Scope) (*ppb.ConflictList, error) {
	v, e := c.stub.ListConflicts(c.context(ctx, r.GetMappingId()), r)
	return v, FromGRPC(e)
}
func (c *syncClient) ResolveConflict(ctx context.Context, r *ppb.ResolveConflictRequest) (*ppb.SyncState, error) {
	v, e := c.stub.ResolveConflict(c.context(ctx, r.GetMappingId()), r)
	return v, FromGRPC(e)
}
func (c *syncClient) ReadConflictFile(ctx context.Context, r *ppb.ReadConflictFileRequest) (Receiver[pb.FileData], error) {
	ctx, cancel := context.WithCancel(c.context(ctx, r.GetMappingId()))
	stream, err := c.stub.ReadConflictFile(ctx, r)
	if err != nil {
		cancel()
		return nil, FromGRPC(err)
	}
	return &receiver[pb.FileData]{receive: stream.Recv, cancel: cancel}, nil
}

type syncServer struct {
	ppb.UnimplementedSyncWorkerServer
	impl SyncWorker
}

func verifyMapping(ctx context.Context, id string) error {
	md, _ := metadata.FromIncomingContext(ctx)
	values := md.Get("shiki-mapping-id")
	if id == "" || len(values) != 1 || values[0] != id {
		return connect.NewError(connect.CodePermissionDenied, "worker mapping scope mismatch")
	}
	return nil
}
func (s *syncServer) ConfigureMapping(ctx context.Context, r *ppb.Mapping) (*ppb.SyncState, error) {
	if err := verifyMapping(ctx, r.GetId()); err != nil {
		return nil, ToGRPC(err)
	}
	v, e := s.impl.ConfigureMapping(ctx, r)
	return v, ToGRPC(e)
}
func (s *syncServer) GetSyncState(ctx context.Context, r *ppb.Scope) (*ppb.SyncState, error) {
	if err := verifyMapping(ctx, r.GetMappingId()); err != nil {
		return nil, ToGRPC(err)
	}
	v, e := s.impl.GetSyncState(ctx, r)
	return v, ToGRPC(e)
}
func (s *syncServer) ListConflicts(ctx context.Context, r *ppb.Scope) (*ppb.ConflictList, error) {
	if err := verifyMapping(ctx, r.GetMappingId()); err != nil {
		return nil, ToGRPC(err)
	}
	v, e := s.impl.ListConflicts(ctx, r)
	return v, ToGRPC(e)
}
func (s *syncServer) ResolveConflict(ctx context.Context, r *ppb.ResolveConflictRequest) (*ppb.SyncState, error) {
	if err := verifyMapping(ctx, r.GetMappingId()); err != nil {
		return nil, ToGRPC(err)
	}
	v, e := s.impl.ResolveConflict(ctx, r)
	return v, ToGRPC(e)
}
func (s *syncServer) ReadConflictFile(r *ppb.ReadConflictFileRequest, out grpc.ServerStreamingServer[pb.FileData]) error {
	if err := verifyMapping(out.Context(), r.GetMappingId()); err != nil {
		return ToGRPC(err)
	}
	return ToGRPC(s.impl.ReadConflictFile(out.Context(), r, func(frame *pb.FileData) error {
		if len(frame.GetChunk()) > limits.MaxDataBytes {
			return connect.NewError(connect.CodeResourceExhausted, "file chunk too large")
		}
		return FromGRPC(out.Send(frame))
	}))
}
