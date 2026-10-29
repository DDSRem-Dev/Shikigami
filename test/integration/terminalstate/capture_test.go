package terminalstate_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/DDSRem-Dev/Shikigami/internal/client"
	"github.com/creack/pty"
	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

type captureFrame struct {
	Data       []byte
	Rows, Cols int
}
type screenCell struct {
	Text   string
	Width  int
	FG, BG string
	Attrs  uint8
}
type screenState struct {
	Rows, Cols, X, Y int
	Alt, Hidden      bool
	Lines            []string
	Cells            []screenCell
}
type screenRequest struct {
	Rows, Cols int
	Frames     []captureFrame
	Points     [][2]int
}
type terminalCapture struct {
	track         func(*testing.T)
	ready         chan client.TerminalState
	mu            sync.Mutex
	frames        []captureFrame
	readErr       error
	master, slave *os.File
	cancel        context.CancelFunc
	done          chan error
	readDone      chan struct{}
	rows, cols    int
	saved         *term.State
	stopped       bool
	name          string
}

func (f *terminalFixture) attach(t *testing.T, name string, rows, cols int, readonly bool) *terminalCapture {
	t.Helper()
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	if err = pty.Setsize(slave, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)}); err != nil {
		t.Fatal(err)
	}
	// A nonblocking duplicate lets Close wake the PTY reader on both platforms.
	fd, err := unix.FcntlInt(master.Fd(), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err = unix.SetNonblock(fd, true); err != nil {
		t.Fatal(err)
	}
	master.Close()
	master = os.NewFile(uintptr(fd), "screen-capture")
	saved, err := term.GetState(int(slave.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(f.ctx)
	c := &terminalCapture{master: master, slave: slave, cancel: cancel, done: make(chan error, 1), readDone: make(chan struct{}), rows: rows, cols: cols, saved: saved, name: name, ready: make(chan client.TerminalState, 1), track: f.track}
	go func() {
		defer close(c.readDone)
		b := make([]byte, 4096)
		for {
			n, err := master.Read(b)
			c.mu.Lock()
			if n > 0 {
				c.frames = append(c.frames, captureFrame{Data: bytes.Clone(b[:n])})
			}
			c.readErr = err
			c.mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
	go func() {
		c.done <- f.front.Terminal(ctx, f.serverID, f.sessionID, client.TerminalOptions{Input: slave, Output: slave, ReadOnly: readonly, DetachKey: 17, BeforeRaw: func(state client.TerminalState) { c.ready <- state }})
	}()
	t.Cleanup(func() { c.close(t) })
	return c
}
func (c *terminalCapture) close(t *testing.T) {
	t.Helper()
	if c.stopped {
		return
	}
	c.stopped = true
	c.cancel()
	select {
	case err := <-c.done:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Error("terminal exit", c.name, err)
		}
	case <-time.After(5 * time.Second):
		t.Error("terminal stuck", c.name)
	}
	got, err := term.GetState(int(c.slave.Fd()))
	if err != nil || !reflect.DeepEqual(got, c.saved) {
		t.Error("termios not restored", c.name, err)
	}
	c.slave.Close()
	c.master.Close()
	select {
	case <-c.readDone:
	case <-time.After(time.Second):
		t.Error("capture reader stuck")
	}
}
func (c *terminalCapture) send(t *testing.T, s string) {
	t.Helper()
	if _, err := c.master.Write([]byte(s)); err != nil {
		t.Fatal(err)
	}
}
func (c *terminalCapture) resize(t *testing.T, rows, cols int) {
	t.Helper()
	c.mu.Lock()
	c.frames = append(c.frames, captureFrame{Rows: rows, Cols: cols})
	c.mu.Unlock()
	if err := pty.Setsize(c.slave, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)}); err != nil {
		t.Fatal(err)
	}
}
func (c *terminalCapture) request(points [][2]int) screenRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	return screenRequest{Rows: c.rows, Cols: c.cols, Frames: append([]captureFrame(nil), c.frames...), Points: points}
}
func decode(t *testing.T, req screenRequest) screenState {
	t.Helper()
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Getenv("SHIKI_TERMINAL_ANALYZER"))
	cmd.Stdin = bytes.NewReader(data)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("independent analyzer: %v: %s", err, out)
	}
	var state screenState
	if err = json.Unmarshal(out, &state); err != nil {
		t.Fatal(err)
	}
	return state
}
func (c *terminalCapture) expect(t *testing.T, label string, points [][2]int, check func(screenState) error) screenState {
	t.Helper()
	var state screenState
	var req screenRequest
	var last error
	for deadline := time.Now().Add(6 * time.Second); time.Now().Before(deadline); {
		req = c.request(points)
		state = decode(t, req)
		last = check(state)
		if last == nil {
			c.save(t, label, req, state)
			return state
		}
		time.Sleep(30 * time.Millisecond)
	}
	c.save(t, label+"-failed", req, state)
	t.Fatalf("%s/%s semantic state: %v; cursor=%d,%d alt=%v hidden=%v cells=%+v lines=%q", c.name, label, last, state.X, state.Y, state.Alt, state.Hidden, state.Cells, state.Lines)
	return state
}
func (c *terminalCapture) save(t *testing.T, label string, req screenRequest, state screenState) {
	t.Helper()
	c.track(t)
	if root := os.Getenv("SHIKI_RUN_EVIDENCE_DIR"); root != "" {
		name := filepath.Join(root, c.name+"-"+label+".json")
		data, err := json.MarshalIndent(map[string]any{"request": req, "decoded": state}, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(name, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("semantic checkpoint %s/%s: size=%dx%d cursor=(%d,%d) alt=%v hidden=%v", c.name, label, state.Cols, state.Rows, state.X, state.Y, state.Alt, state.Hidden)
}
func marker(text string) func(screenState) error {
	return func(s screenState) error {
		for _, line := range s.Lines {
			if bytes.Contains([]byte(line), []byte(text)) {
				return nil
			}
		}
		return fmt.Errorf("missing rendered marker %s", text)
	}
}

func (c *terminalCapture) resetCount() int {
	req := c.request(nil)
	var data []byte
	for _, f := range req.Frames {
		data = append(data, f.Data...)
	}
	return bytes.Count(data, []byte("\x1bc"))
}
func (c *terminalCapture) waitReset(t *testing.T, previous int) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		if c.resetCount() > previous {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("existing frontend never consumed a new RESET", c.name, previous)
}
