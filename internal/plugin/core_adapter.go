package plugin

import (
	"context"
	"errors"
	"io"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	prpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin/pluginconnect"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
)

// RemoteCore is the client-host data-plane portion of CoreAPI. Policy's
// interceptor checks capability/scope before these methods receive messages.
// Resolve must select the already authenticated client for scope.ServerId.
type RemoteCore struct {
	prpc.UnimplementedCoreAPIHandler
	Resolve          func(context.Context, *ppb.Scope) (rpc.ShikiDaemonClient, error)
	AuthorizeForward func(context.Context, *ppb.Scope, *pb.PortData) error
}

func (c *RemoteCore) remote(ctx context.Context, scope *ppb.Scope) (context.Context, rpc.ShikiDaemonClient, error) {
	if scope == nil || scope.ServerId == "" || c.Resolve == nil {
		return nil, nil, connect.NewError(connect.CodeFailedPrecondition, "remote server is unavailable")
	}
	fresh, _ := connect.NewClientContext(ctx)
	client, e := c.Resolve(fresh, scope)
	return fresh, client, connection.RebuildError(e)
}
func (c *RemoteCore) RemoteListFiles(ctx context.Context, r *ppb.RemoteListFilesRequest) (*pb.ListFilesResponse, error) {
	ctx, client, e := c.remote(ctx, r.Scope)
	if e != nil {
		return nil, e
	}
	v, e := client.ListFiles(ctx, r.Request)
	return v, connection.RebuildError(e)
}
func (c *RemoteCore) RemoteStatFile(ctx context.Context, r *ppb.RemoteStatFileRequest) (*pb.FileInfo, error) {
	ctx, client, e := c.remote(ctx, r.Scope)
	if e != nil {
		return nil, e
	}
	v, e := client.StatFile(ctx, r.Request)
	return v, connection.RebuildError(e)
}
func (c *RemoteCore) RemoteDeleteFile(ctx context.Context, r *ppb.RemoteDeleteFileRequest) (*pb.Empty, error) {
	ctx, client, e := c.remote(ctx, r.Scope)
	if e != nil {
		return nil, e
	}
	v, e := client.DeleteFile(ctx, r.Request)
	return v, connection.RebuildError(e)
}
func (c *RemoteCore) RemoteReadFile(ctx context.Context, r *ppb.RemoteReadFileRequest, s prpc.CoreAPIRemoteReadFileServerStream) error {
	ctx, client, e := c.remote(ctx, r.Scope)
	if e != nil {
		return e
	}
	down, e := client.ReadFile(ctx, r.Request)
	if e != nil {
		return connection.RebuildError(e)
	}
	defer down.Close()
	return forwardResponses[pb.FileData](down, s.Send)
}
func (c *RemoteCore) RemoteWatchFiles(ctx context.Context, r *ppb.RemoteWatchFilesRequest, s prpc.CoreAPIRemoteWatchFilesServerStream) error {
	ctx, client, e := c.remote(ctx, r.Scope)
	if e != nil {
		return e
	}
	down, e := client.WatchFiles(ctx, r.Request)
	if e != nil {
		return connection.RebuildError(e)
	}
	defer down.Close()
	return forwardResponses[pb.FileEvent](down, s.Send)
}
func (c *RemoteCore) RemoteWriteFile(ctx context.Context, s prpc.CoreAPIRemoteWriteFileServerStream) (*pb.FileInfo, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	first, e := s.Receive()
	if e != nil {
		return nil, connection.RebuildError(e)
	}
	open := first.GetOpen()
	if open == nil || open.Header == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, "upload open frame required")
	}
	ctx, client, e := c.remote(ctx, open.Scope)
	if e != nil {
		return nil, e
	}
	down, e := client.WriteFile(ctx)
	if e != nil {
		return nil, connection.RebuildError(e)
	}
	// Client streams have no generated Close; the child context owns cleanup.
	if e = down.Send(&pb.WriteFileRequest{Payload: &pb.WriteFileRequest_Header{Header: open.Header}}); e != nil {
		return nil, connection.FinalSendError(ctx, cancel, e, func() error { _, final := down.CloseAndReceive(); return final })
	}
	for {
		frame, e := s.Receive()
		if errors.Is(e, io.EOF) {
			break
		}
		if e != nil {
			return nil, connection.RebuildError(e)
		}
		if _, ok := frame.Payload.(*ppb.RemoteWriteFrame_Chunk); !ok {
			return nil, connect.NewError(connect.CodeInvalidArgument, "upload chunk required")
		}
		if e = down.Send(&pb.WriteFileRequest{Payload: &pb.WriteFileRequest_Chunk{Chunk: frame.GetChunk()}}); e != nil {
			return nil, connection.FinalSendError(ctx, cancel, e, func() error { _, final := down.CloseAndReceive(); return final })
		}
	}
	result, e := down.CloseAndReceive()
	return result, connection.RebuildError(e)
}
func (c *RemoteCore) RemoteListPorts(ctx context.Context, r *ppb.RemoteListPortsRequest) (*pb.ListPortsResponse, error) {
	ctx, client, e := c.remote(ctx, r.Scope)
	if e != nil {
		return nil, e
	}
	v, e := client.ListPorts(ctx, r.Request)
	return v, connection.RebuildError(e)
}
func (c *RemoteCore) RemoteWatchPorts(ctx context.Context, r *ppb.RemoteWatchPortsRequest, s prpc.CoreAPIRemoteWatchPortsServerStream) error {
	ctx, client, e := c.remote(ctx, r.Scope)
	if e != nil {
		return e
	}
	down, e := client.WatchPorts(ctx, r.Request)
	if e != nil {
		return connection.RebuildError(e)
	}
	defer down.Close()
	return forwardResponses[pb.PortEvent](down, s.Send)
}
func (c *RemoteCore) RemoteForwardPort(ctx context.Context, s prpc.CoreAPIRemoteForwardPortServerStream) error {
	first, e := s.Receive()
	if e != nil {
		return connection.RebuildError(e)
	}
	open := first.GetOpen()
	if open == nil || open.Connect == nil {
		return connect.NewError(connect.CodeInvalidArgument, "port open frame required")
	}
	if c.AuthorizeForward != nil {
		if e = c.AuthorizeForward(ctx, open.Scope, open.Connect); e != nil {
			return e
		}
	}
	remote, client, e := c.remote(ctx, open.Scope)
	if e != nil {
		return e
	}
	call, cancel := context.WithCancel(remote)
	defer cancel()
	down, e := client.ForwardPort(call)
	if e != nil {
		return connection.RebuildError(e)
	}
	defer down.Close()
	if e = down.Send(open.Connect); e != nil {
		return connection.FinalSendError(call, cancel, e, func() error { _, final := down.Receive(); return final })
	}
	receive := func() (*pb.PortData, error) {
		frame, e := s.Receive()
		if e != nil {
			return nil, e
		}
		if frame.GetData() == nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, "port data frame required")
		}
		return frame.GetData(), nil
	}
	return bridgePort(ctx, cancel, receive, s.Send, down)
}

func (c *RemoteCore) RemoteCheckPort(ctx context.Context, r *ppb.RemoteCheckPortRequest) (*pb.CheckPortResponse, error) {
	ctx, client, e := c.remote(ctx, r.Scope)
	if e != nil {
		return nil, e
	}
	v, e := client.CheckPort(ctx, r.Request)
	return v, connection.RebuildError(e)
}
