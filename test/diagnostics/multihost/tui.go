package main

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/creack/pty"
)

func (h *helper) tui(ctx context.Context, r request) (result any, finalErr error) {
	a, err := args[struct {
		ServerID    string   `json:"server_id"`
		ProjectID   string   `json:"project_id"`
		SessionID   string   `json:"session_id"`
		ReadOnly    bool     `json:"read_only"`
		TakeControl bool     `json:"take_control"`
		Tabs        int      `json:"tabs"`
		Contains    []string `json:"contains"`
	}](r)
	if err != nil || a.Tabs < 0 || a.Tabs > 8 || len(a.Contains) > 16 {
		return nil, invalid()
	}
	argv := []string{"--config", h.config}
	if a.ServerID != "" {
		argv = append(argv, "--server", a.ServerID)
	}
	if a.ProjectID != "" {
		argv = append(argv, "--project", a.ProjectID)
	}
	expectedMode, expectedPrompt := "", ""
	if r.Op == "terminal.ui" {
		if a.SessionID == "" || a.ReadOnly == a.TakeControl {
			return nil, invalid()
		}
		argv = append(argv, "attach", a.SessionID, "--reconnect=false", "--detach-key", "ctrl-q")
		expectedMode = "controller"
		if a.ReadOnly {
			argv = append(argv, "--read-only")
			expectedMode = "read-only observer"
		} else {
			argv = append(argv, "--take-control")
		}
		expectedPrompt = "(" + expectedMode + "). Detach:"
		a.Contains = []string{expectedPrompt}
	} else {
		argv = append(argv, "dashboard")
	}
	limit := 20 * time.Second
	if r.Op == "tui.capture" {
		limit = 30 * time.Second
	}
	call, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	cmd := exec.CommandContext(call, h.cli, argv...)
	cmd.Dir = h.root
	cmd.Env = append(os.Environ(), "TERM=xterm-256color", "NO_COLOR=1")
	terminal, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 36, Cols: 240})
	if err != nil {
		return nil, err
	}
	defer terminal.Close()
	var mu sync.Mutex
	var output cappedBuffer
	readDone := make(chan struct{})
	outputChanged := make(chan struct{}, 1)
	go func() {
		defer close(readDone)
		buffer := make([]byte, 4096)
		for {
			n, err := terminal.Read(buffer)
			mu.Lock()
			_, writeErr := output.Write(buffer[:n])
			mu.Unlock()
			select {
			case outputChanged <- struct{}{}:
			default:
			}
			if err != nil || writeErr != nil {
				return
			}
		}
	}()
	exit := make(chan error, 1)
	waitDone := make(chan struct{})
	go func() { exit <- cmd.Wait(); close(waitDone) }()
	defer func() {
		cancel()
		_ = terminal.Close()
		select {
		case <-waitDone:
		case <-time.After(5 * time.Second):
			finalErr = errors.Join(finalErr, context.DeadlineExceeded)
		}
		select {
		case <-readDone:
		case <-time.After(time.Second):
			finalErr = errors.Join(finalErr, context.DeadlineExceeded)
		}
	}()
	var driveErr error
	if r.Op == "terminal.ui" {
		// The label is emitted by the real CLI BeforeRaw hook after READY and,
		// for take-control, successful server-side acquisition. Never inject an
		// application input merely to obtain the label.
		until := time.NewTimer(10 * time.Second)
	waitPrompt:
		for {
			mu.Lock()
			matched := strings.Contains(output.String(), expectedPrompt)
			mu.Unlock()
			if matched {
				break
			}
			select {
			case <-call.Done():
				driveErr = call.Err()
				break waitPrompt
			case <-waitDone:
				driveErr = errors.New("terminal exited before mode prompt")
				break waitPrompt
			case <-until.C:
				driveErr = context.DeadlineExceeded
				break waitPrompt
			case <-time.After(10 * time.Millisecond):
			}
		}
		until.Stop()
		if driveErr == nil {
			select {
			case <-call.Done():
				driveErr = call.Err()
			case <-time.After(100 * time.Millisecond):
				_, driveErr = io.WriteString(terminal, "\x11")
			}
		}
		if driveErr != nil {
			cancel()
		}
	} else {
		// Wait on actual rendered UI, including each selected panel. TTY
		// negotiation can take several seconds and discards prematurely sent keys.
		snapshot := func() (string, bool) { mu.Lock(); defer mu.Unlock(); return output.String(), output.overflow }
		wait := func(since int, want ...string) (int, error) {
			return waitTUIOutput(call, waitDone, outputChanged, snapshot, since, want)
		}
		driveErr = driveDashboard(terminal, wait, a.Tabs, a.Contains)
		if driveErr != nil {
			cancel()
		}

	}
	var exitErr error
	select {
	case exitErr = <-exit:
	case <-call.Done():
		cancel()
		exitErr = <-exit
	}
	_ = terminal.Close()
	select {
	case <-readDone:
	case <-time.After(time.Second):
		return nil, context.DeadlineExceeded
	}
	mu.Lock()
	data := append([]byte(nil), output.Bytes()...)
	overflow := output.overflow
	mu.Unlock()
	capture, err := os.CreateTemp(h.root, "tui-private-*.capture")
	if err != nil {
		return nil, err
	}
	_, err = capture.Write(data)
	err = errors.Join(err, capture.Close())
	if err != nil {
		return nil, err
	}
	checks := map[string]bool{}
	for _, s := range a.Contains {
		checks[s] = strings.Contains(string(data), s)
	}
	result = map[string]any{"checks": checks, "sha256": digest(data), "bytes": len(data), "capture_path": capture.Name(), "exit_code": cmd.ProcessState.ExitCode()}
	if r.Op == "terminal.ui" {
		v := result.(map[string]any)
		v["expected_mode"] = expectedMode
		v["matched"] = checks[expectedPrompt]
		if !checks[expectedPrompt] && driveErr == nil {
			driveErr = errors.New("terminal mode prompt absent")
		}
	}
	if overflow {
		return result, invalid()
	}
	return result, errors.Join(driveErr, exitErr)
}

// waitTUIOutput only considers new output after since, so a previous visit to a
// panel cannot acknowledge a later Tab. Notifications are coalesced, but the
// complete bounded capture is checked before every wait.
func waitTUIOutput(ctx context.Context, exited, changed <-chan struct{}, snapshot func() (string, bool), since int, want []string) (int, error) {
	for {
		text, overflow := snapshot()
		if overflow {
			return 0, errors.New("TUI capture overflow")
		}
		if since < 0 || since > len(text) {
			return 0, invalid()
		}
		matched := true
		for _, value := range want {
			if !strings.Contains(text[since:], value) {
				matched = false
				break
			}
		}
		if matched {
			return len(text), nil
		}
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-exited:
			return 0, errors.New("dashboard exited before expected output")
		case <-changed:
		}
	}
}

func driveDashboard(input io.Writer, wait func(int, ...string) (int, error), tabs int, contains []string) error {
	offset, err := wait(0, "[Sessions]", "q quit")
	if err != nil {
		return err
	}
	panels := []string{"[Sessions]", "[Sync & conflicts]", "[Ports]", "[Plugins]", "[Node]"}
	for i := 0; i < tabs; i++ {
		if _, err = io.WriteString(input, "\t"); err != nil {
			return err
		}
		offset, err = wait(offset, panels[(i+1)%len(panels)])
		if err != nil {
			return err
		}
	}
	if _, err = wait(0, contains...); err != nil {
		return err
	}
	_, err = io.WriteString(input, "q")
	return err
}
