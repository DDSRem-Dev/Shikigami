package client

import (
	"context"
	"errors"
	"os"
	"slices"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	arpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent/agentconnect"
)

// NodeClient independently negotiates local node control. It does not require
// Agent readiness, a network login, or a daemon application credential.
type NodeClient struct {
	api      arpc.NodeControlClient
	endpoint *connection.Endpoint
	cancel   context.CancelFunc
}

func OpenNode(ctx context.Context, socket string, timeout time.Duration) (*NodeClient, error) {
	return openNode(ctx, socket, timeout, true)
}

// GetNodeStatus reads the running node's diagnostic endpoint even when login
// is unavailable. It owns and closes the connection; callers cannot use this
// status-only negotiation to issue login or cancellation requests.
func GetNodeStatus(ctx context.Context, socket string, timeout time.Duration) (*apb.NodeStatus, error) {
	node, err := openNode(ctx, socket, timeout, false)
	if err != nil {
		return nil, err
	}
	defer node.Close()
	return node.api.GetStatus(ctx, &pb.Empty{})
}

func openNode(ctx context.Context, socket string, timeout time.Duration, requireLogin bool) (*NodeClient, error) {
	if timeout <= 0 {
		return nil, errors.New("node control timeout must be positive")
	}
	path, e := ConfigPath(socket)
	if e != nil {
		return nil, e
	}
	endpoint, e := connection.NewUnixClient(path, uint32(os.Getuid()), "shiki-node-control")
	if e != nil {
		return nil, e
	}
	probe, stop := context.WithTimeout(ctx, timeout)
	defer stop()
	probe, _ = connect.NewClientContext(probe)
	raw := arpc.NewNodeControlClient(connection.NewRPCClient(endpoint))
	offer := &pb.ProtocolOffer{Major: 1, Capabilities: []string{"node.login.v1"}}
	if requireLogin {
		offer.RequiredCapabilities = []string{"node.login.v1"}
	}
	negotiation, e := raw.Negotiate(probe, offer)
	if e != nil {
		endpoint.Close()
		return nil, e
	}
	if negotiation.GetInstanceId() == "" || negotiation.GetProtocol().GetMajor() != 1 || negotiation.GetProtocol().GetMinor() != 0 || (requireLogin && !slices.Contains(negotiation.GetProtocol().GetCapabilities(), "node.login.v1")) {
		endpoint.Close()
		return nil, errors.New("invalid node control negotiation")
	}
	lifetime, cancel := context.WithCancel(context.Background())
	interceptor := func(next connect.ClientFunc) connect.ClientFunc {
		return func(ctx context.Context, spec connect.Spec) (connect.ClientStream, error) {
			callTimeout := timeout
			if timeout == 5*time.Second && (spec.Procedure == arpc.NodeControlBeginLoginProcedure || spec.Procedure == arpc.NodeControlCancelLoginProcedure) {
				callTimeout = 15 * time.Second
			}
			call, stop := context.WithTimeout(ctx, callTimeout)
			detach := context.AfterFunc(lifetime, stop)
			call, info := connect.NewClientContext(call)
			info.RequestHeader().Set("Shiki-Node-Control-Instance-Id", negotiation.InstanceId)
			stream, e := next(call, spec)
			release := func() { detach(); stop() }
			if e != nil {
				release()
				return nil, e
			}
			return &ownedStream{ClientStream: stream, release: release}, nil
		}
	}
	return &NodeClient{api: arpc.NewNodeControlClient(connection.NewRPCClient(endpoint, interceptor)), endpoint: endpoint, cancel: cancel}, nil
}
func (n *NodeClient) API() arpc.NodeControlClient { return n.api }
func (n *NodeClient) Close()                      { n.cancel(); n.endpoint.Close() }
