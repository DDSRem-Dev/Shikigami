package client

import (
	"context"
	"io"
	"math"
	"testing"
	"testing/synctest"
	"time"

	"connectrpc.com/connect/v2"
	rpcerrors "github.com/DDSRem-Dev/Shikigami/pkg/errors"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin/pluginconnect"
)

// These streams really block on their RPC context. synctest advances only the
// clock, not the production timeout policy or the cancellation behavior.
type stalledFileTransport struct {
	size        int64
	afterHeader bool
}

func (p stalledFileTransport) NewClientStream(ctx context.Context, spec connect.Spec) (connect.ClientStream, error) {
	return &stalledFileStream{ctx: ctx, procedure: spec.Procedure, probe: p}, nil
}

type stalledFileStream struct {
	ctx       context.Context
	procedure string
	probe     stalledFileTransport
	received  bool
}

func (*stalledFileStream) SendHeaders() error { return nil }
func (*stalledFileStream) Send(any) error     { return nil }
func (*stalledFileStream) CloseSend() error   { return nil }
func (*stalledFileStream) Close() error       { return nil }
func (s *stalledFileStream) Receive(message any) error {
	if s.procedure == rpc.CoreAPIRemoteStatFileProcedure {
		if s.received {
			return io.EOF
		}
		s.received = true
		*message.(*pb.FileInfo) = pb.FileInfo{Size: s.probe.size, Revision: "revision"}
		return nil
	}
	if s.procedure == rpc.CoreAPIRemoteReadFileProcedure && s.probe.afterHeader && !s.received {
		s.received = true
		*message.(*pb.FileData) = pb.FileData{Payload: &pb.FileData_Info{Info: &pb.FileInfo{Size: s.probe.size}}}
		return nil
	}
	<-s.ctx.Done()
	return rpcerrors.RebuildError(s.ctx.Err())
}

func TestRemoteFileStallsHaveSizeDeadline(t *testing.T) {
	for _, mode := range []string{"first-frame", "after-header", "commit-ack"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				parent, cancel := context.WithCancel(t.Context())
				defer cancel()
				probe := stalledFileTransport{size: 1, afterHeader: mode == "after-header"}
				files := remoteFiles{core: rpc.NewCoreAPIClient(connect.NewClient(probe))}
				done := make(chan error, 1)
				go func() {
					if mode != "commit-ack" {
						done <- files.ReadFile(parent, &pb.ReadFileRequest{Path: "file", ExpectedRevision: "revision"}, func(*pb.FileData) error { return nil })
						return
					}
					first := true
					_, err := files.WriteFile(parent, func() (*pb.WriteFileRequest, error) {
						if !first {
							return nil, io.EOF
						}
						first = false
						return &pb.WriteFileRequest{Payload: &pb.WriteFileRequest_Header{Header: &pb.WriteFileHeader{Size: 1}}}, nil
					})
					done <- err
				}()
				synctest.Wait()
				time.Sleep(32 * time.Second)
				synctest.Wait()
				select {
				case err := <-done:
					if connect.CodeOf(err) != connect.CodeDeadlineExceeded {
						t.Fatalf("lost timeout: %v", err)
					}
				default:
					t.Error("blocked file RPC survived its size deadline")
					cancel()
					<-done
				}
			})
		})
	}
}

func TestFileTransferBudgetBoundaries(t *testing.T) {
	for _, tt := range []struct {
		size int64
		want time.Duration
	}{
		{0, 30 * time.Second}, {1, 31 * time.Second}, {64 << 10, 31 * time.Second},
		{(64 << 10) + 1, 32 * time.Second}, {10 << 20, 190 * time.Second},
		{math.MaxInt64, time.Duration(math.MaxInt64)},
	} {
		if got := fileTransferTimeout(tt.size); got != tt.want {
			t.Fatalf("size %d: got %v, want %v", tt.size, got, tt.want)
		}
	}
	previous := fileTransferTimeout(0)
	for size := int64(1); size > 0; size <<= 1 {
		got := fileTransferTimeout(size)
		if got < previous || got <= 0 {
			t.Fatalf("overflow/nonmonotonic budget at %d: %v", size, got)
		}
		previous = got
	}
}

func TestRemoteReadPreservesEarlierDeadlineAndWatchLifetime(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		files := remoteFiles{core: rpc.NewCoreAPIClient(connect.NewClient(stalledFileTransport{size: 10 << 20}))}
		parent, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		start := time.Now()
		err := files.ReadFile(parent, &pb.ReadFileRequest{ExpectedRevision: "revision"}, func(*pb.FileData) error { return nil })
		if connect.CodeOf(err) != connect.CodeDeadlineExceeded || time.Since(start) != time.Second {
			t.Fatal("file budget extended parent deadline", err, time.Since(start))
		}
		writeCtx, stopWrite := context.WithTimeout(t.Context(), time.Second)
		defer stopWrite()
		start = time.Now()
		first := true
		_, err = files.WriteFile(writeCtx, func() (*pb.WriteFileRequest, error) {
			if !first {
				return nil, io.EOF
			}
			first = false
			return &pb.WriteFileRequest{Payload: &pb.WriteFileRequest_Header{Header: &pb.WriteFileHeader{Size: 10 << 20}}}, nil
		})
		if connect.CodeOf(err) != connect.CodeDeadlineExceeded || time.Since(start) != time.Second {
			t.Fatal("write extended parent deadline", err, time.Since(start))
		}
		watch, stop := context.WithCancel(t.Context())
		done := make(chan error, 1)
		go func() {
			done <- files.WatchFiles(watch, &pb.WatchFilesRequest{}, func(*pb.FileEvent) error { return nil })
		}()
		time.Sleep(5 * time.Minute)
		synctest.Wait()
		select {
		case err := <-done:
			t.Fatal("file deadline leaked into watch", err)
		default:
		}
		stop()
		if err := <-done; connect.CodeOf(err) != connect.CodeCanceled {
			t.Fatal(err)
		}
	})
}

func TestRemoteFileDeadlineRejectsInvalidMetadata(t *testing.T) {
	files := remoteFiles{core: rpc.NewCoreAPIClient(connect.NewClient(stalledFileTransport{size: -1}))}
	for _, request := range []*pb.ReadFileRequest{nil, {}} {
		if err := files.ReadFile(t.Context(), request, nil); connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Fatal(err)
		}
	}
	if err := files.ReadFile(t.Context(), &pb.ReadFileRequest{ExpectedRevision: "revision"}, nil); connect.CodeOf(err) != connect.CodeDataLoss {
		t.Fatal(err)
	}
	_, err := files.WriteFile(t.Context(), func() (*pb.WriteFileRequest, error) {
		return &pb.WriteFileRequest{Payload: &pb.WriteFileRequest_Header{Header: &pb.WriteFileHeader{Size: -1}}}, nil
	})
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatal(err)
	}
	files = remoteFiles{core: rpc.NewCoreAPIClient(connect.NewClient(stalledFileTransport{size: 1}))}
	if err := files.ReadFile(t.Context(), &pb.ReadFileRequest{ExpectedRevision: "changed"}, nil); connect.CodeOf(err) != connect.CodeAborted {
		t.Fatal(err)
	}
}
