package plugin

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	"github.com/DDSRem-Dev/Shikigami/internal/connection"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
)

// Observe the actual generated Send, without adding a blocking test gate. A
// small HTTP/2 stream window makes a non-reading client exhaust flow control.
// The healthy stream shares the same connection and stays independently usable.
type subscriptionSendProbe struct {
	started  chan struct{}
	returned chan struct{}
	ended    chan struct{}
	finished chan struct{}
}
type subscriptionObservedStream struct {
	connect.ServerStream
	probe *subscriptionSendProbe
}

func (s subscriptionObservedStream) Send(message any) error {
	s.probe.started <- struct{}{}
	err := s.ServerStream.Send(message)
	s.probe.returned <- struct{}{}
	return err
}
func (p *subscriptionSendProbe) intercept(next connect.ServerFunc) connect.ServerFunc {
	return func(ctx context.Context, spec connect.Spec, stream connect.ServerStream) error {
		info, _ := connect.CallInfoForServerContext(ctx)
		if info.RequestHeader().Get("X-Test-Slow-Subscriber") != "true" {
			return next(ctx, spec, stream)
		}
		defer close(p.ended)
		return next(ctx, spec, subscriptionObservedStream{stream, p})
	}
}
func subscriptionSignal(t *testing.T, signal <-chan struct{}, label string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(2 * time.Second):
		t.Fatal(label)
	}
}
func subscriptionEndpoint(t *testing.T, handler http.Handler, probe *subscriptionSendProbe) (*connect.Client, *atomic.Int32) {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "shiki-subscription-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	socket := filepath.Join(dir, "rpc.sock")
	listener, err := connection.ListenUnix(socket, uint32(os.Getuid()))
	if err != nil {
		t.Fatal(err)
	}
	var http2 atomic.Bool
	server := connection.NewHTTPServer(t.Context(), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor != 2 {
			t.Error("subscription did not use HTTP/2")
		}
		http2.Store(r.ProtoMajor == 2)
		if r.Header.Get("X-Test-Slow-Subscriber") == "true" {
			defer close(probe.finished)
		}
		handler.ServeHTTP(w, r)
	}))
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	t.Cleanup(func() {
		server.Close()
		<-done
		if !http2.Load() {
			t.Error("HTTP/2 was never observed")
		}
	})
	protocols := new(http.Protocols)
	protocols.SetUnencryptedHTTP2(true)
	calls := new(atomic.Int32)
	transport := &http.Transport{Protocols: protocols, MaxConnsPerHost: 1, HTTP2: &http.HTTP2Config{MaxReceiveBufferPerStream: 1024, MaxReceiveBufferPerConnection: 1 << 20},
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			calls.Add(1)
			return (&net.Dialer{}).DialContext(ctx, "unix", socket)
		}}
	t.Cleanup(transport.CloseIdleConnections)
	// This test-only transport advertises a small receive window. Production
	// socket listener/UID checks and generated Connect HTTP/2 handlers stay real.
	client := connect.NewClient(connecthttp.NewTransport(&http.Client{Transport: transport}, "http://shiki-subscription", connecthttp.WithCompressors()))
	return client, calls
}

type subscriptionLogHandler struct {
	rpc.UnimplementedShikiDaemonHandler
	manager *Supervisor
}

func (h subscriptionLogHandler) WatchPluginLogs(ctx context.Context, in *pb.PluginLogsRequest, out rpc.ShikiDaemonWatchPluginLogsServerStream) error {
	return h.manager.WatchLogs(ctx, in, out.Send)
}
func TestWatchLogsBlockedSendReleasesSubscription(t *testing.T) {
	for _, mode := range []string{"cancel", "close"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			manager := &Supervisor{ctx: ctx, slots: map[string]*supervised{"sync": {}}}
			if err := manager.RecordLog("sync", &pb.LogRecord{Level: "info", Message: "INITIAL"}); err != nil {
				t.Fatal(err)
			}
			probe := &subscriptionSendProbe{make(chan struct{}, 1), make(chan struct{}, 1), make(chan struct{}), make(chan struct{})}
			server := connect.NewServer(probe.intercept)
			rpc.RegisterShikiDaemonHandler(server, subscriptionLogHandler{manager: manager})
			raw, dials := subscriptionEndpoint(t, connection.NewRPCHandler(server), probe)
			client := rpc.NewShikiDaemonClient(raw)
			request := &pb.PluginLogsRequest{PluginId: "sync", Tail: 1, Follow: true}
			healthy, err := client.WatchPluginLogs(ctx, request)
			if err != nil {
				t.Fatal(err)
			}
			defer healthy.Close()
			if _, err = healthy.Receive(); err != nil {
				t.Fatal(err)
			}
			slowCtx, stopSlow := context.WithCancel(ctx)
			defer stopSlow()
			slowCtx, info := connect.NewClientContext(slowCtx)
			info.RequestHeader().Set("X-Test-Slow-Subscriber", "true")
			slow, err := client.WatchPluginLogs(slowCtx, request)
			if err != nil {
				t.Fatal(err)
			}
			defer slow.Close()
			if _, err = slow.Receive(); err != nil {
				t.Fatal(err)
			}
			subscriptionSignal(t, probe.started, "initial send missing")
			subscriptionSignal(t, probe.returned, "initial send blocked")
			count := func() int {
				manager.mu.Lock()
				defer manager.mu.Unlock()
				return len(manager.slots["sync"].subscribers)
			}
			if count() != 2 {
				t.Fatal("two live owners were not established")
			}
			blocked := false
			for i := 0; i < 128; i++ {
				code := fmt.Sprintf("RECORD_%03d_%s", i, strings.Repeat("X", 110))
				if err := manager.RecordLog("sync", &pb.LogRecord{Level: "info", Message: code}); err != nil {
					t.Fatal(err)
				}
				message, err := healthy.Receive()
				if err != nil || message.Message != code {
					t.Fatal("healthy subscription stopped before cancellation", err)
				}
				subscriptionSignal(t, probe.started, "slow handler never attempted Send")
				select {
				case <-probe.returned:
				case <-time.After(100 * time.Millisecond):
					blocked = true
				}
				if blocked {
					break
				}
			}
			if !blocked {
				t.Fatal("fixture never exhausted the real HTTP/2 stream window")
			}
			if count() != 2 {
				t.Fatal("slow subscription ended before cancellation")
			}
			select {
			case <-probe.ended:
				t.Fatal("handler exited instead of blocking")
			default:
			}
			if mode == "cancel" {
				stopSlow()
			} else if err := slow.Close(); err != nil {
				t.Fatal(err)
			}
			subscriptionSignal(t, probe.returned, "cancellation did not unblock actual Send")
			subscriptionSignal(t, probe.ended, "canceled RPC handler did not exit")
			subscriptionSignal(t, probe.finished, "HTTP handler/final status write did not exit")
			if count() != 1 {
				t.Fatalf("canceled subscriber retained: count=%d", count())
			}
			if err := manager.RecordLog("sync", &pb.LogRecord{Level: "info", Message: "AFTER_CANCEL"}); err != nil {
				t.Fatal(err)
			}
			message, err := healthy.Receive()
			if err != nil || message.Message != "AFTER_CANCEL" {
				t.Fatal("healthy stream lost shared connection", err)
			}
			if dials.Load() != 1 || ctx.Err() != nil {
				t.Fatalf("connection replaced or parent canceled: dials=%d err=%v", dials.Load(), ctx.Err())
			}
			if err = healthy.Close(); err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(2 * time.Second)
			for count() != 0 && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			if count() != 0 {
				t.Fatal("healthy subscription did not release its owner")
			}
			t.Log("observed blocked HTTP/2 Send; canceled owner removed; independent stream remained live on one connection")
		})
	}
}
