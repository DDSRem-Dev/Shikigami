package server

import (
	"context"
	"net"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	rpcerrors "github.com/DDSRem-Dev/Shikigami/pkg/errors"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	"google.golang.org/protobuf/types/known/structpb"
)

type refusedConnect struct {
	frame *pb.PortData
	sent  int
}

func (s *refusedConnect) Receive() (*pb.PortData, error) { return s.frame, nil }
func (s *refusedConnect) Send(*pb.PortData) error        { s.sent++; return nil }

func TestTargetRefusalRequiresAuthorizedLoopbackDial(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	for _, tt := range []struct {
		name, address     string
		allowed, canceled bool
		code              connect.Code
		marker            bool
	}{
		{"refused", "127.0.0.1", true, false, connect.CodeUnavailable, true},
		{"not-allowed", "127.0.0.1", false, false, connect.CodePermissionDenied, false},
		{"not-loopback", "192.0.2.1", true, false, connect.CodePermissionDenied, false},
		{"canceled", "127.0.0.1", true, true, connect.CodeCanceled, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ports := []any{}
			if tt.allowed {
				ports = append(ports, float64(port))
			}
			config, _ := structpb.NewStruct(map[string]any{"allow_ports": ports})
			w := New()
			if err := w.Init(t.Context(), sdk.InitContext{Side: "server", StateDir: t.TempDir(), Core: dummyCore{}, Config: config}); err != nil {
				t.Fatal(err)
			}
			if err := w.Start(t.Context()); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				if err := w.Stop(ctx); err != nil {
					t.Error(err)
				}
			})
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			if tt.canceled {
				cancel()
			}
			stream := &refusedConnect{frame: &pb.PortData{Type: pb.PortData_CONNECT, ConnectionId: "test", RemoteAddress: tt.address, Port: int32(port)}}
			err := w.ForwardPort(ctx, stream)
			if connect.CodeOf(rpcerrors.RebuildError(err)) != tt.code || rpcerrors.IsPortTargetRefused(err) != tt.marker {
				t.Fatalf("error=%v, marker=%v", err, rpcerrors.IsPortTargetRefused(err))
			}
			if stream.sent != 0 {
				t.Fatal("failed dial emitted CONNECTED")
			}
		})
	}
}
