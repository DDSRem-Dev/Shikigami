//go:build darwin || linux

package client

import (
	"bytes"
	"connectrpc.com/connect/v2"
	"context"
	"errors"
	"golang.org/x/term"
	"io"
	"reflect"
	"testing"
	"time"

	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
	"github.com/creack/pty"
)

func TestTerminalTakeoverWaitsForStreamConfirmation(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
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
	requested, confirm := make(chan *pb.TakeControlRequest, 1), make(chan struct{})
	states, received := make(chan TerminalState, 1), make(chan *pb.TerminalInput, 8)
	probe := &frontendProbe{
		takeControl: func(_ context.Context, request *pb.TakeControlRequest) (*pb.Empty, error) {
			requested <- request
			return &pb.Empty{}, nil // A successful unary response is deliberately early.
		},
		terminal: func(ctx context.Context, stream rpc.ShikiDaemonTerminalStreamServerStream) error {
			if _, err := stream.Receive(); err != nil {
				return err
			}
			send := func(seq uint64, event *pb.SessionEvent, data string) error {
				frame := &pb.TerminalOutput{SessionId: "session", AttachmentId: "attachment", Sequence: seq}
				if event != nil {
					frame.Payload = &pb.TerminalOutput_Event{Event: event}
				} else {
					frame.Payload = &pb.TerminalOutput_Data{Data: []byte(data)}
				}
				return stream.Send(frame)
			}
			if err := send(1, &pb.SessionEvent{Type: pb.SessionEvent_READY, Data: map[string]string{"mode": "observer", "control_revision": "old", "controller_attachment_id": "other"}}, ""); err != nil {
				return err
			}
			if err := send(2, nil, "BEFORE-CONFIRMATION"); err != nil {
				return err
			}
			select {
			case <-confirm:
			case <-ctx.Done():
				return ctx.Err()
			}
			if err := send(3, &pb.SessionEvent{Type: pb.SessionEvent_CONTROL_CHANGED, Data: map[string]string{"control_revision": "new", "controller_attachment_id": "attachment"}}, ""); err != nil {
				return err
			}
			if err := send(4, nil, "AFTER-CONFIRMATION"); err != nil {
				return err
			}
			for {
				frame, err := stream.Receive()
				if err != nil {
					return err
				}
				select {
				case received <- frame:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		},
	}
	client := frontendFixture(t, probe)()
	done := make(chan error, 1)
	go func() {
		done <- client.Terminal(ctx, "server", "session", TerminalOptions{Input: slave, Output: slave, TakeControl: true, BeforeRaw: func(state TerminalState) { states <- state }})
	}()
	defer func() { cancel(); <-done }()
	select {
	case request := <-requested:
		if request.SessionId != "session" || request.AttachmentId != "attachment" || request.ExpectedControlRevision != "old" || request.RequestId == "" {
			t.Fatal("invalid takeover request", request)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	select {
	case state := <-states:
		t.Fatal("BeforeRaw ran before stream confirmed takeover", state)
	case <-time.After(100 * time.Millisecond):
	}
	// Bytes entered before the confirmed transition must never become input.
	if _, err := master.Write([]byte("STALE-INPUT\n")); err != nil {
		t.Fatal(err)
	}
	if err := pty.Setsize(slave, &pty.Winsize{Rows: 37, Cols: 121}); err != nil {
		t.Fatal(err)
	}
	close(confirm)
	select {
	case state := <-states:
		if state.Mode != "controller" || state.ControlRevision != "new" || state.AttachmentID != "attachment" {
			t.Fatal("stale BeforeRaw state", state)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	// Both queued pre-confirmation output and live output survive, in order.
	waitTerminalText(t, ctx, reader, "BEFORE-CONFIRMATIONAFTER-CONFIRMATION")
	select {
	case frame := <-received:
		if frame.GetResize().GetRows() != 37 || frame.GetResize().GetCols() != 121 {
			t.Fatal("acquisition must synchronize live size before input", frame)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if _, err := master.Write([]byte("FRESH-INPUT")); err != nil {
		t.Fatal(err)
	}
	select {
	case frame := <-received:
		if string(frame.GetData()) != "FRESH-INPUT" {
			t.Fatal("input before confirmation replayed", frame)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

// Failure during takeover must leave the caller's terminal untouched and must
// not present a controller label from the unary response alone.
func TestTerminalTakeoverFailureDoesNotEnterRaw(t *testing.T) {
	for _, scenario := range []struct {
		name string
		code connect.Code
	}{
		{"cancel", connect.CodeCanceled},
		{"confirmation-timeout", connect.CodeDeadlineExceeded},
		{"peer-failure", connect.CodePermissionDenied},
		{"peer-eof", connect.CodeUnknown},
		{"claude-crashed", connect.CodeUnknown},
		{"wrong-attachment", connect.CodeDataLoss},
		{"repeated-ready", connect.CodeDataLoss},
		{"wrong-sequence", connect.CodeDataLoss},
		{"old-revision", connect.CodeDataLoss},
		{"frame-overflow", connect.CodeResourceExhausted},
		{"byte-overflow", connect.CodeResourceExhausted},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
			defer cancel()
			master, slave, err := pty.Open()
			if err != nil {
				t.Fatal(err)
			}
			defer master.Close()
			defer slave.Close()
			if err := pty.Setsize(slave, &pty.Winsize{Rows: 24, Cols: 80}); err != nil {
				t.Fatal(err)
			}
			before, err := term.GetState(int(slave.Fd()))
			if err != nil {
				t.Fatal(err)
			}
			requested := make(chan struct{})
			probe := &frontendProbe{
				takeControl: func(context.Context, *pb.TakeControlRequest) (*pb.Empty, error) {
					close(requested)
					return &pb.Empty{}, nil
				},
				terminal: func(ctx context.Context, stream rpc.ShikiDaemonTerminalStreamServerStream) error {
					if _, err := stream.Receive(); err != nil {
						return err
					}
					if err := stream.Send(&pb.TerminalOutput{SessionId: "session", AttachmentId: "attachment", Sequence: 1, Payload: &pb.TerminalOutput_Event{Event: &pb.SessionEvent{Type: pb.SessionEvent_READY, Data: map[string]string{"mode": "observer", "control_revision": "old"}}}}); err != nil {
						return err
					}
					select {
					case <-requested:
					case <-ctx.Done():
						return ctx.Err()
					}
					switch scenario.name {
					case "cancel":
						cancel()
						<-ctx.Done()
						return ctx.Err()
					case "peer-eof":
						return nil
					case "peer-failure":
						return connect.NewError(connect.CodePermissionDenied, "revoked")
					}
					count := 1
					if scenario.name == "byte-overflow" {
						count = 5
					}
					if scenario.name == "frame-overflow" {
						count = 65
					}
					for i := 0; i < count; i++ {
						frame := &pb.TerminalOutput{SessionId: "session", AttachmentId: "attachment", Sequence: uint64(i + 2), Payload: &pb.TerminalOutput_Data{Data: []byte("pending")}}
						switch scenario.name {
						case "claude-crashed":
							frame.Payload = &pb.TerminalOutput_Event{Event: &pb.SessionEvent{Type: pb.SessionEvent_CLAUDE_CRASHED, Data: map[string]string{"exit_code": "137"}}}
						case "wrong-attachment":
							frame.AttachmentId = "other"
						case "repeated-ready":
							frame.Payload = &pb.TerminalOutput_Event{Event: &pb.SessionEvent{Type: pb.SessionEvent_READY}}
						case "wrong-sequence":
							frame.Sequence = 7
						case "old-revision":
							frame.Payload = &pb.TerminalOutput_Event{Event: &pb.SessionEvent{Type: pb.SessionEvent_CONTROL_CHANGED, Data: map[string]string{"controller_attachment_id": "attachment", "control_revision": "old"}}}
						case "byte-overflow":
							frame.Payload = &pb.TerminalOutput_Data{Data: bytes.Repeat([]byte("x"), 256<<10)}
						}
						if err := stream.Send(frame); err != nil {
							return err
						}
					}
					<-ctx.Done()
					return ctx.Err()
				},
			}
			client := frontendFixture(t, probe)()
			shown := false
			err = client.Terminal(ctx, "server", "session", TerminalOptions{Input: slave, Output: slave, TakeControl: true, BeforeRaw: func(TerminalState) { shown = true }})
			if connect.CodeOf(err) != scenario.code {
				t.Fatalf("got %v, want %v", err, scenario.code)
			}
			if scenario.name == "peer-eof" && !errors.Is(err, io.EOF) {
				t.Fatalf("unconfirmed clean EOF must fail attachment: %v", err)
			}
			if scenario.name == "claude-crashed" {
				var crash *ClaudeExitError
				if !errors.As(err, &crash) || crash.ExitCode != "137" {
					t.Fatalf("lost Claude crash reason: %v", err)
				}
			}
			if shown {
				t.Fatal("presented attachment before takeover confirmation")
			}
			after, err := term.GetState(int(slave.Fd()))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, after) {
				t.Fatal("failed takeover changed terminal state")
			}
		})
	}
}

func TestTerminalTimeoutStopJoinsCallback(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	stop := terminalTimeout(0, func() { close(entered); <-release })
	<-entered
	stopped := make(chan struct{})
	go func() { stop(); close(stopped) }()
	select {
	case <-stopped:
		t.Fatal("stop did not join active callback")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("stop failed to return after callback")
	}
	stop() // The normal-path call and deferred call must both be safe.
}
