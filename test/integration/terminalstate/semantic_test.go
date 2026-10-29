package terminalstate_test

import (
	"crypto/rand"
	"fmt"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestFrontendSemanticRedraw(t *testing.T) {
	if os.Getenv("SHIKI_TERMINAL_ANALYZER") == "" {
		t.Skip("independent VT analyzer required; use scripts/test-terminal-state.sh")
	}
	f := fixture(t)
	controller := f.attach(t, "controller", 24, 80, false)
	controller.expect(t, "ready", nil, marker("APP_READY"))
	controller.send(t, "M\n")
	mainPoints := [][2]int{{9, 5}, {10, 5}, {11, 5}}
	mainCheck := func(s screenState) error {
		if err := marker("MAIN_SCREEN")(s); err != nil {
			return err
		}
		if s.X != 12 || s.Y != 8 || s.Hidden {
			return fmt.Errorf("main cursor mismatch")
		}
		return colored(s.Cells, "红", "Z", "8080/0000/0000/ffff", "0000/0000/8080/ffff")
	}
	controller.expect(t, "main", mainPoints, mainCheck)
	controller.send(t, "A\n")
	altPoints := [][2]int{{5, 3}, {6, 3}, {7, 3}}
	altCheck := func(s screenState) error {
		if err := marker("ALT_SCREEN")(s); err != nil {
			return err
		}
		if strings.Contains(strings.Join(s.Lines, "\n"), "MAIN_SCREEN") || strings.Contains(strings.Join(s.Lines, "\n"), "红") {
			return fmt.Errorf("main screen leaked into alternate")
		}
		if !s.Alt || s.X != 11 || s.Y != 7 || !s.Hidden {
			return fmt.Errorf("alternate cursor/mode mismatch")
		}
		return colored(s.Cells, "界", "Q", "0000/8080/0000/ffff", "8080/0000/8080/ffff")
	}
	controller.expect(t, "alternate", altPoints, altCheck)
	controllerReset := controller.resetCount()
	observer := f.attach(t, "observer", 24, 80, true)
	controller.waitReset(t, controllerReset)
	observer.expect(t, "alternate-join", altPoints, altCheck)
	controller.expect(t, "alternate-redraw", altPoints, altCheck)
	// Existing observers and the controller must both render the new geometry.
	controller.resize(t, 32, 110)
	observer.resize(t, 32, 110)
	if err := syscall.Kill(os.Getpid(), syscall.SIGWINCH); err != nil {
		t.Fatal(err)
	}
	// Wait for the actual pane size, not just frontend ioctl success, before asking
	// the application to place its edge marker at the current bottom-right corner.
	waitPaneSize(t, f, 32, 110)
	controller.send(t, "R\n")
	resizedPoints := append(append([][2]int(nil), altPoints...), [2]int{106, 31}, [2]int{109, 31})
	resizedCheck := func(s screenState) error {
		if err := marker("ALT_RESIZED_32_110")(s); err != nil {
			return err
		}
		if s.Rows != 32 || s.Cols != 110 || !s.Alt || s.X != 11 || s.Y != 7 || !s.Hidden {
			return fmt.Errorf("resized alternate state mismatch")
		}
		if err := colored(s.Cells[:3], "界", "Q", "0000/8080/0000/ffff", "8080/0000/8080/ffff"); err != nil {
			return err
		}
		if s.Cells[3].Text != "E" || s.Cells[4].Text != "E" {
			return fmt.Errorf("bottom-right edge lost: %+v", s.Cells[3:])
		}
		return nil
	}
	controller.expect(t, "resized", resizedPoints, resizedCheck)
	observer.expect(t, "resized", resizedPoints, resizedCheck)
	observer.close(t)
	controllerReset = controller.resetCount()
	observer2 := f.attach(t, "observer-reconnected", 32, 110, true)
	controller.waitReset(t, controllerReset)
	observer2.expect(t, "resized-reconnect", resizedPoints, resizedCheck)
	controller.expect(t, "resized-observer-redraw", resizedPoints, resizedCheck)
	controller.close(t)
	observerReset := observer2.resetCount()
	newController := f.attach(t, "controller-reconnected", 32, 110, false)
	observer2.waitReset(t, observerReset)
	newController.expect(t, "resized-reconnect", resizedPoints, resizedCheck)
	observer2.expect(t, "controller-redraw", resizedPoints, resizedCheck)
	newController.send(t, "E\n")
	restoredCheck := func(s screenState) error {
		if err := marker("MAIN_SCREEN")(s); err != nil {
			return err
		}
		var x, y, alternate int
		actual := paneCursor(f)
		if n, err := fmt.Sscanf(actual, "%d,%d,alternate=%d", &x, &y, &alternate); err != nil || n != 3 {
			return fmt.Errorf("pane cursor unavailable: %q", actual)
		}
		if strings.Contains(strings.Join(s.Lines, "\n"), "ALT_") || strings.Contains(strings.Join(s.Lines, "\n"), "EDGE") || strings.Contains(strings.Join(s.Lines, "\n"), "界") {
			return fmt.Errorf("alternate screen leaked into restored main")
		}
		if alternate != 0 || s.X != x || s.Y != y || s.Hidden {
			return fmt.Errorf("main restoration differs from actual tmux cursor %q", actual)
		}
		return colored(s.Cells, "红", "Z", "8080/0000/0000/ffff", "0000/0000/8080/ffff")
	}
	newController.expect(t, "main-restored", mainPoints, restoredCheck)
	observer2.expect(t, "main-restored", mainPoints, restoredCheck)
	observer2.close(t)
	taker := f.attach(t, "taker", 20, 60, false)
	taker.expect(t, "observer-main", mainPoints, marker("MAIN_SCREEN"))
	state := <-taker.ready
	if state.Mode != "observer" {
		t.Fatal("new writer should be observer", state)
	}
	taker.resize(t, 37, 121) // no SIGWINCH: control confirmation must send live size
	if _, err := f.front.Remote(f.serverID).TakeControl(f.ctx, &pb.TakeControlRequest{SessionId: f.sessionID, AttachmentId: state.AttachmentID, ExpectedControlRevision: state.ControlRevision, RequestId: rand.Text()}); err != nil {
		t.Fatal(err)
	}
	waitPaneSize(t, f, 37, 121)
	taker.send(t, "A\n")
	taker.expect(t, "takeover-alternate", altPoints, altCheck)
	taker.send(t, "R\n")
	takeoverPoints := append(append([][2]int(nil), altPoints...), [2]int{117, 36}, [2]int{120, 36})
	taker.expect(t, "takeover-live-size", takeoverPoints, func(s screenState) error {
		if err := marker("ALT_RESIZED_37_121")(s); err != nil {
			return err
		}
		if s.Rows != 37 || s.Cols != 121 || !s.Alt || s.X != 11 || s.Y != 7 || !s.Hidden {
			return fmt.Errorf("takeover geometry/cursor mismatch")
		}
		if err := colored(s.Cells[:3], "界", "Q", "0000/8080/0000/ffff", "8080/0000/8080/ffff"); err != nil {
			return err
		}
		if s.Cells[3].Text != "E" || s.Cells[4].Text != "E" {
			return fmt.Errorf("takeover bottom-right edge lost")
		}
		return nil
	})
	taker.close(t)
	newController.close(t)
	observer2.close(t)
	pane, err := f.mux.Inspect(f.ctx, f.sessionID)
	if err != nil || pane.PID != f.pane.PID || pane.Dead {
		t.Fatalf("redraw/reconnect replaced pane: %+v %v", pane, err)
	}
	t.Logf("original pane PID %d survived all attach/resize/reconnect and alternate-screen transitions", pane.PID)
}
func colored(c []screenCell, wide, tail, fg, bg string) error {
	if len(c) != 3 || c[0].Text != wide || c[0].Width != 2 || c[1].Width != 0 || c[2].Text != tail || c[2].Width != 1 {
		return fmt.Errorf("UTF-8/wide cells mismatch: %+v", c)
	}
	for _, i := range []int{0, 2} {
		if c[i].FG != fg || c[i].BG != bg {
			return fmt.Errorf("cell colors mismatch: %+v", c[i])
		}
	}
	return nil
}
func waitPaneSize(t *testing.T, f *terminalFixture, rows, cols int) {
	t.Helper()
	want := fmt.Sprintf("%d %d", rows, cols)
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		// Read-only tmux metadata is an independent sizing observation; screen
		// acceptance itself uses only the actual frontend PTY byte stream.
		out, err := tmuxSize(f)
		if err == nil && strings.TrimSpace(out) == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("pane did not receive controller resize", want)
}
