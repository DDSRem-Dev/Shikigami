package tmux

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestCaptureVisibleScreenExcludesHistoryAndDoesNotControl(t *testing.T) {
	c := testClient(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	pane, err := c.Create(ctx, "sess_screen", t.TempDir(), []string{"/bin/sh", "-c", `printf 'OLD_HISTORY_ONLY\n'; i=0; while [ "$i" -lt 70 ]; do printf 'visible line %s\n' "$i"; i=$((i+1)); done; printf '\033[31mVISIBLE_NOW_世界\033[0m'; sleep 30`})
	if err != nil {
		t.Fatal(err)
	}
	var screen Screen
	for {
		screen, err = c.CaptureScreen(ctx, "sess_screen", pane.PaneID, pane.WindowID)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(screen.Text, "VISIBLE_NOW_世界") {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("current screen did not converge")
		case <-time.After(10 * time.Millisecond):
		}
	}
	if strings.Contains(screen.Text, "OLD_HISTORY_ONLY") || strings.Contains(screen.Text, "\x1b") || screen.Cols != 80 || screen.Rows != 24 {
		t.Fatalf("capture replayed history or ANSI/changed dimensions: %+v", screen)
	}
	after, err := c.Inspect(ctx, "sess_screen")
	if err != nil || after.PID != pane.PID || after.PaneID != pane.PaneID {
		t.Fatal("capture changed process identity", err)
	}
	if _, err := c.CaptureScreen(ctx, "sess_screen", "%99999999", pane.WindowID); err == nil {
		t.Fatal("unregistered pane accepted")
	}
}
func TestCaptureBufferRemainsBounded(t *testing.T) {
	buffer := &boundedCapture{limit: MaxScreenBytes}
	data := []byte(strings.Repeat("x", MaxScreenBytes*2))
	n, err := buffer.Write(data)
	if err != nil || n != len(data) || buffer.Len() != MaxScreenBytes || !buffer.truncated {
		t.Fatal("capture buffer was not bounded")
	}
}
