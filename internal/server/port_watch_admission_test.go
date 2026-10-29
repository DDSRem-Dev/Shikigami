package server

import (
	"context"
	"errors"
	"io"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/auth"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	"github.com/DDSRem-Dev/Shikigami/pkg/plugin/limits"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
)

type admissionPortWatchProbe struct {
	rpc.UnimplementedShikiDaemonHandler
	release <-chan struct{}
}

func (p admissionPortWatchProbe) WatchPorts(ctx context.Context, _ *pb.WatchPortsRequest, stream rpc.ShikiDaemonWatchPortsServerStream) error {
	if err := stream.Send(&pb.PortEvent{}); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-p.release:
		return stream.Send(&pb.PortEvent{})
	}
}

func TestPortAdmissionWatchOutlivesFirstFrameDeadline(t *testing.T) {
	manager, err := auth.Open(filepath.Join(t.TempDir(), "auth"), "tester")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Close() })
	service := &Service{Auth: manager}
	release := make(chan struct{})
	server := connect.NewServer(manager.Interceptor(), service.fileCalls.interceptor(), service.portCalls.interceptor())
	rpc.RegisterShikiDaemonHandler(server, admissionPortWatchProbe{release: release})
	remote := httptest.NewUnstartedServer(connection.NewRPCHandler(server, manager.HTTPMiddleware, firstRequestTransport))
	remote.EnableHTTP2 = true
	remote.StartTLS()
	t.Cleanup(remote.Close)
	fixture := &admissionFixture{auth: manager, service: service, remote: remote}
	client := fixture.client(t, "*")
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	stream, err := rpc.NewShikiDaemonClient(client).WatchPorts(ctx, &pb.WatchPortsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if _, err = stream.Receive(); err != nil {
		t.Fatal("first port event", err)
	}
	select {
	case <-time.After(limits.PortFirstFrameTimeout + 150*time.Millisecond):
	case <-ctx.Done():
		t.Fatal("port Watch ended early", ctx.Err())
	}
	if service.portCalls.watches.Load() != 1 {
		t.Fatal("live port Watch lost slot after five seconds")
	}
	close(release)
	if _, err = stream.Receive(); err != nil {
		t.Fatal("port Watch failed after five seconds", err)
	}
	if _, err = stream.Receive(); !errors.Is(err, io.EOF) {
		t.Fatal("port Watch did not finish cleanly", err)
	}
	waitFileCount(t, &service.portCalls.watches, 0)
}
