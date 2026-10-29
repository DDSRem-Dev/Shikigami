package plugin

import (
	"context"
	"io"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	prpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin/pluginconnect"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
)

// Force the transport ordering where the request writer observes EOF before
// the response reader exposes the remote terminal status. No retry is allowed.
type earlyRejectTransport struct {
	sendAfter int32
	final     error
	stall     bool
	sends     atomic.Int32
	receives  atomic.Int32
}

func (t *earlyRejectTransport) NewClientStream(ctx context.Context, _ connect.Spec) (connect.ClientStream, error) {
	return &earlyRejectStream{owner: t, ctx: ctx}, nil
}

type earlyRejectStream struct {
	owner *earlyRejectTransport
	ctx   context.Context
}

func (*earlyRejectStream) SendHeaders() error { return nil }
func (s *earlyRejectStream) Send(any) error {
	if s.owner.sends.Add(1) >= s.owner.sendAfter {
		return connect.NewError(connect.CodeUnknown, "write envelope: EOF").WithCause(io.EOF)
	}
	return nil
}
func (*earlyRejectStream) CloseSend() error { return nil }
func (*earlyRejectStream) Close() error     { return nil }
func (s *earlyRejectStream) Receive(any) error {
	s.owner.receives.Add(1)
	if s.owner.stall {
		<-s.ctx.Done()
		return s.ctx.Err()
	}
	return s.owner.final
}

func TestRemoteCorePreservesEarlyRejectionFinalStatus(t *testing.T) {
	for _, mode := range []string{"port", "file-header", "file-chunk"} {
		t.Run(mode, func(t *testing.T) {
			for _, stall := range []bool{false, true} {
				t.Run(map[bool]string{false: "remote-status", true: "caller-canceled"}[stall], func(t *testing.T) {
					transport := &earlyRejectTransport{sendAfter: 1, final: connect.NewError(connect.CodeResourceExhausted, "private denial"), stall: stall}
					if mode == "file-chunk" {
						transport.sendAfter = 2
					}
					core := &RemoteCore{Resolve: func(context.Context, *ppb.Scope) (rpc.ShikiDaemonClient, error) {
						return rpc.NewShikiDaemonClient(connect.NewClient(transport)), nil
					}}
					server := connect.NewServer()
					prpc.RegisterCoreAPIHandler(server, core)
					http := httptest.NewUnstartedServer(WithStreamCancellation(connection.NewRPCHandler(server)))
					http.EnableHTTP2 = true
					http.StartTLS()
					defer http.Close()
					client := prpc.NewCoreAPIClient(connect.NewClient(connecthttp.NewTransport(http.Client(), http.URL)))
					ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
					defer cancel()
					if stall {
						ctx, cancel = context.WithTimeout(ctx, 150*time.Millisecond)
						defer cancel()
					}
					var err error
					if mode == "port" {
						stream, e := client.RemoteForwardPort(ctx)
						if e != nil {
							t.Fatal(e)
						}
						defer stream.Close()
						_ = stream.Send(&ppb.RemotePortFrame{Payload: &ppb.RemotePortFrame_Open{Open: &ppb.RemotePortOpen{Scope: &ppb.Scope{ServerId: "srv_test"}, Connect: &pb.PortData{Type: pb.PortData_CONNECT, ConnectionId: "test", Port: 3000}}}})
						_, err = stream.Receive()
					} else {
						stream, e := client.RemoteWriteFile(ctx)
						if e != nil {
							t.Fatal(e)
						}
						_ = stream.Send(&ppb.RemoteWriteFrame{Payload: &ppb.RemoteWriteFrame_Open{Open: &ppb.RemoteWriteOpen{Scope: &ppb.Scope{ServerId: "srv_test"}, Header: &pb.WriteFileHeader{Path: "file"}}}})
						if mode == "file-chunk" {
							_ = stream.Send(&ppb.RemoteWriteFrame{Payload: &ppb.RemoteWriteFrame_Chunk{Chunk: []byte("data")}})
						}
						_, err = stream.CloseAndReceive()
					}
					want := connect.CodeResourceExhausted
					if stall {
						want = connect.CodeDeadlineExceeded
					}
					if connect.CodeOf(err) != want {
						t.Fatalf("final rejection lost: got %v, want %v", err, want)
					}
					if transport.sends.Load() != transport.sendAfter || transport.receives.Load() != 1 {
						t.Fatalf("request replayed or final status not read: sends=%d receives=%d", transport.sends.Load(), transport.receives.Load())
					}
				})
			}
		})
	}
}
