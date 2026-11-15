package agent

import (
	"context"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	epb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions"
	"google.golang.org/protobuf/proto"
)

// BrowserLocalService is an agent-owned route to the current browser client
// worker. The agent and CLI never own the viewer's loopback listener or lifetime.
func (s *Service) OpenBrowserViewer(ctx context.Context, request *epb.OpenBrowserViewerRequest) (response *epb.BrowserViewer, err error) {
	if request == nil || request.ServerId == "" || request.ProfileId == "" {
		return nil, rpcError(connect.CodeInvalidArgument, "BROWSER_VIEWER_SCOPE_REQUIRED")
	}
	resources, err := s.requireResources()
	if err != nil {
		return nil, err
	}
	snapshot, err := resources.store.View()
	if err != nil {
		return nil, stateError(err)
	}
	if _, err = storedServer(snapshot, request.ServerId); err != nil {
		return nil, err
	}
	err = s.plugins.Load().BrowserLocalCall(ctx, func(call context.Context, client sdk.BrowserLocalClient) error {
		response, err = client.OpenBrowserViewer(call, request)
		return err
	})
	return response, connection.RebuildError(err)
}

func (s *Service) CloseBrowserViewer(ctx context.Context, request *epb.CloseBrowserViewerRequest) (response *epb.BrowserViewer, err error) {
	if request == nil || request.ViewerId == "" {
		return nil, rpcError(connect.CodeInvalidArgument, "BROWSER_VIEWER_ID_REQUIRED")
	}
	err = s.plugins.Load().BrowserLocalCall(ctx, func(call context.Context, client sdk.BrowserLocalClient) error {
		response, err = client.CloseBrowserViewer(call, request)
		return err
	})
	return response, connection.RebuildError(err)
}

// Every remote extension carries server_id in its first typed request. Later
// frames may omit that redundant field but may never switch a pinned route.
type extensionRouteStream struct {
	connect.ServerStream
	serverID string
	opened   bool
}

func (s *extensionRouteStream) Receive(message any) error {
	if err := s.ServerStream.Receive(message); err != nil {
		return err
	}
	request, ok := message.(proto.Message)
	if !ok {
		return rpcError(connect.CodeInternal, "MESSAGE_SCHEMA_REQUIRED")
	}
	reflected := request.ProtoReflect()
	field := reflected.Descriptor().Fields().ByName("server_id")
	if field == nil {
		return rpcError(connect.CodeInvalidArgument, "EXTENSION_SERVER_SCOPE_REQUIRED")
	}
	serverID := reflected.Get(field).String()
	if serverID != s.serverID && (!s.opened || serverID != "") {
		return rpcError(connect.CodeInvalidArgument, "SERVER_ROUTE_MISMATCH")
	}
	s.opened = true
	return nil
}
