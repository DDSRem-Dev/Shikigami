package tmux

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTmuxCommandRejectsOversizedProcessOutput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tmux-output-fixture")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nhead -c 1048577 /dev/zero\n"), 0700); err != nil {
		t.Fatal(err)
	}
	c, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	data, err := c.run(ctx, "list-panes")
	if err == nil || !strings.Contains(err.Error(), "output exceeds") || len(data) != 0 {
		t.Fatalf("oversized tmux output parsed: %d bytes, %v", len(data), err)
	}
}

func TestRetainedLogsAreBoundedAndSeparateFromScreen(t *testing.T) {
	c := testClient(t)
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	pane, err := c.Create(ctx, "sess_logs", t.TempDir(), []string{"/bin/sh", "-c", `printf 'OLD_RETAINED_DIAGNOSTIC\n'; i=0; while [ "$i" -lt 70 ]; do printf 'line %s\n' "$i"; i=$((i+1)); done; printf '\033[31mCRASH_世界\033[0m\n'; exit 7`})
	if err != nil {
		t.Fatal(err)
	}
	for !pane.Dead {
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
		pane, err = c.Inspect(ctx, "sess_logs")
		if err != nil {
			t.Fatal(err)
		}
	}
	logs, err := c.CaptureLogs(ctx, "sess_logs", pane.PaneID, pane.WindowID, 200)
	if err != nil || logs.State != "available" || !strings.Contains(logs.Text, "OLD_RETAINED_DIAGNOSTIC") || !strings.Contains(logs.Text, "CRASH_世界") || strings.Contains(logs.Text, "\x1b") {
		t.Fatalf("retained logs: %+v %v", logs, err)
	}
	if logs.ExitCode == nil {
		if pane.ExitStatus != nil && pane.ExitSignal == 0 && !pane.ExitSignalUnknown {
			t.Fatal("capture omitted an already verified exit code")
		}
		t.Log("retained logs outcome counts: known=0 unknown=1 (tmux did not retain a cause)")
	} else if *logs.ExitCode != 7 {
		t.Fatalf("invented exit code: %d", *logs.ExitCode)
	} else {
		t.Log("retained logs outcome counts: known=1 unknown=0")
	}
	screen, err := c.CaptureScreen(ctx, "sess_logs", pane.PaneID, pane.WindowID)
	if err != nil || strings.Contains(screen.Text, "OLD_RETAINED_DIAGNOSTIC") {
		t.Fatalf("screen unexpectedly includes scrollback: %+v %v", screen, err)
	}
	tail, err := c.CaptureLogs(ctx, "sess_logs", pane.PaneID, pane.WindowID, 3)
	if err != nil || !tail.Truncated || strings.Count(tail.Text, "\n") > 2 || strings.Contains(tail.Text, "OLD_RETAINED_DIAGNOSTIC") {
		t.Fatalf("bounded line tail: %+v %v", tail, err)
	}
	after, err := c.Inspect(ctx, "sess_logs")
	if err != nil || after.PID != pane.PID || after.PaneID != pane.PaneID || !after.Dead {
		t.Fatal("capture changed process identity", err)
	}
	bad, err := c.CaptureLogs(ctx, "sess_logs", "%99999999", pane.WindowID, 200)
	if err != nil || bad.State != "unavailable" || bad.Text != "" {
		t.Fatalf("unregistered pane logs: %+v %v", bad, err)
	}
	if err := c.Delete(ctx, "sess_logs"); err != nil {
		t.Fatal(err)
	}
	missing, err := c.CaptureLogs(ctx, "sess_logs", pane.PaneID, pane.WindowID, 200)
	if err != nil || missing.State != "not_retained" || missing.Text != "" {
		t.Fatalf("missing pane: %+v %v", missing, err)
	}
}

func TestRetainedLogByteTailKeepsNewestOutput(t *testing.T) {
	w := &tailCapture{limit: MaxLogBytes}
	for _, text := range []string{strings.Repeat("a", MaxLogBytes*2), strings.Repeat("b", 500), "LATEST_CRASH"} {
		if n, err := w.Write([]byte(text)); err != nil || n != len(text) {
			t.Fatal(n, err)
		}
	}
	if len(w.data) != MaxLogBytes || !w.truncated || !strings.HasSuffix(string(w.data), "LATEST_CRASH") {
		t.Fatal("newest bounded output was not retained")
	}
}

func TestPaneExitSignalFormats(t *testing.T) {
	for _, test := range []struct {
		value string
		want  int
	}{{"term", 15}, {"SIGTERM", 15}, {"15", 15}, {"", 0}, {"unknown", 0}} {
		pane, err := parsePane("shiki-sess_signal|@1|%1|123|1||$1|" + test.value)
		if err != nil || pane.ExitSignal != test.want || pane.ExitStatus != nil || pane.ExitSignalUnknown != (test.value == "unknown") {
			t.Fatalf("signal %q: %+v, %v", test.value, pane, err)
		}
	}
}

func TestSignalRespawnRefusesPaneThatBecameAlive(t *testing.T) {
	c := testClient(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	pane, err := c.Create(ctx, "sess_signal_race", t.TempDir(), []string{"/bin/sh", "-c", "kill -TERM $$"})
	if err != nil {
		t.Fatal(err)
	}
	for !pane.Dead {
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
		pane, err = c.Inspect(ctx, "sess_signal_race")
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := c.Respawn(ctx, "sess_signal_race", []string{"/bin/sleep", "30"}); err != nil {
		t.Fatal(err)
	}
	live, err := c.Inspect(ctx, "sess_signal_race")
	if err != nil {
		t.Fatal(err)
	}
	if err := c.RespawnDead(ctx, "sess_signal_race", pane.PaneID, []string{"/bin/sleep", "30"}); err == nil {
		t.Fatal("automatic respawn killed a concurrently restarted pane")
	}
	after, err := c.Inspect(ctx, "sess_signal_race")
	if err != nil || after.Dead || after.PID != live.PID {
		t.Fatal("automatic respawn changed the live process", err)
	}
}
