package client

import (
	"context"
	"errors"
	"io"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	config "github.com/DDSRem-Dev/Shikigami/pkg/plugin/config"
	ppb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/plugin/pluginconnect"
	"google.golang.org/protobuf/types/known/structpb"
)

func TestHandshakeTerminalBranchesPreserveAdmissionBoundary(t *testing.T) {
	for _, tc := range []struct {
		mode     string
		isolated bool
		reason   string
	}{
		{"open-resource", true, ""},
		{"send-resource", true, ""},
		{"receive-resource", true, ""},
		{"eof-resource", true, ""},
		{"eof-permission", false, "REMOTE_TARGET_DENIED"},
		{"eof-connected", false, "FORWARD_STREAM_FAILED"},
		{"eof-block", false, "FORWARD_STREAM_FAILED"},
		{"established-resource", false, "FORWARD_STREAM_FAILED"},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			stable, fault, replacement := newHandshakeStream("echo"), newHandshakeStream(tc.mode), newHandshakeStream("echo")
			transport := &handshakeTransport{plans: make(chan *handshakeStream, 3)}
			transport.plans <- stable
			transport.plans <- fault
			transport.plans <- replacement
			cfg, err := structpb.NewStruct(map[string]any{"max_connections": 2.0})
			if err != nil {
				t.Fatal(err)
			}
			defaults, err := config.Parse(cfg, "client")
			if err != nil {
				t.Fatal(err)
			}
			w := New()
			if err = w.Init(t.Context(), sdk.InitContext{Core: rpc.NewCoreAPIClient(connect.NewClient(transport)), Side: "client", StateDir: t.TempDir(), Config: cfg}); err != nil {
				t.Fatal(err)
			}
			lifetime, cancel := context.WithCancel(t.Context())
			defer cancel()
			if err = w.Start(lifetime); err != nil {
				t.Fatal(err)
			}
			// Cleanup is a failure safety net; all successful cases explicitly stop and
			// assert closure below before this cleanup or the parent cancellation runs.
			t.Cleanup(func() {
				ctx, stop := context.WithTimeout(context.Background(), 3*time.Second)
				defer stop()
				if err := w.Stop(ctx); err != nil {
					t.Error(err)
				}
			})
			state, err := w.StartForward(t.Context(), &ppb.ForwardSpec{Id: "branch", ServerId: "server", RemotePort: 3000, Enabled: true})
			if err != nil {
				t.Fatal(err)
			}
			bound := state.BoundPort
			address := net.JoinHostPort("127.0.0.1", strconv.Itoa(int(bound)))
			dial := func() *net.TCPConn {
				t.Helper()
				c, err := net.DialTimeout("tcp4", address, 3*time.Second)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { c.Close() })
				return c.(*net.TCPConn)
			}
			first := dial()
			handshakeSignal(t, stable.opened)
			handshakeEcho(t, first, "before-rejection")
			second := dial()
			handshakeSignal(t, fault.opened)
			if tc.mode == "established-resource" {
				handshakeEcho(t, second, "established-payload")
				// DATA has crossed the real TCP/Bridge path before the final error.
				close(fault.release)
			}
			if tc.mode != "open-resource" {
				handshakeSignal(t, fault.closed)
			}
			handshakeClosedTCP(t, second)
			if tc.mode == "eof-block" {
				handshakeSignal(t, fault.received)
				fault.mu.Lock()
				terminal := fault.finalError
				fault.mu.Unlock()
				if !errors.Is(terminal, context.Canceled) || lifetime.Err() != nil {
					t.Fatal("wrong cancellation owner", terminal, lifetime.Err())
				}
				if elapsed := time.Since(fault.started); elapsed < defaults.DialTimeout*9/10 || elapsed > defaults.DialTimeout+3*time.Second {
					t.Fatal("did not retain the original handshake budget", elapsed, defaults.DialTimeout)
				}
			}
			fault.mu.Lock()
			opens, receives, dataSends := fault.opens, fault.receives, fault.dataSends
			sent := string(fault.sentData)
			sendError, finalError := fault.sendError, fault.finalError
			fault.mu.Unlock()
			if strings.HasPrefix(tc.mode, "eof-") && !errors.Is(sendError, io.EOF) {
				t.Fatal("Send EOF branch was not triggered", sendError)
			}
			if tc.mode == "receive-resource" || tc.mode == "eof-resource" || tc.mode == "established-resource" {
				if connect.CodeOf(finalError) != connect.CodeResourceExhausted {
					t.Fatal("controlled final resource error was not observed", finalError)
				}
			}
			switch tc.mode {
			case "open-resource":
				if opens != 0 || receives != 0 {
					t.Fatal("failed open used stream", opens, receives)
				}
				handshakeSignal(t, fault.ctx.Done())
			case "send-resource":
				if opens != 1 || receives != 0 || connect.CodeOf(sendError) != connect.CodeResourceExhausted {
					t.Fatal("non-EOF send read a terminal status", opens, receives)
				}
			case "established-resource":
				if opens != 1 || dataSends == 0 || sent != "established-payload" {
					t.Fatal("established failure lacked actual payload", opens, dataSends, sent)
				}
			default:
				if opens != 1 || receives != 1 || dataSends != 0 {
					t.Fatal("handshake replay, missing final Receive or unintended Bridge", opens, receives, dataSends)
				}
			}
			if tc.isolated {
				handshakeState(t, w, func(s *ppb.ForwardState) bool {
					return s.State == "listening" && s.BoundPort == bound && s.Connections == 1 && s.ReasonCode == ""
				})
				handshakeEcho(t, first, "after-rejection")
				third := dial()
				handshakeSignal(t, replacement.opened)
				handshakeEcho(t, third, "replacement-payload")
				handshakeState(t, w, func(s *ppb.ForwardState) bool {
					return s.State == "listening" && s.BoundPort == bound && s.Connections == 2
				})
				handshakeEcho(t, first, "after-replacement")
				// Normal half-close must receive final HALF_CLOSE/CLOSE/EOF and return its
				// slot while the original connection keeps echoing on the same listener.
				if err := third.CloseWrite(); err != nil {
					t.Fatal(err)
				}
				if err := third.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
					t.Fatal(err)
				}
				if n, err := third.Read(make([]byte, 1)); n != 0 || !errors.Is(err, io.EOF) {
					t.Fatal("replacement did not half-close normally", n, err)
				}
				handshakeSignal(t, replacement.closed)
				handshakeState(t, w, func(s *ppb.ForwardState) bool {
					return s.Connections == 1 && s.BoundPort == bound && s.State == "listening"
				})
				handshakeEcho(t, first, "after-half-close")
				if transport.calls.Load() != 3 {
					t.Fatal("unexpected stream replay", transport.calls.Load())
				}
			} else {
				final := handshakeState(t, w, func(s *ppb.ForwardState) bool { return s.State == "error" && s.BoundPort == 0 && s.Connections == 0 })
				if final.ReasonCode != tc.reason {
					t.Fatal("wrong failure classification", final)
				}
				handshakeClosedTCP(t, first)
				handshakeSignal(t, stable.closed)
				if transport.calls.Load() != 2 {
					t.Fatal("failed task retried", transport.calls.Load())
				}
			}
			if err := w.StopForward(t.Context(), &ppb.ForwardID{Id: "branch"}); err != nil {
				t.Fatal(err)
			}
			handshakeSignal(t, stable.closed)
			handshakeState(t, w, func(s *ppb.ForwardState) bool { return s.State == "stopped" && s.BoundPort == 0 && s.Connections == 0 })
			if err := w.Stop(t.Context()); err != nil {
				t.Fatal(err)
			}
			if lifetime.Err() != nil {
				t.Fatal("test cancellation manufactured cleanup", lifetime.Err())
			}
			listener, err := net.Listen("tcp4", address)
			if err != nil {
				t.Fatal("listener was not released", err)
			}
			listener.Close()
		})
	}
}
