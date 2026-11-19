package client

import (
	"context"
	"net"
	"strconv"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin/pluginconnect"
)

func TestTargetRefusalIsolatedOnlyBeforeConnectedWithExactMarker(t *testing.T) {
	for _, test := range []struct {
		mode, state, reason string
		isolated            bool
	}{
		{"receive-target-refused", "listening", "", true},
		{"eof-target-refused", "listening", "", true},
		{"eof-target-metadata", "offline", "REMOTE_UNAVAILABLE", false},
		{"eof-unavailable", "offline", "REMOTE_UNAVAILABLE", false},
		{"eof-unauthenticated", "auth_required", "REMOTE_AUTH_REQUIRED", false},
		{"eof-permission", "error", "REMOTE_TARGET_DENIED", false},
		{"established-target-refused", "offline", "REMOTE_UNAVAILABLE", false},
	} {
		t.Run(test.mode, func(t *testing.T) {
			stable, fault, fresh := newHandshakeStream("echo"), newHandshakeStream(test.mode), newHandshakeStream("echo")
			transport := &handshakeTransport{plans: make(chan *handshakeStream, 3)}
			transport.plans <- stable
			transport.plans <- fault
			transport.plans <- fresh
			w := New()
			if err := w.Init(t.Context(), sdk.InitContext{Core: rpc.NewCoreAPIClient(connect.NewClient(transport)), Side: "client", StateDir: t.TempDir()}); err != nil {
				t.Fatal(err)
			}
			if err := w.Start(t.Context()); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				if err := w.Stop(ctx); err != nil {
					t.Error(err)
				}
			})
			state, err := w.StartForward(t.Context(), &ppb.ForwardSpec{Id: "refusal", ServerId: "server", RemotePort: 3000, Enabled: true})
			if err != nil {
				t.Fatal(err)
			}
			bound := state.BoundPort
			address := net.JoinHostPort("127.0.0.1", strconv.Itoa(int(bound)))
			dial := func() *net.TCPConn {
				t.Helper()
				conn, err := net.DialTimeout("tcp4", address, time.Second)
				if err != nil {
					t.Fatal("listener was lost", err)
				}
				t.Cleanup(func() { conn.Close() })
				return conn.(*net.TCPConn)
			}
			first := dial()
			handshakeEcho(t, first, "existing application")
			second := dial()
			handshakeSignal(t, fault.opened)
			if test.mode == "established-target-refused" {
				handshakeEcho(t, second, "established data")
				close(fault.release)
			}
			handshakeSignal(t, fault.closed)
			handshakeClosedTCP(t, second)
			if test.isolated {
				handshakeState(t, w, func(s *ppb.ForwardState) bool {
					return s.State == test.state && s.ReasonCode == test.reason && s.BoundPort == bound && s.Connections == 1
				})
				handshakeEcho(t, first, "same existing stream")
				third := dial()
				handshakeEcho(t, third, "fresh application connection")
				if transport.calls.Load() != 3 {
					t.Fatal("refusal retried or replayed TCP", transport.calls.Load())
				}
			} else {
				handshakeState(t, w, func(s *ppb.ForwardState) bool {
					return s.State == test.state && s.ReasonCode == test.reason && s.BoundPort == 0 && s.Connections == 0
				})
				handshakeClosedTCP(t, first)
				if transport.calls.Load() != 2 {
					t.Fatal("task-level failure retried TCP", transport.calls.Load())
				}
			}
			if err := w.StopForward(t.Context(), &ppb.ForwardID{Id: "refusal"}); err != nil {
				t.Fatal(err)
			}
			handshakeState(t, w, func(s *ppb.ForwardState) bool { return s.BoundPort == 0 && s.Connections == 0 && s.State == "stopped" })
		})
	}
}
