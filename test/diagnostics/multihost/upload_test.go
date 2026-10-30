package main

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
)

type uploadProbe struct {
	sent     []*pb.WriteFileRequest
	failAt   int
	sendErr  error
	received int
	final    func() (*pb.FileInfo, error)
}

func (p *uploadProbe) Send(frame *pb.WriteFileRequest) error {
	p.sent = append(p.sent, frame)
	if len(p.sent) == p.failAt {
		return p.sendErr
	}
	return nil
}

func (p *uploadProbe) CloseAndReceive() (*pb.FileInfo, error) {
	p.received++
	return p.final()
}

func TestUploadPreservesConditionalRejectionAfterSendEOF(t *testing.T) {
	for _, code := range []connect.Code{connect.CodeAborted, connect.CodeAlreadyExists} {
		for _, phase := range []struct {
			name   string
			failAt int
		}{{"header", 1}, {"chunk", 2}} {
			t.Run(code.String()+"/"+phase.name, func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				p := &uploadProbe{failAt: phase.failAt, sendErr: io.EOF, final: func() (*pb.FileInfo, error) {
					return nil, connect.NewError(code, "conditional target changed")
				}}
				header := &pb.WriteFileHeader{ProjectId: "proj_owned", Path: "file.txt", BaseRevision: "observed"}
				info, err := uploadContent(ctx, cancel, p, header, []byte("payload"))
				if info != nil || connect.CodeOf(err) != code || errors.Is(err, io.EOF) {
					t.Fatalf("lost final rejection: %v %v", info, err)
				}
				if len(p.sent) != phase.failAt || p.received != 1 {
					t.Fatalf("replayed request or read status more than once: sends=%d receives=%d", len(p.sent), p.received)
				}
				if p.sent[0].GetHeader() != header || phase.failAt == 2 && string(p.sent[1].GetChunk()) != "payload" {
					t.Fatal("conditional header or payload changed")
				}
			})
		}
	}
}

func TestUploadFailedSendCannotBecomeSuccessfulCompletion(t *testing.T) {
	for _, end := range []struct {
		name string
		err  error
	}{{"success-response", nil}, {"clean-eof", io.EOF}} {
		t.Run(end.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			p := &uploadProbe{failAt: 1, sendErr: io.EOF, final: func() (*pb.FileInfo, error) {
				return &pb.FileInfo{Revision: "untrusted"}, end.err
			}}
			info, err := uploadContent(ctx, cancel, p, &pb.WriteFileHeader{}, []byte("payload"))
			if info != nil || connect.CodeOf(err) != connect.CodeUnavailable || errors.Is(err, io.EOF) || p.received != 1 || len(p.sent) != 1 {
				t.Fatalf("failed send became success or replay: %v %v", info, err)
			}
		})
	}
}

func TestUploadCancellationAndFinalStatusDeadline(t *testing.T) {
	for _, scenario := range []string{"already-canceled", "cancel-during-final", "hard-transport-error", "unresponsive-final"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 4*time.Second)
			defer cancel()
			want, reads := connect.CodeCanceled, 0
			p := &uploadProbe{failAt: 1, sendErr: io.EOF}
			p.final = func() (*pb.FileInfo, error) {
				if scenario == "cancel-during-final" {
					cancel()
				}
				<-ctx.Done()
				return nil, ctx.Err()
			}
			switch scenario {
			case "already-canceled":
				cancel()
			case "hard-transport-error":
				p.sendErr = connect.NewError(connect.CodeUnavailable, "transport failed")
				want = connect.CodeUnavailable
			case "cancel-during-final":
				reads = 1
			case "unresponsive-final":
				want, reads = connect.CodeDeadlineExceeded, 1
			}
			started := time.Now()
			info, err := uploadContent(ctx, cancel, p, &pb.WriteFileHeader{}, []byte("payload"))
			if info != nil || connect.CodeOf(err) != want || p.received != reads || len(p.sent) != 1 {
				t.Fatalf("cancellation/deadline changed: %v %v reads=%d", info, err, p.received)
			}
			if time.Since(started) > 3*time.Second {
				t.Fatal("final status exceeded bounded cleanup deadline")
			}
		})
	}
}

func TestUploadSuccessReturnsOnlyAfterFinalResponse(t *testing.T) {
	for _, content := range []string{"", "payload"} {
		t.Run(content, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			result := &pb.FileInfo{Revision: "committed"}
			p := &uploadProbe{final: func() (*pb.FileInfo, error) { return result, nil }}
			header := &pb.WriteFileHeader{BaseRevision: "observed", Size: int64(len(content))}
			info, err := uploadContent(ctx, cancel, p, header, []byte(content))
			wantFrames := 1
			if content != "" {
				wantFrames++
			}
			if err != nil || info != result || p.received != 1 || len(p.sent) != wantFrames || p.sent[0].GetHeader() != header {
				t.Fatalf("incorrect successful upload: %v %v", info, err)
			}
		})
	}
}
