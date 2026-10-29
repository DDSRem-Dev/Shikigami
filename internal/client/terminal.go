package client

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"connectrpc.com/connect/v2"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	rpc "github.com/DDSRem-Dev/Shikigami/pkg/protocol/protocolconnect"
	"golang.org/x/term"
	"google.golang.org/protobuf/proto"
)

var ErrDetached = errors.New("terminal detached")

// ClaudeExitError ends this attachment so callers can offer recovery only
// after Terminal has stopped its readers and restored the user's terminal.
type ClaudeExitError struct {
	SessionID  string
	ExitCode   string
	ExitSignal string
	ReasonCode string
}

func (e *ClaudeExitError) Error() string {
	if e.ReasonCode == "EXIT_REASON_UNKNOWN" {
		return "Claude process ended with an unknown exit reason in session " + e.SessionID
	}
	return "Claude stopped unexpectedly in session " + e.SessionID
}

type TerminalOptions struct {
	Input, Output         *os.File
	ReadOnly, TakeControl bool
	DetachKey             byte
	BeforeRaw             func(TerminalState)
}
type TerminalState struct{ SessionID, AttachmentID, Mode, ControlRevision string }
type queuedInput struct {
	frame *pb.TerminalInput
	epoch uint64
}

// Terminal runs one attachment. A reconnect must call it again with a fresh
// stream; no input queue or attachment identity is carried across attempts.
func (c *Client) Terminal(ctx context.Context, serverID, sessionID string, options TerminalOptions) (result error) {
	if options.Input == nil || options.Output == nil || !term.IsTerminal(int(options.Input.Fd())) || !term.IsTerminal(int(options.Output.Fd())) {
		return errors.New("attach requires terminal stdin and stdout")
	}
	if options.ReadOnly && options.TakeControl {
		return errors.New("--read-only and --take-control cannot be combined")
	}
	cols, rows, e := term.GetSize(int(options.Output.Fd()))
	if e != nil {
		return e
	}
	if rows <= 0 || cols <= 0 {
		return errors.New("terminal dimensions are unavailable")
	}
	call, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	remote := c.Remote(serverID)
	device, _ := os.Hostname()
	timeout := time.AfterFunc(5*time.Second, func() { cancel(connect.NewError(connect.CodeDeadlineExceeded, "terminal READY timed out")) })
	defer timeout.Stop()
	var stream rpc.ShikiDaemonTerminalStreamClientStream
	var ready *pb.TerminalOutput
	admissionDeadline := time.Now().Add(time.Second)
	for {
		stream, e = remote.TerminalStream(call)
		if e == nil {
			e = stream.Send(&pb.TerminalInput{Payload: &pb.TerminalInput_Open{Open: &pb.TerminalOpen{SessionId: sessionID, ReadOnly: options.ReadOnly, DeviceName: device, Size: &pb.ResizeEvent{Rows: uint32(rows), Cols: uint32(cols)}}}})
			if e == nil {
				ready, e = stream.Receive()
			} else if call.Err() == nil {
				// A peer can reject Open before its sender observes the final
				// status. Preserve that status rather than reporting send EOF.
				if _, final := stream.Receive(); final != nil {
					e = final
				}
			}
			if e == nil {
				break
			}
			_ = stream.Close()
		}
		// Simultaneous attachments can briefly contend for a tmux redraw.
		// Retry only admission: no terminal data has been accepted or replayed.
		if connect.CodeOf(e) != connect.CodeResourceExhausted || !time.Now().Before(admissionDeadline) {
			return terminalCause(call, e)
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-call.Done():
			timer.Stop()
			return context.Cause(call)
		case <-timer.C:
		}
	}
	timeout.Stop()
	defer stream.Close()
	if ready.SessionId != sessionID || ready.AttachmentId == "" || ready.Sequence == 0 || ready.GetEvent() == nil || ready.GetEvent().Type != pb.SessionEvent_READY {
		return connect.NewError(connect.CodeDataLoss, "invalid terminal READY frame")
	}
	mode := ready.GetEvent().Data["mode"]
	if mode != "controller" && mode != "observer" {
		return connect.NewError(connect.CodeDataLoss, "invalid terminal mode")
	}
	state := TerminalState{SessionID: sessionID, AttachmentID: ready.AttachmentId, Mode: mode, ControlRevision: ready.GetEvent().Data["control_revision"]}
	if options.TakeControl && mode != "controller" {
		if _, e = remote.TakeControl(call, &pb.TakeControlRequest{SessionId: sessionID, AttachmentId: ready.AttachmentId, ExpectedControlRevision: state.ControlRevision, RequestId: rand.Text()}); e != nil {
			return e
		}
	}
	if options.BeforeRaw != nil {
		options.BeforeRaw(state)
	}
	original, e := term.MakeRaw(int(options.Input.Fd()))
	if e != nil {
		return e
	}
	defer func() { result = errors.Join(result, term.Restore(int(options.Input.Fd()), original)) }()
	input, e := openTerminalFD(options.Input)
	if e != nil {
		return e
	}
	defer func() { result = errors.Join(result, input.Close()) }()
	output, e := openTerminalFD(options.Output)
	if e != nil {
		return e
	}
	defer func() { result = errors.Join(result, output.Close()) }()
	if e = input.DiscardPending(); e != nil {
		return e
	}
	// Restore display modes before restoring descriptor flags and termios.
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer stop()
		_ = output.Write(cleanup, []byte("\x1b[?1000l\x1b[?1002l\x1b[?1003l\x1b[?1006l\x1b[?1004l\x1b[?2004l\x1b[?1049l\x1b[?25h\x1b[0m"))
	}()
	// One atomic value binds input eligibility to its control generation. Odd
	// generations own control; every transition invalidates queued old input.
	var control atomic.Uint64
	if mode == "controller" && !options.ReadOnly {
		control.Store(1)
	}
	queue := make(chan queuedInput, 32)
	resized := make(chan os.Signal, 1)
	signal.Notify(resized, syscall.SIGWINCH)
	inputDone, senderDone, receiverDone := make(chan struct{}), make(chan struct{}), make(chan struct{})
	finishSend := func(err error) {
		// Send may fail because Receive has observed the peer's final status
		// and closed the transport. Let that sole receiver decide the status
		// before treating a write failure as an independent transport failure.
		deadline := time.NewTimer(time.Second)
		defer deadline.Stop()
		select {
		case <-receiverDone:
		case <-call.Done():
		case <-deadline.C:
			cancel(err)
		}
	}
	go func() {
		defer close(inputDone)
		decoder := InputDecoder{Detach: options.DetachKey}
		data := make([]byte, 8192)
		for {
			n, e := input.Read(call, data)
			if n > 0 {
				bytes, detach := decoder.Feed(data[:n])
				if detach {
					cancel(ErrDetached)
					return
				}
				generation := control.Load()
				if len(bytes) > 0 && generation%2 != 0 {
					item := queuedInput{frame: &pb.TerminalInput{Payload: &pb.TerminalInput_Data{Data: bytes}}, epoch: generation}
					select {
					case queue <- item:
					case <-call.Done():
						return
					default:
						cancel(connect.NewError(connect.CodeResourceExhausted, "terminal input queue is full; detached without replay"))
						return
					}
				}
			}
			if e != nil {
				if errors.Is(e, io.EOF) {
					cancel(ErrDetached)
				} else {
					cancel(e)
				}
				return
			}
		}
	}()
	go func() {
		defer close(senderDone)
		for {
			select {
			case <-call.Done():
				return
			case item := <-queue:
				generation := control.Load()
				if generation%2 == 0 || item.epoch != generation {
					continue
				}
				if e := stream.Send(item.frame); e != nil {
					finishSend(e)
					return
				}
			case <-resized:
				generation := control.Load()
				if generation%2 == 0 {
					continue
				}
				cols, rows, e := term.GetSize(int(options.Output.Fd()))
				if e != nil {
					cancel(e)
					return
				}
				if cols <= 0 || rows <= 0 {
					continue
				}
				if control.Load() != generation {
					continue
				}
				if e = stream.Send(&pb.TerminalInput{Payload: &pb.TerminalInput_Resize{Resize: &pb.ResizeEvent{Rows: uint32(rows), Cols: uint32(cols)}}}); e != nil {
					finishSend(e)
					return
				}
			}
		}
	}()
	// Receive independently of local output so a remote failure can cancel a
	// PTY write even when the terminal emulator is no longer draining it. Both
	// frame count and serialized bytes are bounded; overflow ends this attachment
	// explicitly, never drops bytes and continues a corrupted display.
	outputs := make(chan *pb.TerminalOutput, 64)
	var queuedBytes atomic.Int64
	go func() {
		defer close(receiverDone)
		defer close(outputs)
		for {
			message, err := stream.Receive()
			if err != nil {
				if !errors.Is(err, io.EOF) {
					cancel(err)
				}
				return // Clean EOF drains every queued frame before returning.
			}
			if queuedBytes.Add(int64(proto.Size(message))) > 1<<20 {
				cancel(connect.NewError(connect.CodeResourceExhausted, "terminal output queue is full; detach and redraw required"))
				return
			}
			select {
			case outputs <- message:
			case <-call.Done():
				return
			default:
				cancel(connect.NewError(connect.CodeResourceExhausted, "terminal output queue is full; detach and redraw required"))
				return
			}
		}
	}()
	defer func() {
		cancel(nil)
		signal.Stop(resized)
		_ = stream.Close()
		<-inputDone
		<-senderDone
		<-receiverDone
	}()
	sequence := ready.Sequence
	for {
		var message *pb.TerminalOutput
		var ok bool
		select {
		case <-call.Done():
			return terminalCause(call, call.Err())
		case message, ok = <-outputs:
			if !ok {
				return terminalCause(call, nil)
			}
			queuedBytes.Add(-int64(proto.Size(message)))
		}
		if call.Err() != nil {
			return terminalCause(call, call.Err())
		}
		if message.SessionId != sessionID || message.AttachmentId != ready.AttachmentId || message.Sequence != sequence+1 {
			return connect.NewError(connect.CodeDataLoss, "terminal attachment or sequence changed")
		}
		sequence = message.Sequence
		if event := message.GetEvent(); event != nil {
			switch event.Type {
			case pb.SessionEvent_CLAUDE_CRASHED:
				return &ClaudeExitError{SessionID: sessionID, ExitCode: event.Data["exit_code"], ExitSignal: event.Data["exit_signal"]}
			case pb.SessionEvent_CLAUDE_STOPPED:
				if event.Data["reason_code"] == "EXIT_REASON_UNKNOWN" {
					return &ClaudeExitError{SessionID: sessionID, ReasonCode: "EXIT_REASON_UNKNOWN"}
				}
			case pb.SessionEvent_RESET:
				if e = output.Write(call, []byte("\x1bc")); e != nil {
					return terminalCause(call, e)
				}
			case pb.SessionEvent_CONTROL_CHANGED:
				next := event.Data["controller_attachment_id"] == ready.AttachmentId && !options.ReadOnly
				if (control.Load()%2 != 0) != next {
					control.Add(1)
					if next {
						// The observer's current size can differ from its Open
						// size. Wake the sole sender after confirmed acquisition;
						// it reads the live TTY and rechecks control before Send.
						select {
						case resized <- syscall.SIGWINCH:
						default:
						}
					}
				}
			case pb.SessionEvent_READY:
				return connect.NewError(connect.CodeDataLoss, "terminal READY repeated")
			}
			continue
		}
		if e = output.Write(call, message.GetData()); e != nil {
			return terminalCause(call, e)
		}
	}
}
func terminalCause(ctx context.Context, e error) error {
	if cause := context.Cause(ctx); cause != nil {
		if errors.Is(cause, ErrDetached) {
			return nil
		}
		return cause
	}
	return e
}
func ParseDetachKey(value string) (byte, error) {
	switch value {
	case "", "ctrl-q", "Ctrl+Q", "^Q":
		return 17, nil
	case "none", "off":
		return 0, nil
	case "ctrl-]", "Ctrl+]", "^]":
		return 29, nil
	default:
		return 0, fmt.Errorf("unsupported detach key %q; use ctrl-q, ctrl-], or none", value)
	}
}
