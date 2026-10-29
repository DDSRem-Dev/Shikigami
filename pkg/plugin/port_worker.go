package plugin

import (
	"connectrpc.com/connect/v2"
	"context"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	"google.golang.org/grpc/metadata"
)

type portWorkerClient struct {
	owner *lifecycleClient
	stub  ppb.PortWorkerClient
}

func (c *portWorkerClient) context(ctx context.Context, id string) context.Context {
	return metadata.AppendToOutgoingContext(c.owner.context(ctx), "shiki-forward-id", id)
}
func (c *portWorkerClient) StartForward(ctx context.Context, r *ppb.ForwardSpec) (*ppb.ForwardState, error) {
	v, e := c.stub.StartForward(c.context(ctx, r.GetId()), r)
	return v, FromGRPC(e)
}
func (c *portWorkerClient) StopForward(ctx context.Context, r *ppb.ForwardID) error {
	_, e := c.stub.StopForward(c.context(ctx, r.GetId()), r)
	return FromGRPC(e)
}
func (c *portWorkerClient) ListForwards(ctx context.Context) (*ppb.ForwardList, error) {
	v, e := c.stub.ListForwards(c.owner.context(ctx), &pb.Empty{})
	return v, FromGRPC(e)
}

type portWorkerServer struct {
	ppb.UnimplementedPortWorkerServer
	impl PortWorker
}

func verifyForward(ctx context.Context, id string) error {
	md, _ := metadata.FromIncomingContext(ctx)
	values := md.Get("shiki-forward-id")
	if id == "" || len(values) != 1 || values[0] != id {
		return connect.NewError(connect.CodePermissionDenied, "worker forward scope mismatch")
	}
	return nil
}
func (s *portWorkerServer) StartForward(ctx context.Context, r *ppb.ForwardSpec) (*ppb.ForwardState, error) {
	if e := verifyForward(ctx, r.GetId()); e != nil {
		return nil, ToGRPC(e)
	}
	v, e := s.impl.StartForward(ctx, r)
	return v, ToGRPC(e)
}
func (s *portWorkerServer) StopForward(ctx context.Context, r *ppb.ForwardID) (*pb.Empty, error) {
	if e := verifyForward(ctx, r.GetId()); e != nil {
		return nil, ToGRPC(e)
	}
	e := s.impl.StopForward(ctx, r)
	return &pb.Empty{}, ToGRPC(e)
}
func (s *portWorkerServer) ListForwards(ctx context.Context, _ *pb.Empty) (*ppb.ForwardList, error) {
	v, e := s.impl.ListForwards(ctx)
	return v, ToGRPC(e)
}

func (c *portWorkerClient) DiscoverForwards(ctx context.Context, r *ppb.Scope) (*ppb.ForwardCandidates, error) {
	v, e := c.stub.DiscoverForwards(c.owner.context(ctx), r)
	return v, FromGRPC(e)
}
func (s *portWorkerServer) DiscoverForwards(ctx context.Context, r *ppb.Scope) (*ppb.ForwardCandidates, error) {
	v, e := s.impl.DiscoverForwards(ctx, r)
	return v, ToGRPC(e)
}
