package session

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
)

type backpressuredTerminal struct {
	*fakeTerminal
	entered chan struct{}
	unblock chan struct{}
}

func (t *backpressuredTerminal) Write(data []byte) (int, error) {
	close(t.entered)
	<-t.unblock
	return len(data), nil
}

func TestOutputContinuesWhileInputIsBackpressured(t *testing.T) {
	r, w := io.Pipe()
	terminal := &backpressuredTerminal{fakeTerminal: &fakeTerminal{r: r, w: w}, entered: make(chan struct{}), unblock: make(chan struct{})}
	b := NewBridge(t.Context(), "sess_test", func(context.Context, string, uint32, uint32) (Terminal, error) { return terminal, nil }, TerminalOptions{})
	t.Cleanup(func() { b.Close() })
	defer close(terminal.unblock)
	s := openTest(t, b, true)
	nextEvent(t, s, pb.SessionEvent_READY)
	nextEvent(t, s, pb.SessionEvent_RESET)
	inputDone := make(chan error, 1)
	go func() {
		inputDone <- b.Input(s.ID, &pb.TerminalInput{Payload: &pb.TerminalInput_Data{Data: []byte("input")}})
	}()
	<-terminal.entered
	if _, err := w.Write([]byte("output while input waits")); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	output, err := s.Next(ctx)
	if err != nil {
		t.Fatalf("PTY input blocked independent output fanout: %v", err)
	}
	if string(output.GetData()) != "output while input waits" {
		t.Fatal("output lost")
	}
}

func TestOpeningCancellationAndShutdownReleaseLifecycleLock(t *testing.T) {
	for _, cancelMode := range []string{"caller", "bridge"} {
		t.Run(cancelMode, func(t *testing.T) {
			parent, cancelParent := context.WithCancel(t.Context())
			defer cancelParent()
			entered := make(chan struct{})
			b := NewBridge(parent, "sess_test", func(ctx context.Context, _ string, _, _ uint32) (Terminal, error) {
				close(entered)
				<-ctx.Done()
				return nil, ctx.Err()
			}, TerminalOptions{})
			t.Cleanup(func() { b.Close() })
			caller, cancelCaller := context.WithCancel(t.Context())
			defer cancelCaller()
			opened := make(chan error, 1)
			go func() {
				_, err := b.Open(caller, &pb.TerminalOpen{SessionId: "sess_test", Size: &pb.ResizeEvent{Rows: 24, Cols: 80}}, true)
				opened <- err
			}()
			<-entered
			closed := make(chan struct{})
			if cancelMode == "caller" {
				cancelCaller()
				close(closed)
			} else {
				go func() { b.Close(); close(closed) }()
			}
			select {
			case err := <-opened:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("unexpected opening outcome: %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("opening ignored cancellation")
			}
			select {
			case <-closed:
			case <-time.After(time.Second):
				t.Fatal("shutdown blocked behind pending attachment")
			}
			count, controller, _ := b.State()
			if count != 0 || controller != "" {
				t.Fatal("failed open retained attachment/control state")
			}
		})
	}
}

type fakeTerminal struct {
	r          *io.PipeReader
	w          *io.PipeWriter
	mu         sync.Mutex
	input      bytes.Buffer
	rows, cols uint32
}

func (f *fakeTerminal) Read(p []byte) (int, error) { return f.r.Read(p) }
func (f *fakeTerminal) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.input.Write(p)
}
func (f *fakeTerminal) Resize(rows, cols uint32) error { f.rows, f.cols = rows, cols; return nil }
func (f *fakeTerminal) Close() error                   { _ = f.w.Close(); return f.r.Close() }

func bridgeFixture(t *testing.T, queue int) (*Bridge, func() *fakeTerminal) {
	t.Helper()
	var terminal *fakeTerminal
	b := NewBridge(t.Context(), "sess_test", func(_ context.Context, _ string, rows, cols uint32) (Terminal, error) {
		r, w := io.Pipe()
		terminal = &fakeTerminal{r: r, w: w, rows: rows, cols: cols}
		return terminal, nil
	}, TerminalOptions{QueueSize: queue, MinRedrawInterval: time.Nanosecond})
	t.Cleanup(func() { _ = b.Close() })
	return b, func() *fakeTerminal { return terminal }
}

func openTest(t *testing.T, b *Bridge, writable bool) *Subscription {
	t.Helper()
	s, err := b.Open(t.Context(), &pb.TerminalOpen{SessionId: "sess_test", Size: &pb.ResizeEvent{Rows: 30, Cols: 100}}, writable)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func nextEvent(t *testing.T, s *Subscription, kind pb.SessionEvent_Type) *pb.TerminalOutput {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	for {
		out, err := s.Next(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if event := out.GetEvent(); event != nil && event.Type == kind {
			return out
		}
	}
}

func TestControllerObserverAndConcurrentTakeover(t *testing.T) {
	b, current := bridgeFixture(t, 64)
	readonly := openTest(t, b, false)
	ready := nextEvent(t, readonly, pb.SessionEvent_READY)
	if ready.GetEvent().Data["mode"] != "observer" {
		t.Fatal("read-only first attachment became controller")
	}
	first := openTest(t, b, true)
	ready = nextEvent(t, first, pb.SessionEvent_READY)
	if ready.GetEvent().Data["mode"] != "controller" {
		t.Fatal("first writer did not acquire control")
	}
	second := openTest(t, b, true)
	third := openTest(t, b, true)
	input := &pb.TerminalInput{Payload: &pb.TerminalInput_Data{Data: []byte("input")}}
	for _, observer := range []*Subscription{readonly, second, third} {
		if err := b.Input(observer.ID, input); connect.CodeOf(err) != connect.CodePermissionDenied {
			t.Fatalf("observer write: %v", err)
		}
		if err := b.Input(observer.ID, &pb.TerminalInput{Payload: &pb.TerminalInput_Resize{Resize: &pb.ResizeEvent{Rows: 1, Cols: 1}}}); connect.CodeOf(err) != connect.CodePermissionDenied {
			t.Fatalf("observer resize: %v", err)
		}
	}
	_, original, revision := b.State()
	if err := b.TakeControl("unknown", revision, "invalid-target", true); connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatal(err)
	}
	_, after, _ := b.State()
	if original != after {
		t.Fatal("unknown takeover demoted controller")
	}
	if err := b.TakeControl(readonly.ID, revision, "readonly", true); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	for i, s := range []*Subscription{second, third} {
		go func() { results <- b.TakeControl(s.ID, revision, fmt.Sprintf("race-%d", i), true) }()
	}
	one, two := <-results, <-results
	if !((one == nil && connect.CodeOf(two) == connect.CodeAborted) || (two == nil && connect.CodeOf(one) == connect.CodeAborted)) {
		t.Fatalf("takeover not atomic: %v / %v", one, two)
	}
	_, controller, currentRevision := b.State()
	if currentRevision == revision {
		t.Fatal("control revision did not change")
	}
	if err := b.Input(controller, input); err != nil {
		t.Fatal(err)
	}
	if current().input.String() != "input" {
		t.Fatal("input not delivered exactly once")
	}
	if err := b.Input(first.ID, input); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatal("old controller could write")
	}
	b.Detach(controller)
	_, controller, _ = b.State()
	if controller != "" {
		t.Fatal("observer was promoted implicitly")
	}
}

func TestRedrawOrderBytesAndSlowConsumer(t *testing.T) {
	b, current := bridgeFixture(t, 8)
	s := openTest(t, b, true)
	ready := nextEvent(t, s, pb.SessionEvent_READY)
	reset := nextEvent(t, s, pb.SessionEvent_RESET)
	if ready.Sequence != 1 || reset.Sequence != 2 {
		t.Fatal("incorrect READY/RESET sequence")
	}
	payload := []byte{0xff, 0x1b, '[', '3', '1', 'm', 0xe4, 0xb8}
	if _, err := current().w.Write(payload); err != nil {
		t.Fatal(err)
	}
	out, err := s.Next(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out.GetData(), payload) {
		t.Fatal("terminal bytes changed")
	}
	for i := 0; i < 9; i++ {
		if _, err := current().w.Write([]byte{byte(i)}); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	// The last pipe write can return before the reader enqueues its data.
	select {
	case <-s.ctx.Done():
	case <-ctx.Done():
		t.Fatal("slow reader not canceled")
	}
	if _, err := s.Next(ctx); connect.CodeOf(err) != connect.CodeResourceExhausted {
		t.Fatalf("slow consumer must fail, not silently skip bytes: %v", err)
	}
}

func TestTakeControlIdempotencyAndCancellation(t *testing.T) {
	b, _ := bridgeFixture(t, 32)
	first := openTest(t, b, true)
	second := openTest(t, b, true)
	_, _, revision := b.State()
	if err := b.TakeControl(second.ID, revision, "take", true); err != nil {
		t.Fatal(err)
	}
	if err := b.TakeControl(second.ID, revision, "take", true); err != nil {
		t.Fatal("same request must be idempotent", err)
	}
	if err := b.TakeControl(second.ID, revision, "take", false); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatal("saved takeover result bypassed current permission", err)
	}
	if err := b.TakeControl(second.ID, "different", "take", true); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatal(err)
	}
	first.Close()
	second.Close()
	count, controller, _ := b.State()
	if count != 0 || controller != "" {
		t.Fatal("detach leaked attachment")
	}
	ctx, cancel := context.WithCancel(t.Context())
	s, err := b.Open(ctx, &pb.TerminalOpen{SessionId: "sess_test", Size: &pb.ResizeEvent{Rows: 24, Cols: 80}}, true)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case <-s.ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("parent cancellation not propagated")
	}
}
