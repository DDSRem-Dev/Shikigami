//go:build darwin || linux

package client

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
	"github.com/creack/pty"
	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

func TestTerminalAdmissionAndRestoration(t *testing.T) {
	for _, exit := range []string{"detach", "cancel", "server-error"} {
		t.Run(exit, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			master, slave, e := pty.Open()
			if e != nil {
				t.Fatal(e)
			}
			defer master.Close()
			defer slave.Close()
			if e = pty.Setsize(slave, &pty.Winsize{Rows: 24, Cols: 80}); e != nil {
				t.Fatal(e)
			}
			saved, e := term.GetState(int(slave.Fd()))
			if e != nil {
				t.Fatal(e)
			}
			originalFD := int(slave.Fd())
			flags, e := unix.FcntlInt(uintptr(originalFD), unix.F_GETFL, 0)
			if e != nil {
				t.Fatal(e)
			}
			reader, e := openTerminalFD(master)
			if e != nil {
				t.Fatal(e)
			}
			defer reader.Close()
			var attempts atomic.Int32
			received := make(chan *pb.TerminalInput, 8)
			breakServer := make(chan struct{})
			p := &frontendProbe{terminal: func(ctx context.Context, s rpc.ShikiDaemonTerminalStreamServerStream) error {
				first, e := s.Receive()
				if e != nil {
					return e
				}
				if first.GetOpen() == nil || first.GetOpen().SessionId != "session" {
					return connect.NewError(connect.CodeInvalidArgument, "missing Open")
				}
				if attempts.Add(1) <= 2 {
					return connect.NewError(connect.CodeResourceExhausted, "redraw busy")
				}
				if e = s.Send(&pb.TerminalOutput{SessionId: "session", AttachmentId: "attachment", Sequence: 1, Payload: &pb.TerminalOutput_Event{Event: &pb.SessionEvent{Type: pb.SessionEvent_READY, Data: map[string]string{"mode": "controller", "control_revision": "revision"}}}}); e != nil {
					return e
				}
				if e = s.Send(&pb.TerminalOutput{SessionId: "session", AttachmentId: "attachment", Sequence: 2, Payload: &pb.TerminalOutput_Data{Data: []byte("RAW-READY")}}); e != nil {
					return e
				}
				if exit == "server-error" {
					select {
					case <-ctx.Done():
						return ctx.Err()
					case <-breakServer:
						return connect.NewError(connect.CodePermissionDenied, "revoked")
					}
				}
				for {
					frame, e := s.Receive()
					if e != nil {
						return e
					}
					select {
					case received <- frame:
					case <-ctx.Done():
						return ctx.Err()
					}
				}
			}}
			c := frontendFixture(t, p)()
			done := make(chan error, 1)
			go func() {
				done <- c.Terminal(ctx, "server", "session", TerminalOptions{Input: slave, Output: slave, DetachKey: 17})
			}()
			waitTerminalText(t, ctx, reader, "RAW-READY")
			if attempts.Load() != 3 {
				t.Fatalf("admission attempts: %d", attempts.Load())
			}
			raw, e := term.GetState(originalFD)
			if e != nil || reflect.DeepEqual(raw, saved) {
				t.Fatal("terminal never became raw")
			}
			if exit != "server-error" {
				paste := []byte("\x1b[200~paste\x11\x1b[201~")
				if e = reader.Write(ctx, paste); e != nil {
					t.Fatal(e)
				}
				var actual []byte
				for !bytes.Equal(actual, paste) {
					select {
					case frame := <-received:
						actual = append(actual, frame.GetData()...)
						if len(actual) > len(paste) {
							t.Fatalf("unexpected input %q", actual)
						}
					case <-ctx.Done():
						t.Fatal("paste not delivered")
					}
				}
				if e = pty.Setsize(slave, &pty.Winsize{Rows: 31, Cols: 101}); e != nil {
					t.Fatal(e)
				}
				if e = syscall.Kill(os.Getpid(), syscall.SIGWINCH); e != nil {
					t.Fatal(e)
				}
				select {
				case frame := <-received:
					if frame.GetResize().GetRows() != 31 || frame.GetResize().GetCols() != 101 {
						t.Fatalf("resize: %v", frame)
					}
				case <-ctx.Done():
					t.Fatal("resize not delivered")
				}
			}
			switch exit {
			case "detach":
				if e = reader.Write(ctx, []byte{17}); e != nil {
					t.Fatal(e)
				}
			case "cancel":
				cancel()
			case "server-error":
				close(breakServer)
			}
			select {
			case e = <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("terminal pumps did not join")
			}
			switch exit {
			case "detach":
				if e != nil {
					t.Fatal(e)
				}
			case "cancel":
				if !errors.Is(e, context.Canceled) {
					t.Fatalf("cancel: %v", e)
				}
			case "server-error":
				if connect.CodeOf(e) != connect.CodePermissionDenied {
					t.Fatalf("server error: %v", e)
				}
			}
			restored, e := term.GetState(originalFD)
			if e != nil || !reflect.DeepEqual(saved, restored) {
				t.Fatalf("termios not restored: %v", e)
			}
			after, e := unix.FcntlInt(uintptr(originalFD), unix.F_GETFL, 0)
			if e != nil || (after^flags)&(unix.O_NONBLOCK|unix.O_APPEND|unix.O_ASYNC) != 0 {
				t.Fatalf("descriptor flags %x -> %x: %v", flags, after, e)
			}
			// A subsequent console reader must own all new input; no attach reader survives.
			if _, e = master.Write([]byte("next command\n")); e != nil {
				t.Fatal(e)
			}
			next, stop := context.WithTimeout(context.Background(), time.Second)
			defer stop()
			line, e := ReadConsoleLine(next, slave, 128)
			if e != nil || line != "next command" {
				t.Fatalf("lingering reader consumed input: %q %v", line, e)
			}
		})
	}
}
func waitTerminalText(t *testing.T, ctx context.Context, fd *terminalFD, want string) {
	t.Helper()
	var output []byte
	data := make([]byte, 4096)
	for !bytes.Contains(output, []byte(want)) {
		n, e := fd.Read(ctx, data)
		output = append(output, data[:n]...)
		if e != nil {
			t.Fatalf("waiting for %s in %q: %v", want, output, e)
		}
	}
}
func TestTerminalAdmissionCancellation(t *testing.T) {
	master, slave, e := pty.Open()
	if e != nil {
		t.Fatal(e)
	}
	defer master.Close()
	defer slave.Close()
	if e = pty.Setsize(slave, &pty.Winsize{Rows: 24, Cols: 80}); e != nil {
		t.Fatal(e)
	}
	var attempts atomic.Int32
	p := &frontendProbe{terminal: func(ctx context.Context, s rpc.ShikiDaemonTerminalStreamServerStream) error {
		_, e := s.Receive()
		if e != nil {
			return e
		}
		attempts.Add(1)
		return connect.NewError(connect.CodeResourceExhausted, "busy")
	}}
	c := frontendFixture(t, p)()
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	e = c.Terminal(ctx, "server", "session", TerminalOptions{Input: slave, Output: slave, DetachKey: 17})
	if !errors.Is(e, context.DeadlineExceeded) || attempts.Load() < 1 || attempts.Load() > 3 {
		t.Fatal(fmt.Sprintf("attempts=%d error=%v", attempts.Load(), e))
	}
}

// A terminal emulator may stop draining its PTY while the remote credential is
// revoked. The final RPC error must still release the attachment and raw mode.
func TestTerminalPeerFailureInterruptsBlockedOutput(t *testing.T) {
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer master.Close()
	defer slave.Close()
	if err := pty.Setsize(slave, &pty.Winsize{Rows: 24, Cols: 80}); err != nil {
		t.Fatal(err)
	}
	fd := int(slave.Fd())
	original, err := term.GetState(fd)
	if err != nil {
		t.Fatal(err)
	}
	final := make(chan struct{})
	probe := &frontendProbe{terminal: func(ctx context.Context, stream rpc.ShikiDaemonTerminalStreamServerStream) error {
		if _, err := stream.Receive(); err != nil {
			return err
		}
		if err := stream.Send(&pb.TerminalOutput{SessionId: "session", AttachmentId: "attachment", Sequence: 1, Payload: &pb.TerminalOutput_Event{Event: &pb.SessionEvent{Type: pb.SessionEvent_READY, Data: map[string]string{"mode": "controller"}}}}); err != nil {
			return err
		}
		if err := stream.Send(&pb.TerminalOutput{SessionId: "session", AttachmentId: "attachment", Sequence: 2, Payload: &pb.TerminalOutput_Data{Data: bytes.Repeat([]byte("x"), 256<<10)}}); err != nil {
			return err
		}
		close(final)
		return connect.NewError(connect.CodeUnauthenticated, "credential revoked")
	}}
	client := frontendFixture(t, probe)()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- client.Terminal(ctx, "server", "session", TerminalOptions{Input: slave, Output: slave, DetachKey: 17})
	}()
	select {
	case <-final:
	case <-ctx.Done():
		t.Fatal("server did not send output and final error")
	}
	select {
	case err = <-done:
	case <-time.After(time.Second):
		cancel()
		<-done
		t.Fatal("peer failure did not interrupt blocked local terminal output")
	}
	if connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Fatalf("lost final authentication error: %v", err)
	}
	restored, err := term.GetState(fd)
	if err != nil || !reflect.DeepEqual(restored, original) {
		t.Fatal("peer failure did not restore terminal", err)
	}
}

func TestTerminalOutputDrainAndOverflow(t *testing.T) {
	for _, overflow := range []bool{false, true} {
		name := "clean-eof-drains-bytes"
		if overflow {
			name = "slow-consumer-fails-explicitly"
		}
		t.Run(name, func(t *testing.T) {
			master, slave, err := pty.Open()
			if err != nil {
				t.Fatal(err)
			}
			defer master.Close()
			defer slave.Close()
			if err := pty.Setsize(slave, &pty.Winsize{Rows: 24, Cols: 80}); err != nil {
				t.Fatal(err)
			}
			reader, err := openTerminalFD(master)
			if err != nil {
				t.Fatal(err)
			}
			defer reader.Close()
			payload := bytes.Repeat([]byte("raw-世界\x1b[31m"), 15000)
			sent := make(chan struct{})
			probe := &frontendProbe{terminal: func(ctx context.Context, stream rpc.ShikiDaemonTerminalStreamServerStream) error {
				if _, err := stream.Receive(); err != nil {
					return err
				}
				if err := stream.Send(&pb.TerminalOutput{SessionId: "session", AttachmentId: "attachment", Sequence: 1, Payload: &pb.TerminalOutput_Event{Event: &pb.SessionEvent{Type: pb.SessionEvent_READY, Data: map[string]string{"mode": "controller"}}}}); err != nil {
					return err
				}
				count := 1
				if overflow {
					count = 16
				}
				for i := 0; i < count; i++ {
					if err := stream.Send(&pb.TerminalOutput{SessionId: "session", AttachmentId: "attachment", Sequence: uint64(i + 2), Payload: &pb.TerminalOutput_Data{Data: payload}}); err != nil {
						return err
					}
				}
				close(sent)
				return nil
			}}
			client := frontendFixture(t, probe)()
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() {
				done <- client.Terminal(ctx, "server", "session", TerminalOptions{Input: slave, Output: slave, DetachKey: 17})
			}()
			if !overflow {
				select {
				case <-sent:
				case <-ctx.Done():
					t.Fatal("server could not send finite output")
				}
				actual := make([]byte, len(payload))
				for offset := 0; offset < len(actual); {
					n, err := reader.Read(ctx, actual[offset:])
					offset += n
					if err != nil {
						t.Fatal("clean EOF truncated terminal output", err)
					}
				}
				if !bytes.Equal(actual, payload) {
					t.Fatal("terminal changed bytes before clean EOF")
				}
			}
			select {
			case err = <-done:
			case <-ctx.Done():
				cancel()
				<-done
				t.Fatal("terminal did not finish")
			}
			if overflow && connect.CodeOf(err) != connect.CodeResourceExhausted {
				t.Fatalf("overflow must fail explicitly: %v", err)
			}
			if !overflow && err != nil {
				t.Fatalf("clean EOF failed: %v", err)
			}
		})
	}
}
