//go:build darwin || linux

package main

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/client"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	apb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/agent"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
	"github.com/creack/pty"
	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

type attachErrorRemote struct {
	rpc.UnimplementedShikiDaemonHandler
	code   connect.Code
	active bool
	calls  atomic.Int32
}

func (p *attachErrorRemote) TerminalStream(ctx context.Context, stream rpc.ShikiDaemonTerminalStreamServerStream) error {
	p.calls.Add(1)
	if _, err := stream.Receive(); err != nil {
		return err
	}
	if err := stream.Send(&pb.TerminalOutput{SessionId: "session", AttachmentId: "attachment", Sequence: 1, Payload: &pb.TerminalOutput_Event{Event: &pb.SessionEvent{Type: pb.SessionEvent_READY, Data: map[string]string{"mode": "observer"}}}}); err != nil {
		return err
	}
	if p.active {
		if err := stream.Send(&pb.TerminalOutput{SessionId: "session", AttachmentId: "attachment", Sequence: 2, Payload: &pb.TerminalOutput_Data{Data: []byte("ATTACH_ACTIVE")}}); err != nil {
			return err
		}
		<-ctx.Done()
		return ctx.Err()
	}
	return connect.NewError(p.code, "terminal result before diagnostics")
}

// The real terminal response arrives before verbose log collection expires the
// parent context. Diagnostics must not replace an authoritative terminal error.
func TestAttachTerminalErrorSurvivesDiagnosticCancellation(t *testing.T) {
	for _, tc := range []struct {
		name     string
		code     connect.Code
		deadline bool
		active   bool
	}{
		{"permission_denied/cancel", connect.CodePermissionDenied, false, false},
		{"permission_denied/deadline", connect.CodePermissionDenied, true, false},
		{"unauthenticated/cancel", connect.CodeUnauthenticated, false, false},
		{"unauthenticated/deadline", connect.CodeUnauthenticated, true, false},
		{"network_failure/cancel", connect.CodeUnavailable, false, false},
		{"active_terminal/cancel", connect.CodeCanceled, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			master, slave, err := pty.Open()
			if err != nil {
				t.Fatal(err)
			}
			process := smartPTYProcess{t: t, master: master, slave: slave}
			defer process.close()
			if err := pty.Setsize(slave, &pty.Winsize{Rows: 24, Cols: 80}); err != nil {
				t.Fatal(err)
			}
			fd := int(master.Fd())
			if err := unix.SetNonblock(fd, true); err != nil {
				t.Fatal(err)
			}
			saved, err := term.GetState(int(slave.Fd()))
			if err != nil {
				t.Fatal(err)
			}
			remote := &attachErrorRemote{code: tc.code, active: tc.active}
			probe := &managementProbe{remote: remote}
			path := managementFixture(t, probe)
			wait, stop := context.WithTimeout(t.Context(), 5*time.Second)
			defer stop()
			c, err := client.Open(wait, path, client.Options{Timeout: time.Second})
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			ctx, cancel := context.WithCancel(wait)
			if tc.deadline {
				cancel()
				ctx, cancel = context.WithTimeout(wait, time.Second)
			}
			defer cancel()
			var logCalls atomic.Int32
			var restoredBeforeLogs atomic.Bool
			probe.logCheck = func(*apb.PluginLogsRequest) error {
				logCalls.Add(1)
				state, e := term.GetState(int(slave.Fd()))
				restoredBeforeLogs.Store(e == nil && reflect.DeepEqual(saved, state))
				if !tc.deadline {
					cancel()
				}
				<-ctx.Done()
				return ctx.Err()
			}
			var diagnostics bytes.Buffer
			a := &app{input: slave, output: slave, errorOutput: &diagnostics, configPath: path, timeout: time.Second, verbose: true}
			done := process.start(cancel, func() error {
				return a.runTerminal(ctx, c, "server", &pb.Session{Id: "session", Name: "test-session", ClaudeStatus: "running"}, client.TerminalOptions{Input: slave, Output: slave, ReadOnly: true, DetachKey: 17}, true)
			})
			if tc.active {
				smartReadPTY(t, wait, fd, "ATTACH_ACTIVE")
				cancel()
			}
			if err := smartWaitPTY(wait, fd, process.finished); err != nil {
				t.Fatal("terminal did not finish", err)
			}
			got := <-done
			if tc.active || tc.code == connect.CodeUnavailable {
				if !errors.Is(got, context.Canceled) {
					t.Fatalf("local cancellation lost: %v", got)
				}
			} else if connect.CodeOf(got) != tc.code || errors.Is(got, context.Canceled) || errors.Is(got, context.DeadlineExceeded) {
				t.Fatalf("terminal error replaced during diagnostics: got %v, want %s", got, tc.code)
			}
			if remote.calls.Load() != 1 || strings.Contains(diagnostics.String(), "Reconnecting (") {
				t.Fatal("terminal was retried after cancellation or authoritative rejection")
			}
			if tc.active {
				if logCalls.Load() != 0 {
					t.Fatal("already-canceled terminal entered diagnostic RPCs")
				}
			} else if logCalls.Load() != 1 || !restoredBeforeLogs.Load() || ctx.Err() == nil {
				t.Fatalf("diagnostic cancellation fixture was not exercised after terminal restore: calls=%d restored=%t ended=%t", logCalls.Load(), restoredBeforeLogs.Load(), ctx.Err() != nil)
			}
			restored, err := term.GetState(int(slave.Fd()))
			if err != nil || !reflect.DeepEqual(saved, restored) {
				t.Fatal("terminal state was not restored", err)
			}
		})
	}
}
