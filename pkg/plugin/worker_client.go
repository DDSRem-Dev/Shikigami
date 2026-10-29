package plugin

import (
	"context"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/pkg/plugin/limits"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

type fileClient struct {
	owner *lifecycleClient
	stub  ppb.FileServiceClient
}

func (c *fileClient) context(ctx context.Context, project string) context.Context {
	return metadata.AppendToOutgoingContext(c.owner.context(ctx), "shiki-project-id", project)
}
func (c *fileClient) ListFiles(ctx context.Context, r *pb.ListFilesRequest) (*pb.ListFilesResponse, error) {
	v, e := c.stub.ListFiles(c.context(ctx, r.ProjectId), r)
	return v, FromGRPC(e)
}
func (c *fileClient) StatFile(ctx context.Context, r *pb.StatFileRequest) (*pb.FileInfo, error) {
	v, e := c.stub.StatFile(c.context(ctx, r.ProjectId), r)
	return v, FromGRPC(e)
}
func (c *fileClient) DeleteFile(ctx context.Context, r *pb.DeleteFileRequest) error {
	_, e := c.stub.DeleteFile(c.context(ctx, r.ProjectId), r)
	return FromGRPC(e)
}
func (c *fileClient) ReadFile(ctx context.Context, r *pb.ReadFileRequest) (Receiver[pb.FileData], error) {
	ctx, cancel := context.WithCancel(c.context(ctx, r.ProjectId))
	s, e := c.stub.ReadFile(ctx, r)
	if e != nil {
		cancel()
		return nil, FromGRPC(e)
	}
	return &receiver[pb.FileData]{receive: s.Recv, cancel: cancel}, nil
}
func (c *fileClient) WatchFiles(ctx context.Context, r *pb.WatchFilesRequest) (Receiver[pb.FileEvent], error) {
	ctx, cancel := context.WithCancel(c.context(ctx, r.ProjectId))
	s, e := c.stub.WatchFiles(ctx, r)
	if e != nil {
		cancel()
		return nil, FromGRPC(e)
	}
	return &receiver[pb.FileEvent]{receive: s.Recv, cancel: cancel}, nil
}
func (c *fileClient) WriteFile(ctx context.Context) (FileWriteStream, error) {
	ctx, cancel := context.WithCancel(ctx)
	return &fileWriter{client: c, ctx: ctx, cancel: cancel}, nil
}

type receiver[T any] struct {
	receive func() (*T, error)
	cancel  context.CancelFunc
}

func (r *receiver[T]) Receive() (*T, error) {
	v, e := r.receive()
	if e != nil {
		r.cancel()
	}
	return v, FromGRPC(e)
}
func (r *receiver[T]) Close() error { r.cancel(); return nil }

type fileWriter struct {
	client *fileClient
	ctx    context.Context
	cancel context.CancelFunc
	stream grpc.ClientStreamingClient[pb.WriteFileRequest, pb.FileInfo]
}

func (w *fileWriter) Send(data *pb.WriteFileRequest) error {
	if data == nil || data.Payload == nil {
		return connect.NewError(connect.CodeInvalidArgument, "file frame required")
	}
	if w.stream == nil {
		if data.GetHeader() == nil || data.GetHeader().ProjectId == "" {
			return connect.NewError(connect.CodeInvalidArgument, "file header required")
		}
		s, e := w.client.stub.WriteFile(w.client.context(w.ctx, data.GetHeader().ProjectId))
		if e != nil {
			return FromGRPC(e)
		}
		w.stream = s
	} else if data.GetHeader() != nil {
		return connect.NewError(connect.CodeInvalidArgument, "file header repeated")
	}
	if len(data.GetChunk()) > limits.MaxDataBytes {
		return connect.NewError(connect.CodeResourceExhausted, "file chunk too large")
	}
	return FromGRPC(w.stream.Send(data))
}
func (w *fileWriter) CloseAndReceive() (*pb.FileInfo, error) {
	defer w.cancel()
	if w.stream == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, "file header required")
	}
	v, e := w.stream.CloseAndRecv()
	return v, FromGRPC(e)
}
func (w *fileWriter) Close() error { w.cancel(); return nil }

type portClient struct {
	owner *lifecycleClient
	stub  ppb.PortServiceClient
}

func (c *portClient) ListPorts(ctx context.Context, r *pb.ListPortsRequest) (*pb.ListPortsResponse, error) {
	v, e := c.stub.ListPorts(c.owner.context(ctx), r)
	return v, FromGRPC(e)
}
func (c *portClient) WatchPorts(ctx context.Context, r *pb.WatchPortsRequest) (Receiver[pb.PortEvent], error) {
	ctx, cancel := context.WithCancel(c.owner.context(ctx))
	s, e := c.stub.WatchPorts(ctx, r)
	if e != nil {
		cancel()
		return nil, FromGRPC(e)
	}
	return &receiver[pb.PortEvent]{receive: s.Recv, cancel: cancel}, nil
}
func (c *portClient) ForwardPort(ctx context.Context) (PortClientStream, error) {
	ctx, cancel := context.WithCancel(c.owner.context(ctx))
	s, e := c.stub.ForwardPort(ctx)
	if e != nil {
		cancel()
		return nil, FromGRPC(e)
	}
	return &clientPortStream{stream: s, cancel: cancel}, nil
}

type clientPortStream struct {
	stream grpc.BidiStreamingClient[pb.PortData, pb.PortData]
	cancel context.CancelFunc
}

func (s *clientPortStream) Send(data *pb.PortData) error {
	if data == nil {
		return connect.NewError(connect.CodeInvalidArgument, "port frame required")
	}
	if len(data.Data) > limits.MaxDataBytes {
		return connect.NewError(connect.CodeResourceExhausted, "port chunk too large")
	}
	return FromGRPC(s.stream.Send(data))
}
func (s *clientPortStream) Receive() (*pb.PortData, error) {
	v, e := s.stream.Recv()
	if e != nil {
		s.cancel()
	}
	return v, FromGRPC(e)
}
func (s *clientPortStream) CloseSend() error { return FromGRPC(s.stream.CloseSend()) }
func (s *clientPortStream) Close() error     { s.cancel(); return nil }

func (c *portClient) CheckPort(ctx context.Context, r *pb.CheckPortRequest) (*pb.CheckPortResponse, error) {
	v, e := c.stub.CheckPort(c.owner.context(ctx), r)
	return v, FromGRPC(e)
}
