package agent

import (
	"context"
	"io"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	agentrpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent/agentconnect"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
)

// Model the precise transport ordering in which an early RPC rejection closes
// Send before Receive exposes the authoritative response status. The frontend
// still crosses the real UID-checked UDS, handshake and agent proxy.
type terminalAdmissionTransport struct {
	final           error
	stall           bool
	sends, receives atomic.Int32
}

func (t *terminalAdmissionTransport) NewClientStream(ctx context.Context, _ connect.Spec) (connect.ClientStream, error) {
	return &terminalAdmissionStream{owner: t, ctx: ctx}, nil
}

type terminalAdmissionStream struct {
	owner *terminalAdmissionTransport
	ctx   context.Context
}

func (*terminalAdmissionStream) SendHeaders() error { return nil }
func (*terminalAdmissionStream) CloseSend() error   { return nil }
func (*terminalAdmissionStream) Close() error       { return nil }
func (s *terminalAdmissionStream) Send(any) error {
	s.owner.sends.Add(1)
	return connect.NewError(connect.CodeUnknown, "write envelope: EOF").WithCause(io.EOF)
}
func (s *terminalAdmissionStream) Receive(any) error {
	s.owner.receives.Add(1)
	if s.owner.stall {
		<-s.ctx.Done()
		return s.ctx.Err()
	}
	return s.owner.final
}

func TestTerminalAdmissionPreservesEarlyRemoteRejection(t *testing.T) {
	for _, tc := range []struct {
		name  string
		final error
		stall bool
		want  connect.Code
	}{
		{"permission", connect.NewError(connect.CodePermissionDenied, "private-peer-diagnostic"), false, connect.CodePermissionDenied},
		{"exhausted", connect.NewError(connect.CodeResourceExhausted, "private-peer-diagnostic"), false, connect.CodeResourceExhausted},
		{"empty-response", io.EOF, false, connect.CodeUnavailable},
		{"stalled-status", nil, true, connect.CodeDeadlineExceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			transport := &terminalAdmissionTransport{final: tc.final, stall: tc.stall}
			remote := connect.NewClient(transport)
			service := NewService(ctx, uint32(os.Getuid()), "test", func(context.Context, string) (*connect.Client, error) { return remote, nil })
			endpoint := serveUnix(t, service.Handler(service.RPCServer()), ctx)
			client := connection.NewRPCClient(endpoint)
			identity := handshake(t, agentrpc.NewAgentClient(client))
			stream, err := rpc.NewShikiDaemonClient(client).TerminalStream(callContext(ctx, identity, "server"))
			if err != nil {
				t.Fatal(err)
			}
			defer stream.Close()
			started := time.Now()
			_ = stream.Send(&pb.TerminalInput{Payload: &pb.TerminalInput_Open{Open: &pb.TerminalOpen{SessionId: "sess_admission", Size: &pb.ResizeEvent{Rows: 24, Cols: 80}}}})
			_, err = stream.Receive()
			if connect.CodeOf(err) != tc.want {
				t.Fatalf("early terminal admission lost status: got %v, want %v", err, tc.want)
			}
			if strings.Contains(err.Error(), "private-peer-diagnostic") {
				t.Fatal("raw peer diagnostic leaked")
			}
			if transport.sends.Load() != 1 || transport.receives.Load() != 1 {
				t.Fatalf("Open replayed or final status ignored: sends=%d receives=%d", transport.sends.Load(), transport.receives.Load())
			}
			if tc.stall && time.Since(started) >= 3*time.Second {
				t.Fatal("final-status cleanup waited for caller deadline")
			}
		})
	}
}
