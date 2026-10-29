package session

import (
	"bytes"
	"context"
	"crypto/rand"
	"testing"
	"time"

	"github.com/DDSRem-Dev/Shikigami/internal/server/tmux"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
)

func TestRealTmuxMultiObserverRedraw(t *testing.T) {
	client, err := tmux.New("")
	if err != nil {
		t.Skipf("tmux dependency unavailable: %v", err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	id := "sess_" + rand.Text()
	pane, err := client.Create(ctx, id, t.TempDir(), []string{"/bin/sh", "-c", `printf '\033[?1049h\033[32mSCREEN_世界\033[0m'; read value; printf '\nRECEIVED:%s\n' "$value"; sleep 30`})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if err := client.Delete(cleanup, id); err != nil {
			t.Error(err)
		}
	})
	b := NewBridge(ctx, id, func(ctx context.Context, id string, rows, cols uint32) (Terminal, error) {
		return client.Attach(ctx, id, rows, cols)
	}, TerminalOptions{MinRedrawInterval: time.Nanosecond})
	defer b.Close()
	open := &pb.TerminalOpen{SessionId: id, Size: &pb.ResizeEvent{Rows: 30, Cols: 100}}
	controller, err := b.Open(ctx, open, true)
	if err != nil {
		t.Fatal(err)
	}
	nextEvent(t, controller, pb.SessionEvent_READY)
	nextEvent(t, controller, pb.SessionEvent_RESET)
	readScreen(t, ctx, controller, "SCREEN_世界")
	observer, err := b.Open(ctx, open, false)
	if err != nil {
		t.Fatal(err)
	}
	ready := nextEvent(t, observer, pb.SessionEvent_READY)
	if ready.GetEvent().Data["mode"] != "observer" {
		t.Fatal("observer acquired controller")
	}
	nextEvent(t, observer, pb.SessionEvent_RESET)
	observerScreen := readScreen(t, ctx, observer, "SCREEN_世界")
	nextEvent(t, controller, pb.SessionEvent_RESET)
	controllerScreen := readScreen(t, ctx, controller, "SCREEN_世界")
	if !bytes.Contains(observerScreen, []byte("\x1b[")) || !bytes.Contains(controllerScreen, []byte("\x1b[")) {
		t.Fatal("screen initialization lost ANSI state")
	}
	if err := b.Input(controller.ID, &pb.TerminalInput{Payload: &pb.TerminalInput_Data{Data: []byte("once\n")}}); err != nil {
		t.Fatal(err)
	}
	readScreen(t, ctx, controller, "RECEIVED:once")
	readScreen(t, ctx, observer, "RECEIVED:once")
	controller.Close()
	observer.Close()
	current, err := client.Inspect(ctx, id)
	if err != nil || current.PID != pane.PID || current.Dead {
		t.Fatalf("redraw or detach restarted pane: %+v %v", current, err)
	}
}

func readScreen(t *testing.T, ctx context.Context, s *Subscription, marker string) []byte {
	t.Helper()
	var data []byte
	for !bytes.Contains(data, []byte(marker)) {
		out, err := s.Next(ctx)
		if err != nil {
			t.Fatalf("missing %s: %v (bytes %q)", marker, err, data)
		}
		data = append(data, out.GetData()...)
	}
	return data
}
