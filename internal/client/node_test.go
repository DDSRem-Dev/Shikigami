package client

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	arpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent/agentconnect"
)

type nodeProbe struct {
	arpc.UnimplementedNodeControlHandler
	restarted atomic.Bool
}

func (p *nodeProbe) Negotiate(context.Context, *pb.ProtocolOffer) (*apb.NodeNegotiation, error) {
	return &apb.NodeNegotiation{Protocol: &pb.ProtocolAgreement{Major: 1, Capabilities: []string{"node.login.v1"}}, InstanceId: "node-instance"}, nil
}
func (p *nodeProbe) GetStatus(ctx context.Context, _ *pb.Empty) (*apb.NodeStatus, error) {
	info, _ := connect.CallInfoForServerContext(ctx)
	header := info.RequestHeader()
	if header.Get("Shiki-Node-Control-Instance-Id") != "node-instance" || header.Get("Authorization") != "" || header.Get(frontendHeader) != "" || header.Get("X-Secret") != "" {
		return nil, connect.NewError(connect.CodePermissionDenied, "header isolation failed")
	}
	if p.restarted.Load() {
		return nil, connect.NewError(connect.CodeFailedPrecondition, "NODE_CONTROL_RESTARTED")
	}
	return &apb.NodeStatus{State: "needs_login"}, nil
}
func TestNodeControlIndependentNegotiationAndHeaders(t *testing.T) {
	root, e := os.MkdirTemp("/tmp", "shiki-node-cli-")
	if e != nil {
		t.Fatal(e)
	}
	defer os.RemoveAll(root)
	socket := filepath.Join(root, "node.sock")
	listener, e := connection.ListenUnix(socket, uint32(os.Getuid()))
	if e != nil {
		t.Fatal(e)
	}
	p := &nodeProbe{}
	server := connect.NewServer()
	arpc.RegisterNodeControlHandler(server, p)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	hs := connection.NewHTTPServer(ctx, connection.NewRPCHandler(server))
	done := make(chan error, 1)
	go func() { done <- hs.Serve(listener) }()
	defer func() { hs.Close(); <-done }()
	// There is deliberately no Agent service, application token, or online node.
	node, e := OpenNode(ctx, socket, time.Second)
	if e != nil {
		t.Fatal(e)
	}
	defer node.Close()
	call, info := connect.NewClientContext(ctx)
	info.RequestHeader().Set("Authorization", "Bearer caller-secret")
	info.RequestHeader().Set(frontendHeader, "caller-frontend")
	info.RequestHeader().Set("X-Secret", "caller-secret")
	status, e := node.API().GetStatus(call, &pb.Empty{})
	if e != nil || status.State != "needs_login" {
		t.Fatalf("pre-login control: %v %v", status, e)
	}
	p.restarted.Store(true)
	if _, e = node.API().GetStatus(call, &pb.Empty{}); connect.CodeOf(e) != connect.CodeFailedPrecondition {
		t.Fatalf("restart identity was not enforced: %v", e)
	}
}
