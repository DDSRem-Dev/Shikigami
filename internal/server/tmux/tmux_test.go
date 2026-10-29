package tmux

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The child represents a daemon with a live PTY. Killing it must leave the
// independent tmux pane running while the ordinary attach child exits.
func TestCrashHelper(t *testing.T) {
	if os.Getenv("SHIKI_TMUX_CRASH_HELPER") != "1" {
		return
	}
	c, err := New("")
	if err != nil {
		t.Fatal(err)
	}
	c.socket = os.Getenv("SHIKI_TEST_SOCKET")
	a, err := c.Attach(context.Background(), "sess_crash", 24, 80)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("READY %d\n", a.cmd.Process.Pid)
	io.Copy(io.Discard, a)
	_ = a.Close()
}

func TestParentCrashKeepsPaneAndAttachExits(t *testing.T) {
	c := testClient(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	pane, err := c.Create(ctx, "sess_crash", t.TempDir(), []string{"/bin/sleep", "60"})
	if err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	child := exec.CommandContext(ctx, executable, "-test.run=^TestCrashHelper$")
	child.Env = append(os.Environ(), "SHIKI_TMUX_CRASH_HELPER=1", "SHIKI_TEST_SOCKET="+c.socket)
	pipe, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	child.Stderr = os.Stderr
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = child.Process.Kill(); _ = child.Wait() })
	scanner := bufio.NewScanner(pipe)
	if !scanner.Scan() || !strings.HasPrefix(scanner.Text(), "READY ") {
		t.Fatalf("child failed to attach: %s %v", scanner.Text(), scanner.Err())
	}
	pid, err := strconv.Atoi(strings.TrimPrefix(scanner.Text(), "READY "))
	if err != nil {
		t.Fatal(err)
	}
	if err := child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = child.Wait()
	after, err := c.Inspect(ctx, "sess_crash")
	if err != nil || after.Dead || after.PID != pane.PID {
		t.Fatalf("parent crash killed pane: %+v %v", after, err)
	}
	for !processExited(pid) {
		select {
		case <-ctx.Done():
			t.Fatal("orphan attach process survived daemon crash")
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// After the daemon is killed, its attach child is adopted by init. Minimal
// container PID 1 implementations may retain an already-exited zombie; that
// is not a running orphan. Real init/systemd reaps it. Never accept a live
// sleeping or blocked attach process as having exited.
func processExited(pid int) bool {
	if errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
		return true
	}
	if runtime.GOOS == "linux" {
		data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
		if errors.Is(err, os.ErrNotExist) {
			return true
		}
		if err == nil {
			if end := strings.LastIndex(string(data), ") "); end >= 0 {
				fields := strings.Fields(string(data)[end+2:])
				return len(fields) > 0 && fields[0] == "Z"
			}
		}
	}
	return false
}

func testClient(t *testing.T) *Client {
	t.Helper()
	c, err := New("")
	if err != nil {
		t.Skipf("tmux not installed: %v", err)
	}
	c.socket = fmt.Sprintf("shiki-test-%d-%d", os.Getpid(), time.Now().UnixNano())
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = c.run(ctx, "kill-server")
	})
	return c
}

func TestAttachSurvivesDetachAndRedraws(t *testing.T) {
	c := testClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	dir := t.TempDir()
	marker := filepath.Join(dir, "keep.txt")
	if err := os.WriteFile(marker, []byte("project contents"), 0600); err != nil {
		t.Fatal(err)
	}
	pane, err := c.Create(ctx, "sess_lifecycle", dir, []string{"/bin/sh", "-c", `printf '\033[?1049h\033[31mSHIKI_REDRAW_世界\033[0m'; read value; printf '\nINPUT:%s\n' "$value"; sleep 30`})
	if err != nil {
		t.Fatal(err)
	}
	first, err := c.Attach(ctx, pane.SessionID, 24, 80)
	if err != nil {
		t.Fatal(err)
	}
	readUntil(t, first, []byte("SHIKI_REDRAW_"))
	if err := first.Resize(40, 120); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	still, err := c.Inspect(ctx, pane.SessionID)
	if err != nil || still.Dead || still.PID != pane.PID {
		t.Fatalf("pane did not survive detach: %+v %v", still, err)
	}
	second, err := c.Attach(ctx, pane.SessionID, 40, 120)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	redraw := readUntil(t, second, []byte("SHIKI_REDRAW_"))
	if !bytes.Contains(redraw, []byte("\x1b[")) {
		t.Fatal("expected terminal redraw escape sequences")
	}
	if _, err := second.Write([]byte("hello\n")); err != nil {
		t.Fatal(err)
	}
	readUntil(t, second, []byte("INPUT:hello"))
	if err := c.Delete(ctx, pane.SessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Inspect(ctx, pane.SessionID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("inspect deleted: %v", err)
	}
	contents, err := os.ReadFile(marker)
	if err != nil || string(contents) != "project contents" {
		t.Fatal("delete changed project files")
	}
}

func readUntil(t *testing.T, a *Attachment, target []byte) []byte {
	t.Helper()
	type result struct {
		data []byte
		err  error
	}
	resultCh := make(chan result, 1)
	go func() {
		var data []byte
		buf := make([]byte, 4096)
		for {
			n, err := a.Read(buf)
			data = append(data, buf[:n]...)
			if bytes.Contains(data, target) || err != nil {
				resultCh <- result{data, err}
				return
			}
		}
	}()
	select {
	case got := <-resultCh:
		if !bytes.Contains(got.data, target) {
			t.Fatalf("missing %q in %q (%v)", target, got.data, got.err)
		}
		return got.data
	case <-time.After(5 * time.Second):
		_ = a.Close()
		got := <-resultCh
		t.Fatalf("timed out waiting for %q; got %q (%v)", target, got.data, got.err)
		return nil
	}
}

func TestExitStatusAndLiteralArguments(t *testing.T) {
	c := testClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	dir := t.TempDir()
	output := filepath.Join(dir, "literal")
	argument := "$(touch SHOULD_NOT_EXIST); ' spaces 世界"
	_, err := c.Create(ctx, "sess_exit", dir, []string{"/bin/sh", "-c", `printf '%s' "$1" > "$2"; exit 17`, "shiki-test", argument, output})
	if err != nil {
		t.Fatal(err)
	}
	for {
		p, err := c.Inspect(ctx, "sess_exit")
		if err != nil {
			t.Fatal(err)
		}
		// tmux can mark the pane dead before its SIGCHLD callback records
		// waitpid's exit status. Require the real final status, not just EOF.
		if p.Dead && p.ExitStatus != nil {
			if *p.ExitStatus != 17 {
				t.Fatalf("wrong exit: %+v", p)
			}
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
	got, err := os.ReadFile(output)
	if err != nil || string(got) != argument {
		t.Fatalf("arguments not literal: %q %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "SHOULD_NOT_EXIST")); !os.IsNotExist(err) {
		t.Fatal("shell injection")
	}
}

func TestRejectUnsafeTargets(t *testing.T) {
	for _, id := range []string{"", "sess_", "other", "sess_a:1", "sess_a;kill-server", "sess_a\n"} {
		if _, err := Name(id); err == nil {
			t.Fatalf("accepted %q", id)
		}
	}
	if _, err := Name("sess_" + strings.Repeat("x", 97)); err == nil {
		t.Fatal("accepted oversized id")
	}
}

func TestEmptyRunningServerHasEmptyInventory(t *testing.T) {
	c := testClient(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if _, err := c.Create(ctx, "sess_empty", t.TempDir(), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := c.run(ctx, "set-option", "-s", "exit-empty", "off"); err != nil {
		t.Fatal(err)
	}
	if err := c.Delete(ctx, "sess_empty"); err != nil {
		t.Fatal(err)
	}
	panes, err := c.List(ctx)
	if err != nil || len(panes) != 0 {
		t.Fatalf("empty live server inventory: %+v %v", panes, err)
	}
}

func TestResizeKeepsReadDeadlineAndCancellation(t *testing.T) {
	c := testClient(t)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	if _, err := c.Create(ctx, "sess_resize_cancel", t.TempDir(), []string{"/bin/sleep", "30"}); err != nil {
		t.Fatal(err)
	}
	a, err := c.Attach(ctx, "sess_resize_cancel", 24, 80)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if err = a.Resize(40, 120); err != nil {
		t.Fatal(err)
	}
	if err = a.file.SetReadDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	_, err = io.Copy(io.Discard, a)
	if !os.IsTimeout(err) {
		t.Fatalf("resize disabled poller deadline; read ended with %v", err)
	}
	if err = a.Close(); err != nil {
		t.Fatal(err)
	}
	if pane, err := c.Inspect(ctx, "sess_resize_cancel"); err != nil || pane.Dead {
		t.Fatalf("attach cancellation killed pane: %+v %v", pane, err)
	}
}

func TestExactTargetsDoNotMatchSessionNamePrefixes(t *testing.T) {
	c := testClient(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if _, err := c.Create(ctx, "sess_prefix_long", t.TempDir(), []string{"/bin/sleep", "30"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Inspect(ctx, "sess_prefix"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("short name matched existing session: %v", err)
	}
	if _, err := c.Attach(ctx, "sess_prefix", 24, 80); !errors.Is(err, ErrNotFound) {
		t.Fatalf("short attach matched existing session: %v", err)
	}
	if err := c.Delete(ctx, "sess_prefix"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Inspect(ctx, "sess_prefix_long"); err != nil {
		t.Fatalf("short delete killed prefix match: %v", err)
	}
	if _, err := c.Create(ctx, "sess_prefix", t.TempDir(), []string{"/bin/sleep", "30"}); err != nil {
		t.Fatal(err)
	}
	if err := c.Delete(ctx, "sess_prefix"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Inspect(ctx, "sess_prefix_long"); err != nil {
		t.Fatal("exact delete affected longer name", err)
	}
}
