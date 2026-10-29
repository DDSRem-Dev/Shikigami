package server

import (
	"context"
	"crypto/rand"
	"os"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	"github.com/DDSRem-Dev/Shikigami/internal/node"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	"github.com/DDSRem-Dev/Shikigami/pkg/protocol/version"
)

// Keep the local management socket useful for an explicitly TLS-only daemon.
// It advertises no login capability while embedded Tailscale is disabled.
type disabledNodeControl struct{ instance, build string }

func newDisabledNodeControl(build string) *disabledNodeControl {
	return &disabledNodeControl{instance: "node_" + rand.Text(), build: build}
}
func (c *disabledNodeControl) authorize(ctx context.Context, handshake bool) error {
	uid, ok := connection.PeerUID(ctx)
	if !ok || uid != uint32(os.Geteuid()) {
		return connect.NewError(connect.CodePermissionDenied, "IPC_UID_REQUIRED")
	}
	info, ok := connect.CallInfoForServerContext(ctx)
	if !ok || len(info.RequestHeader().Values("Authorization")) > 0 {
		return connect.NewError(connect.CodePermissionDenied, "LOCAL_CONTROL_REQUIRED")
	}
	if !handshake {
		values := info.RequestHeader().Values(node.NodeInstanceHeader)
		if len(values) != 1 || values[0] != c.instance {
			return connect.NewError(connect.CodeFailedPrecondition, "NODE_CONTROL_RESTARTED")
		}
	}
	return nil
}
func (c *disabledNodeControl) Negotiate(ctx context.Context, offer *pb.ProtocolOffer) (*apb.NodeNegotiation, error) {
	if err := c.authorize(ctx, true); err != nil {
		return nil, err
	}
	agreement, err := version.Negotiate(&pb.ProtocolOffer{Major: 1}, offer, c.build)
	if err != nil {
		return nil, err
	}
	return &apb.NodeNegotiation{Protocol: agreement, InstanceId: c.instance}, nil
}
func (c *disabledNodeControl) GetStatus(ctx context.Context, _ *pb.Empty) (*apb.NodeStatus, error) {
	if err := c.authorize(ctx, false); err != nil {
		return nil, err
	}
	return &apb.NodeStatus{State: "offline", ReasonCode: "NODE_DISABLED"}, nil
}
func (c *disabledNodeControl) BeginLogin(ctx context.Context, _ *apb.BeginLoginRequest) (*apb.NodeStatus, error) {
	if err := c.authorize(ctx, false); err != nil {
		return nil, err
	}
	return nil, connect.NewError(connect.CodeFailedPrecondition, "NODE_DISABLED")
}
func (c *disabledNodeControl) CancelLogin(ctx context.Context, _ *apb.CancelLoginRequest) (*apb.NodeStatus, error) {
	if err := c.authorize(ctx, false); err != nil {
		return nil, err
	}
	return nil, connect.NewError(connect.CodeFailedPrecondition, "NODE_DISABLED")
}
