//go:build darwin || linux

package client

import (
	"context"
	"testing"
	"time"

	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
	"github.com/creack/pty"
)

// A takeover confirmation must synchronize the actual current terminal size,
// including changes while observing, without waiting for another SIGWINCH.
func TestTerminalControlAcquisitionSynchronizesCurrentSize(t *testing.T) {
	for _, mode := range []string{"acquired", "still-observer", "read-only"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			master, slave, err := pty.Open()
			if err != nil {
				t.Fatal(err)
			}
			defer master.Close()
			defer slave.Close()
			if err := pty.Setsize(slave, &pty.Winsize{Rows: 31, Cols: 101}); err != nil {
				t.Fatal(err)
			}
			reader, err := openTerminalFD(master)
			if err != nil {
				t.Fatal(err)
			}
			defer reader.Close()
			confirm := make(chan struct{})
			received := make(chan *pb.TerminalInput, 8)
			opened := make(chan *pb.TerminalOpen, 1)
			probe := &frontendProbe{terminal: func(ctx context.Context, stream rpc.ShikiDaemonTerminalStreamServerStream) error {
				first, err := stream.Receive()
				if err != nil {
					return err
				}
				opened <- first.GetOpen()
				send := func(sequence uint64, event *pb.SessionEvent, data string) error {
					frame := &pb.TerminalOutput{SessionId: "session", AttachmentId: "observer", Sequence: sequence}
					if event != nil {
						frame.Payload = &pb.TerminalOutput_Event{Event: event}
					} else {
						frame.Payload = &pb.TerminalOutput_Data{Data: []byte(data)}
					}
					return stream.Send(frame)
				}
				if err := send(1, &pb.SessionEvent{Type: pb.SessionEvent_READY, Data: map[string]string{"mode": "observer", "controller_attachment_id": "other"}}, ""); err != nil {
					return err
				}
				if err := send(2, nil, "OBSERVING"); err != nil {
					return err
				}
				select {
				case <-confirm:
				case <-ctx.Done():
					return ctx.Err()
				}
				controller := "observer"
				if mode == "still-observer" {
					controller = "other"
				}
				if err := send(3, &pb.SessionEvent{Type: pb.SessionEvent_CONTROL_CHANGED, Data: map[string]string{"controller_attachment_id": controller}}, ""); err != nil {
					return err
				}
				if err := send(4, nil, "CONFIRMED"); err != nil {
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
			}}
			client := frontendFixture(t, probe)()
			done := make(chan error, 1)
			go func() {
				done <- client.Terminal(ctx, "server", "session", TerminalOptions{Input: slave, Output: slave, ReadOnly: mode == "read-only"})
			}()
			defer func() { cancel(); <-done }()
			waitTerminalText(t, ctx, reader, "OBSERVING")
			initial := <-opened
			if initial.GetSize().GetRows() != 31 || initial.GetSize().GetCols() != 101 {
				t.Fatal("initial observer size", initial)
			}
			// This PTY is not this process's controlling terminal. No SIGWINCH
			// is sent: the acquisition itself must read these new dimensions.
			if err := pty.Setsize(slave, &pty.Winsize{Rows: 37, Cols: 121}); err != nil {
				t.Fatal(err)
			}
			select {
			case frame := <-received:
				t.Fatal("observer sent input before control confirmation", frame)
			default:
			}
			close(confirm)
			waitTerminalText(t, ctx, reader, "CONFIRMED")
			if mode == "acquired" {
				select {
				case frame := <-received:
					if frame.GetResize().GetRows() != 37 || frame.GetResize().GetCols() != 121 {
						t.Fatal("takeover did not send current dimensions", frame)
					}
				case <-time.After(time.Second):
					t.Fatal("confirmed controller sent no resize without SIGWINCH")
				}
			} else {
				select {
				case frame := <-received:
					t.Fatal("unconfirmed or read-only observer sent resize", frame)
				case <-time.After(100 * time.Millisecond):
				}
			}
		})
	}
}
