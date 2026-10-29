package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	"github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	"github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
)

type authTestHandler struct {
	protocolconnect.UnimplementedShikiDaemonHandler
	finished chan struct{}
}

func (h *authTestHandler) TerminalStream(ctx context.Context, stream protocolconnect.ShikiDaemonTerminalStreamServerStream) error {
	defer close(h.finished)
	if _, err := stream.Receive(); err != nil {
		return err
	}
	if err := stream.Send(&protocol.TerminalOutput{}); err != nil {
		return err
	}
	// Deliberately block in the transport (not a context select) to prove
	// revocation wakes a completely silent terminal sender.
	_, err := stream.Receive()
	return err
}

func (h *authTestHandler) HealthCheck(context.Context, *protocol.Empty) (*protocol.HealthStatus, error) {
	return &protocol.HealthStatus{}, nil
}

func TestHTTP2RevocationInterruptsBlockedReadWithoutKillingOtherCalls(t *testing.T) {
	m := openManager(t, filepath.Join(t.TempDir(), "auth"))
	token, info := createToken(t, m, "session.attach")
	healthToken, _ := createToken(t, m, "system.info")
	handler := &authTestHandler{finished: make(chan struct{})}
	server := connect.NewServer(m.Interceptor())
	protocolconnect.RegisterShikiDaemonHandler(server, handler)
	mux := http.NewServeMux()
	connecthttp.Mount(mux, server, connecthttp.WithRequireConnectProtocolHeader())
	httpServer := httptest.NewUnstartedServer(m.HTTPMiddleware(mux))
	httpServer.EnableHTTP2 = true
	httpServer.StartTLS()
	defer httpServer.Close()
	client := protocolconnect.NewShikiDaemonClient(connect.NewClient(connecthttp.NewTransport(httpServer.Client(), httpServer.URL)))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	terminalCtx, headers := connect.NewClientContext(ctx)
	headers.RequestHeader().Set("Authorization", "Bearer "+token)
	stream, err := client.TerminalStream(terminalCtx)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if err = stream.Send(&protocol.TerminalInput{}); err != nil {
		t.Fatal(err)
	}
	if _, err = stream.Receive(); err != nil {
		t.Fatal(err)
	} // still haven't CloseSend
	if err = m.Revoke(info.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-handler.finished:
	case <-time.After(2 * time.Second):
		t.Fatal("revocation left HTTP/2 Receive blocked")
	}
	if _, err = stream.Receive(); err == nil {
		t.Fatal("revoked stream continued successfully")
	}
	healthCtx, healthHeaders := connect.NewClientContext(ctx)
	healthHeaders.RequestHeader().Set("Authorization", "Bearer "+healthToken)
	if _, err = client.HealthCheck(healthCtx, &protocol.Empty{}); err != nil {
		t.Fatalf("other credential/stream affected: %v", err)
	}
	if _, err = client.HealthCheck(ctx, &protocol.Empty{}); connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Fatalf("missing auth: %v", err)
	}
	deniedCtx, deniedHeaders := connect.NewClientContext(ctx)
	deniedHeaders.RequestHeader().Set("Authorization", "Bearer "+healthToken)
	if _, err = client.ListSessions(deniedCtx, &protocol.ListSessionsRequest{}); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("permission denied became %v", err)
	}
}
