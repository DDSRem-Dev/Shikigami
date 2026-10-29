package plugin

import (
	"context"
	"errors"
	"io"
	"sync/atomic"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/pkg/plugin/limits"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

type fileServer struct {
	ppb.UnimplementedFileServiceServer
	impl FileWorkerService
}

func (f *fileServer) ListFiles(ctx context.Context, r *pb.ListFilesRequest) (*pb.ListFilesResponse, error) {
	if err := verifyProject(ctx, r.ProjectId); err != nil {
		return nil, ToGRPC(err)
	}
	v, e := f.impl.ListFiles(ctx, r)
	return v, ToGRPC(e)
}
func (f *fileServer) StatFile(ctx context.Context, r *pb.StatFileRequest) (*pb.FileInfo, error) {
	if err := verifyProject(ctx, r.ProjectId); err != nil {
		return nil, ToGRPC(err)
	}
	v, e := f.impl.StatFile(ctx, r)
	return v, ToGRPC(e)
}
func (f *fileServer) DeleteFile(ctx context.Context, r *pb.DeleteFileRequest) (*pb.Empty, error) {
	if err := verifyProject(ctx, r.ProjectId); err != nil {
		return nil, ToGRPC(err)
	}
	if e := f.impl.DeleteFile(ctx, r); e != nil {
		return nil, ToGRPC(e)
	}
	return &pb.Empty{}, nil
}
func (f *fileServer) ReadFile(r *pb.ReadFileRequest, s grpc.ServerStreamingServer[pb.FileData]) error {
	if err := verifyProject(s.Context(), r.ProjectId); err != nil {
		return ToGRPC(err)
	}
	return ToGRPC(f.impl.ReadFile(s.Context(), r, func(data *pb.FileData) error {
		if len(data.GetChunk()) > limits.MaxDataBytes {
			return connect.NewError(connect.CodeResourceExhausted, "file chunk too large")
		}
		return FromGRPC(s.Send(data))
	}))
}
func (f *fileServer) WatchFiles(r *pb.WatchFilesRequest, s grpc.ServerStreamingServer[pb.FileEvent]) error {
	if err := verifyProject(s.Context(), r.ProjectId); err != nil {
		return ToGRPC(err)
	}
	return ToGRPC(f.impl.WatchFiles(s.Context(), r, func(event *pb.FileEvent) error { return FromGRPC(s.Send(event)) }))
}
func (f *fileServer) WriteFile(s grpc.ClientStreamingServer[pb.WriteFileRequest, pb.FileInfo]) error {
	var receivedHeader atomic.Bool
	reader := newFileReceiver(func() (*pb.WriteFileRequest, error) {
		r, err := s.Recv()
		if err != nil {
			return nil, FromGRPC(err)
		}
		if !receivedHeader.Load() {
			if r.GetHeader() == nil {
				return nil, connect.NewError(connect.CodeInvalidArgument, "file header required")
			}
			if err = verifyProject(s.Context(), r.GetHeader().ProjectId); err != nil {
				return nil, err
			}
			receivedHeader.Store(true)
		} else {
			if _, ok := r.Payload.(*pb.WriteFileRequest_Chunk); !ok {
				return nil, connect.NewError(connect.CodeInvalidArgument, "file chunk required")
			}
			if len(r.GetChunk()) > limits.MaxDataBytes {
				return nil, connect.NewError(connect.CodeResourceExhausted, "file chunk too large")
			}
		}
		return r, nil
	})
	defer reader.close()
	result, err := f.impl.WriteFile(s.Context(), reader.receive)
	if err != nil {
		return ToGRPC(err)
	}
	if err := reader.failure(); err != nil {
		return ToGRPC(err)
	}
	if !receivedHeader.Load() {
		return ToGRPC(connect.NewError(connect.CodeInvalidArgument, "file header required"))
	}
	// A worker cannot report success before consuming the complete request.
	if _, err = reader.receive(s.Context()); !errors.Is(err, io.EOF) {
		if err == nil {
			err = connect.NewError(connect.CodeFailedPrecondition, "worker did not consume upload")
		}
		return ToGRPC(err)
	}
	return s.SendAndClose(result)
}
func verifyProject(ctx context.Context, id string) error {
	md, _ := metadata.FromIncomingContext(ctx)
	values := md.Get("shiki-project-id")
	if id == "" || len(values) != 1 || values[0] != id {
		return connect.NewError(connect.CodePermissionDenied, "worker project scope mismatch")
	}
	return nil
}

type portServer struct {
	ppb.UnimplementedPortServiceServer
	impl PortService
}

func (p *portServer) ListPorts(ctx context.Context, r *pb.ListPortsRequest) (*pb.ListPortsResponse, error) {
	v, e := p.impl.ListPorts(ctx, r)
	return v, ToGRPC(e)
}
func (p *portServer) WatchPorts(r *pb.WatchPortsRequest, s grpc.ServerStreamingServer[pb.PortEvent]) error {
	return ToGRPC(p.impl.WatchPorts(s.Context(), r, func(event *pb.PortEvent) error { return FromGRPC(s.Send(event)) }))
}
func (p *portServer) ForwardPort(s grpc.BidiStreamingServer[pb.PortData, pb.PortData]) error {
	return ToGRPC(p.impl.ForwardPort(s.Context(), &serverPortStream{stream: s, first: true}))
}

type serverPortStream struct {
	stream         grpc.BidiStreamingServer[pb.PortData, pb.PortData]
	first          bool
	readHalfClosed bool
	connectionID   string
}

func (s *serverPortStream) Send(data *pb.PortData) error {
	if data == nil {
		return connect.NewError(connect.CodeInvalidArgument, "port frame required")
	}
	if len(data.Data) > limits.MaxDataBytes {
		return connect.NewError(connect.CodeResourceExhausted, "port chunk too large")
	}
	return FromGRPC(s.stream.Send(data))
}
func (s *serverPortStream) Receive() (*pb.PortData, error) {
	data, err := s.stream.Recv()
	if err != nil {
		return nil, FromGRPC(err)
	}
	if s.first {
		s.first = false
		if data.Type != pb.PortData_CONNECT {
			return nil, connect.NewError(connect.CodeInvalidArgument, "port CONNECT required")
		}
		s.connectionID = data.ConnectionId
	} else {
		if data.Port != 0 || data.RemoteAddress != "" || data.SessionId != "" || data.Automatic || data.SourceIdentity != "" || (data.ConnectionId != "" && data.ConnectionId != s.connectionID) {
			return nil, connect.NewError(connect.CodeInvalidArgument, "port target changed after CONNECT")
		}
		switch data.Type {
		case pb.PortData_DATA:
			if s.readHalfClosed {
				return nil, connect.NewError(connect.CodeInvalidArgument, "port data follows HALF_CLOSE")
			}
		case pb.PortData_HALF_CLOSE:
			if s.readHalfClosed {
				return nil, connect.NewError(connect.CodeInvalidArgument, "port HALF_CLOSE repeated")
			}
			s.readHalfClosed = true
		case pb.PortData_CLOSE:
		default:
			return nil, connect.NewError(connect.CodeInvalidArgument, "invalid port request frame")
		}
	}
	if len(data.Data) > limits.MaxDataBytes {
		return nil, connect.NewError(connect.CodeResourceExhausted, "port chunk too large")
	}
	return data, nil
}

func (p *portServer) CheckPort(ctx context.Context, r *pb.CheckPortRequest) (*pb.CheckPortResponse, error) {
	v, e := p.impl.CheckPort(ctx, r)
	return v, ToGRPC(e)
}
