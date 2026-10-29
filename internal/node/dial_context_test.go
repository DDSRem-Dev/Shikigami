package node

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	agentpb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	"tailscale.com/client/local"
	"tailscale.com/tsnet"
)

func TestDialStatusPreservesCallerCancellation(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		name := "cancel"
		wantCode, wantCause := connect.CodeCanceled, context.Canceled
		if deadline {
			name = "deadline"
			wantCode, wantCause = connect.CodeDeadlineExceeded, context.DeadlineExceeded
		}
		t.Run(name, func(t *testing.T) {
			entered := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				close(entered)
				<-r.Context().Done()
			}))
			defer server.Close()
			transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "tcp", server.Listener.Addr().String())
			}}
			defer transport.CloseIdleConnections()
			node := &Node{
				server:        &tsnet.Server{},
				local:         &local.Client{OmitAuth: true, Transport: transport},
				status:        &agentpb.NodeStatus{State: "online"},
				authorization: context.Background(),
			}
			duration := 5 * time.Second
			if deadline {
				duration = time.Second
			}
			ctx, cancel := context.WithTimeout(context.Background(), duration)
			defer cancel()
			done := make(chan error, 1)
			go func() {
				conn, err := node.Dial(ctx, "tcp", "100.64.0.2:9527")
				if conn != nil {
					conn.Close()
				}
				done <- err
			}()
			select {
			case <-entered:
			case <-ctx.Done():
				t.Fatal("status request did not reach LocalAPI")
			}
			if !deadline {
				cancel()
			}
			select {
			case err := <-done:
				if connect.CodeOf(err) != wantCode || !errors.Is(err, wantCause) || connection.IsRetryableNetworkError(err) {
					t.Fatalf("caller cancellation became a retryable/status error: %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("status cancellation did not stop Dial")
			}
		})
	}
}
