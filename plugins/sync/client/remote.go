package client

import (
	"context"
	"errors"
	"io"
	"time"

	"connectrpc.com/connect/v2"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
)

// remoteFiles translates typed file operations into the scoped host callback.
// Remote credentials and network clients stay entirely in the agent.
type remoteFiles struct {
	core  sdk.CoreAPI
	scope *ppb.Scope
}

func (r remoteFiles) ListFiles(ctx context.Context, in *pb.ListFilesRequest) (*pb.ListFilesResponse, error) {
	return r.core.RemoteListFiles(ctx, &ppb.RemoteListFilesRequest{Scope: r.scope, Request: in})
}
func (r remoteFiles) StatFile(ctx context.Context, in *pb.StatFileRequest) (*pb.FileInfo, error) {
	return r.core.RemoteStatFile(ctx, &ppb.RemoteStatFileRequest{Scope: r.scope, Request: in})
}
func (r remoteFiles) DeleteFile(ctx context.Context, in *pb.DeleteFileRequest) error {
	_, err := r.core.RemoteDeleteFile(ctx, &ppb.RemoteDeleteFileRequest{Scope: r.scope, Request: in})
	return err
}
func (r remoteFiles) ReadFile(ctx context.Context, in *pb.ReadFileRequest, send func(*pb.FileData) error) error {
	if in == nil || in.ExpectedRevision == "" {
		return fail(connect.CodeInvalidArgument, "FILE_REVISION_REQUIRED")
	}
	// Obtain a bounded size before opening the stream: waiting for its first
	// frame must not leave the background mapping without a deadline.
	probe, stop := context.WithTimeout(ctx, 5*time.Second)
	info, err := r.StatFile(probe, &pb.StatFileRequest{ProjectId: in.ProjectId, Path: in.Path})
	stop()
	if err != nil {
		return err
	}
	if info == nil || info.Size < 0 {
		return fail(connect.CodeDataLoss, "INVALID_FILE_SIZE")
	}
	if in.ExpectedRevision != "" && info.Revision != in.ExpectedRevision {
		return fail(connect.CodeAborted, "SOURCE_CHANGED")
	}
	ctx, cancel := context.WithTimeout(ctx, fileTransferTimeout(info.Size))
	defer cancel()
	stream, err := r.core.RemoteReadFile(ctx, &ppb.RemoteReadFileRequest{Scope: r.scope, Request: in})
	if err != nil {
		return err
	}
	defer stream.Close()
	for {
		frame, err := stream.Receive()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if err = send(frame); err != nil {
			return err
		}
	}
}
func (r remoteFiles) WatchFiles(ctx context.Context, in *pb.WatchFilesRequest, send func(*pb.FileEvent) error) error {
	stream, err := r.core.RemoteWatchFiles(ctx, &ppb.RemoteWatchFilesRequest{Scope: r.scope, Request: in})
	if err != nil {
		return err
	}
	defer stream.Close()
	for {
		event, err := stream.Receive()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if err = send(event); err != nil {
			return err
		}
	}
}
func (r remoteFiles) WriteFile(ctx context.Context, receive func() (*pb.WriteFileRequest, error)) (*pb.FileInfo, error) {
	// transfer's first callback constructs a local header; no remote operation
	// starts until its size has established the deadline below.
	first, err := receive()
	if err != nil {
		return nil, err
	}
	if first.GetHeader() == nil {
		return nil, fail(connect.CodeInvalidArgument, "FILE_HEADER_REQUIRED")
	}
	if first.GetHeader().Size < 0 {
		return nil, fail(connect.CodeInvalidArgument, "INVALID_FILE_SIZE")
	}
	ctx, cancel := context.WithTimeout(ctx, fileTransferTimeout(first.GetHeader().Size))
	defer cancel()
	stream, err := r.core.RemoteWriteFile(ctx)
	if err != nil {
		return nil, err
	}
	if err = stream.Send(&ppb.RemoteWriteFrame{Payload: &ppb.RemoteWriteFrame_Open{Open: &ppb.RemoteWriteOpen{Scope: r.scope, Header: first.GetHeader()}}}); err != nil {
		if ctx.Err() == nil {
			if _, finalErr := stream.CloseAndReceive(); finalErr != nil {
				return nil, finalErr
			}
		}
		return nil, err
	}
	for {
		frame, err := receive()
		if errors.Is(err, io.EOF) {
			return stream.CloseAndReceive()
		}
		if err != nil {
			return nil, err
		}
		if _, ok := frame.Payload.(*pb.WriteFileRequest_Chunk); !ok {
			return nil, fail(connect.CodeInvalidArgument, "FILE_CHUNK_REQUIRED")
		}
		if err = stream.Send(&ppb.RemoteWriteFrame{Payload: &ppb.RemoteWriteFrame_Chunk{Chunk: frame.GetChunk()}}); err != nil {
			if ctx.Err() == nil {
				if _, finalErr := stream.CloseAndReceive(); finalErr != nil {
					return nil, finalErr
				}
			}
			return nil, err
		}
	}
}

var _ sdk.FileService = remoteFiles{}
