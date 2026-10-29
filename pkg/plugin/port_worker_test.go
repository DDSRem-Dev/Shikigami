package plugin

import (
	"context"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"testing"
)

type portWorkerProbe struct{ calls int }

func (p *portWorkerProbe) StartForward(_ context.Context, s *ppb.ForwardSpec) (*ppb.ForwardState, error) {
	p.calls++
	return &ppb.ForwardState{Spec: s}, nil
}
func (p *portWorkerProbe) StopForward(context.Context, *ppb.ForwardID) error { p.calls++; return nil }
func (p *portWorkerProbe) ListForwards(context.Context) (*ppb.ForwardList, error) {
	return &ppb.ForwardList{}, nil
}
func TestPortWorkerControlRequiresMatchingForwardScope(t *testing.T) {
	p := &portWorkerProbe{}
	server := &portWorkerServer{impl: p}
	spec := &ppb.ForwardSpec{Id: "forward_one"}
	for _, md := range []metadata.MD{nil, metadata.Pairs("shiki-forward-id", "forward_two"), metadata.Pairs("shiki-forward-id", "forward_one", "shiki-forward-id", "forward_one")} {
		ctx := metadata.NewIncomingContext(context.Background(), md)
		if _, err := server.StartForward(ctx, spec); status.Code(err) != codes.PermissionDenied {
			t.Fatal(err)
		}
		if _, err := server.StopForward(ctx, &ppb.ForwardID{Id: spec.Id}); status.Code(err) != codes.PermissionDenied {
			t.Fatal(err)
		}
	}
	if p.calls != 0 {
		t.Fatal("unscoped request reached worker")
	}
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("shiki-forward-id", spec.Id))
	if _, err := server.StartForward(ctx, spec); err != nil {
		t.Fatal(err)
	}
	if _, err := server.StopForward(ctx, &ppb.ForwardID{Id: spec.Id}); err != nil {
		t.Fatal(err)
	}
	if _, err := server.ListForwards(ctx, &pb.Empty{}); err != nil {
		t.Fatal(err)
	}
	if p.calls != 2 {
		t.Fatal(p.calls)
	}
}

func (p *portWorkerProbe) DiscoverForwards(context.Context, *ppb.Scope) (*ppb.ForwardCandidates, error) {
	return &ppb.ForwardCandidates{}, nil
}
