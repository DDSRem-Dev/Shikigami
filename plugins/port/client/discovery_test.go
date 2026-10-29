package client

import (
	"context"
	"strings"
	"testing"

	"connectrpc.com/connect/v2"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin/pluginconnect"
	"google.golang.org/protobuf/types/known/structpb"
)

type discoveryCore struct {
	rpc.CoreAPIClient
	ports  []*pb.OpenPort
	err    error
	checks []int32
}

func (c *discoveryCore) RemoteListPorts(context.Context, *ppb.RemoteListPortsRequest) (*pb.ListPortsResponse, error) {
	return &pb.ListPortsResponse{Ports: c.ports}, c.err
}
func (c *discoveryCore) RemoteCheckPort(_ context.Context, r *ppb.RemoteCheckPortRequest) (*pb.CheckPortResponse, error) {
	c.checks = append(c.checks, r.Request.Port)
	if r.Request.Port == 3004 {
		return nil, connect.NewError(connect.CodePermissionDenied, "server denied")
	}
	return &pb.CheckPortResponse{Port: &pb.OpenPort{SessionId: r.Request.SessionId, SourceIdentity: r.Request.SourceIdentity}}, nil
}
func TestDiscoveryRequiresExplicitPolicyOwnershipAndNoBinding(t *testing.T) {
	core := &discoveryCore{}
	for _, port := range []int32{3000, 3001, 3002, 3003, 3004, 9527} {
		core.ports = append(core.ports, &pb.OpenPort{Port: port, Pid: 1, Protocol: "tcp", BindAddress: "127.0.0.1", SessionId: "sess_verified", SourceIdentity: strings.Repeat("a", 64)})
	}
	core.ports[1].SessionId = ""
	core.ports[2].SourceIdentity = ""
	w := New()
	config, _ := structpb.NewStruct(map[string]any{"auto_forward": true, "auto_allow_ports": []any{3000.0, 3001.0, 3002.0, 3004.0, 9527.0}})
	if e := w.Init(t.Context(), sdk.InitContext{Core: core, Side: "client", StateDir: t.TempDir(), Config: config}); e != nil {
		t.Fatal(e)
	}
	result, e := w.DiscoverForwards(t.Context(), &ppb.Scope{ServerId: "srv_fixture"})
	if e != nil || len(result.Specs) != 1 || result.Specs[0].RemotePort != 3000 || result.Specs[0].Id != "" || result.Specs[0].Revision != "" {
		t.Fatal(result, e)
	}
	if len(core.checks) != 2 || core.checks[0] != 3000 || core.checks[1] != 3004 {
		t.Fatal("unsafe endpoints reached authorization", core.checks)
	}
	states, e := w.ListForwards(t.Context())
	if e != nil || len(states.Forwards) != 0 {
		t.Fatal("discovery bound without durable host authorization", states, e)
	}
	core.err = connect.NewError(connect.CodeUnavailable, "scanner failed")
	if _, e = w.DiscoverForwards(t.Context(), &ppb.Scope{ServerId: "srv_fixture"}); connect.CodeOf(e) != connect.CodeUnavailable {
		t.Fatal("scan failure became empty candidates", e)
	}
}
