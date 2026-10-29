// Package plugin is the SDK for Shikigami subprocess plugins. Its public
// lifecycle and callback APIs do not expose raw gRPC or Connect connections.
package plugin

import (
	"context"
	"errors"
	"os"
	"strings"

	"connectrpc.com/connect/v2"
	connection "github.com/DDSRem-Dev/Shikigami/pkg/plugin/internal/coretransport"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin/pluginconnect"
)

// CoreAPI is a typed callback interface; each operation carries an explicit
// scope. The host authorizes the instance and scope on every call.
type CoreAPI interface{ rpc.CoreAPIClient }

type coreClient struct {
	rpc.CoreAPIClient
	endpoint *connection.Endpoint
}

func (c *coreClient) Close() { c.endpoint.Close() }

func newCoreClient(req *pb.InitRequest) (*coreClient, error) {
	ep := req.GetCoreApi()
	if ep == nil || ep.Token == "" || strings.ContainsAny(ep.Token, "\r\n \t") || req.PluginInstanceId == "" || req.HostInstanceId == "" {
		return nil, errors.New("invalid CoreAPI endpoint or instance")
	}
	endpoint, err := connection.NewUnixClient(ep.Address, uint32(os.Getuid()), "shiki-core")
	if err != nil {
		return nil, err
	}
	credentials := func(next connect.ClientFunc) connect.ClientFunc {
		return func(ctx context.Context, spec connect.Spec) (connect.ClientStream, error) {
			info, ok := connect.CallInfoForClientContext(ctx)
			if !ok {
				ctx, info = connect.NewClientContext(ctx)
			}
			info.RequestHeader().Set("Authorization", "Bearer "+ep.Token)
			info.RequestHeader().Set("shiki-plugin-instance-id", req.PluginInstanceId)
			info.RequestHeader().Set("shiki-host-instance-id", req.HostInstanceId)
			return next(ctx, spec)
		}
	}
	return &coreClient{CoreAPIClient: rpc.NewCoreAPIClient(connection.NewRPCClient(endpoint, credentials)), endpoint: endpoint}, nil
}
