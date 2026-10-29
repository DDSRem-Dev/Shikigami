package client

import (
	"context"
	"io"
	"testing"
	"testing/synctest"

	"connectrpc.com/connect/v2"
	rpcerrors "github.com/DDSRem-Dev/Shikigami/pkg/errors"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin/pluginconnect"
	"google.golang.org/protobuf/proto"
)

// The real file tree commits the actual staged bytes, then the ACK receiver
// waits for the production remoteFiles deadline rather than fabricating an
// unavailable error. Recovery uses the existing persistent journal fixture.
type deadlineJournalCore struct {
	sdk.CoreAPI
	files sdk.FileService
}

func (c deadlineJournalCore) RemoteStatFile(ctx context.Context, r *ppb.RemoteStatFileRequest) (*pb.FileInfo, error) {
	return c.files.StatFile(ctx, r.Request)
}
func (c deadlineJournalCore) RemoteListFiles(ctx context.Context, r *ppb.RemoteListFilesRequest) (*pb.ListFilesResponse, error) {
	return c.files.ListFiles(ctx, r.Request)
}

type commitWithoutACK struct{ files sdk.FileService }

func (p commitWithoutACK) NewClientStream(ctx context.Context, _ connect.Spec) (connect.ClientStream, error) {
	return &commitWithoutACKStream{ctx: ctx, files: p.files}, nil
}

type commitWithoutACKStream struct {
	ctx    context.Context
	files  sdk.FileService
	frames []*pb.WriteFileRequest
}

func (*commitWithoutACKStream) SendHeaders() error { return nil }
func (*commitWithoutACKStream) CloseSend() error   { return nil }
func (*commitWithoutACKStream) Close() error       { return nil }
func (s *commitWithoutACKStream) Send(message any) error {
	frame := message.(*ppb.RemoteWriteFrame)
	if open := frame.GetOpen(); open != nil {
		s.frames = append(s.frames, &pb.WriteFileRequest{Payload: &pb.WriteFileRequest_Header{Header: proto.Clone(open.Header).(*pb.WriteFileHeader)}})
	} else {
		s.frames = append(s.frames, &pb.WriteFileRequest{Payload: &pb.WriteFileRequest_Chunk{Chunk: append([]byte(nil), frame.GetChunk()...)}})
	}
	return nil
}
func (s *commitWithoutACKStream) Receive(any) error {
	i := 0
	_, err := s.files.WriteFile(s.ctx, func() (*pb.WriteFileRequest, error) {
		if i == len(s.frames) {
			return nil, io.EOF
		}
		frame := s.frames[i]
		i++
		return frame, nil
	})
	if err != nil {
		return err
	}
	<-s.ctx.Done()
	return rpcerrors.RebuildError(s.ctx.Err())
}

func TestDeadlineAfterCommitReobservesWithoutReplay(t *testing.T) {
	for _, mode := range []string{"matching", "local-reverted", "remote-reverted"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				f := newJournalFixture(t)
				base := proto.Clone(f.task.data.Baselines["file.txt"]).(*ppb.FileVersion)
				core := deadlineJournalCore{CoreAPI: rpc.NewCoreAPIClient(connect.NewClient(commitWithoutACK{files: f.probe})), files: f.probe}
				f.task.remote = remoteFiles{core: core, scope: scopeFor(f.task.data.Mapping)}
				write(t, f.task.data.Mapping.LocalRoot, "file.txt", "uploaded")
				if err := f.transfer(t, false); connect.CodeOf(err) != connect.CodeDeadlineExceeded {
					t.Fatal("missing actual ACK deadline", err)
				}
				op := f.saved(t).Operations["file.txt"]
				if op == nil || op.Phase != "prepared" || op.CommitState != "possible" || f.probe.writes != 1 || contents(t, f.remoteRoot, "file.txt") != "uploaded" {
					t.Fatal("timeout lost unknown commit evidence")
				}
				stage := op.Stage
				if mode == "local-reverted" {
					write(t, f.task.data.Mapping.LocalRoot, "file.txt", "baseline")
				}
				if mode == "remote-reverted" {
					write(t, f.remoteRoot, "file.txt", "baseline")
				}
				f.restart(t)
				for range 3 {
					if err := f.task.reconcile(t.Context()); err != nil {
						t.Fatal(err)
					}
				}
				if f.probe.writes != 1 || f.probe.deletes != 0 {
					t.Fatal("deadline recovery replayed mutation")
				}
				r := f.saved(t)
				if mode == "matching" {
					if len(r.Operations) != 0 || len(r.Conflicts) != 0 || proto.Equal(r.Baselines["file.txt"], base) {
						t.Fatal("matching observed commit failed to converge")
					}
				} else if r.Operations["file.txt"] == nil || r.Conflicts["file.txt"] == nil || !proto.Equal(r.Baselines["file.txt"], base) || contents(t, f.task.staging, stage) != "uploaded" {
					t.Fatal("reverted path lost conflict, baseline or retained snapshot")
				}
			})
		})
	}
}
