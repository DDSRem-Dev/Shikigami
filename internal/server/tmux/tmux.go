// Package tmux owns all tmux subprocesses. Closing an attachment never destroys
// the tmux session: only Delete does that.
package tmux

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

var (
	ErrNotFound      = errors.New("tmux session not found")
	validSession     = regexp.MustCompile(`^sess_[a-zA-Z0-9_-]{1,96}$`)
	validTmuxSession = regexp.MustCompile(`^\$[0-9]+$`)
)

type Client struct {
	binary string
	socket string
}

type Pane struct {
	SessionID         string
	TmuxSessionID     string // tmux's exact $N target, never a name prefix
	WindowID          string
	PaneID            string
	PID               int
	Dead              bool
	ExitStatus        *int
	ExitSignal        int
	ExitSignalUnknown bool
}

// AbnormalExit requires a retained dead pane and an actual observed cause.
// A signal is distinct from an exit code; unknown signal names fail closed.
func (p Pane) AbnormalExit() bool {
	return p.Dead && !p.ExitSignalUnknown && (p.ExitSignal != 0 || p.ExitStatus != nil && *p.ExitStatus != 0)
}

func New(binary string) (*Client, error) {
	if binary == "" {
		binary = "tmux"
	}
	path, err := exec.LookPath(binary)
	if err != nil {
		return nil, fmt.Errorf("tmux dependency: %w", err)
	}
	return &Client{binary: path, socket: "shiki-" + strconv.Itoa(os.Getuid())}, nil
}

func Name(id string) (string, error) {
	if !validSession.MatchString(id) {
		return "", errors.New("invalid session ID")
	}
	return "shiki-" + id, nil
}

func (c *Client) command(ctx context.Context, args ...string) *exec.Cmd {
	// Daemon service environments may have LANG=C. The terminal wire contract
	// is UTF-8, so explicitly ask tmux not to substitute non-ASCII characters.
	base := []string{"-u", "-L", c.socket, "-f", "/dev/null"}
	cmd := exec.CommandContext(ctx, c.binary, append(base, args...)...)
	cmd.WaitDelay = time.Second
	// Do not attach inside an inherited tmux server, and keep rendering stable.
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "TMUX=") && !strings.HasPrefix(e, "TERM=") {
			cmd.Env = append(cmd.Env, e)
		}
	}
	cmd.Env = append(cmd.Env, "TERM=xterm-256color")
	return cmd
}

func (c *Client) run(ctx context.Context, args ...string) ([]byte, error) {
	command := c.command(ctx, args...)
	output := &boundedCapture{limit: 1 << 20}
	command.Stdout, command.Stderr = output, output
	err := command.Run()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if output.truncated {
		return nil, fmt.Errorf("tmux %s: output exceeds 1 MiB limit", args[0])
	}
	out := []byte(output.String())
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("tmux %s: %w: %s", args[0], err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

// Create starts a detached session, sets pane retention before launching the
// requested argv, and never evaluates caller strings as a shell command.
// Empty argv leaves a plain interactive shell for auto_start_claude=false.
func (c *Client) Create(ctx context.Context, id, dir string, argv []string) (Pane, error) {
	name, err := Name(id)
	if err != nil {
		return Pane{}, err
	}
	if !strings.HasPrefix(dir, "/") {
		return Pane{}, errors.New("working directory must be absolute")
	}
	if len(argv) > 0 && !strings.HasPrefix(argv[0], "/") {
		return Pane{}, errors.New("executable must be resolved to an absolute path")
	}
	_, err = c.run(ctx, "new-session", "-d", "-s", name, "-c", dir, "-x", "80", "-y", "24", "/bin/sh")
	if err != nil {
		return Pane{}, err
	}
	rollback := func(cause error) (Pane, error) {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
		defer cancel()
		return Pane{}, errors.Join(cause, c.Delete(cleanup, id))
	}
	pane, err := c.Inspect(ctx, id)
	if err != nil {
		return rollback(err)
	}
	if _, err = c.run(ctx, "set-option", "-w", "-t", pane.WindowID, "remain-on-exit", "on"); err != nil {
		return rollback(err)
	}
	if _, err = c.run(ctx, "set-option", "-t", pane.TmuxSessionID, "status", "off"); err != nil {
		return rollback(err)
	}
	if len(argv) > 0 {
		if err = c.Respawn(ctx, id, argv); err != nil {
			return rollback(err)
		}
	}
	pane, err = c.Inspect(ctx, id)
	if err != nil {
		return rollback(err)
	}
	return pane, nil
}

// tmux 3.3 sanitizes literal control characters in formats to underscores.
// A printable delimiter is portable; none of these validated fields allow '|'.
const paneFormat = "#{session_name}|#{window_id}|#{pane_id}|#{pane_pid}|#{pane_dead}|#{pane_dead_status}|#{session_id}|#{pane_dead_signal}"

func parsePane(line string) (Pane, error) {
	fields := strings.Split(strings.TrimSuffix(line, "\r"), "|")
	if len(fields) != 8 {
		return Pane{}, errors.New("unexpected tmux pane format")
	}
	id := strings.TrimPrefix(fields[0], "shiki-")
	if _, err := Name(id); err != nil {
		return Pane{}, err
	}
	pid, err := strconv.Atoi(fields[3])
	if err != nil {
		return Pane{}, fmt.Errorf("parse pane PID: %w", err)
	}
	if !validTmuxSession.MatchString(fields[6]) {
		return Pane{}, errors.New("invalid tmux session identity")
	}
	p := Pane{SessionID: id, TmuxSessionID: fields[6], WindowID: fields[1], PaneID: fields[2], PID: pid, Dead: fields[4] == "1"}
	if fields[5] != "" {
		exit, err := strconv.Atoi(fields[5])
		if err != nil {
			return Pane{}, err
		}
		p.ExitStatus = &exit
	}
	if fields[7] != "" {
		signal, err := strconv.Atoi(fields[7])
		if err != nil {
			// tmux reports strsignal-style names such as "term" on macOS.
			signal = int(unix.SignalNum("SIG" + strings.ToUpper(strings.TrimPrefix(strings.ToLower(fields[7]), "sig"))))
			p.ExitSignalUnknown = signal == 0
		}
		if signal < 0 || signal > 127 {
			return Pane{}, errors.New("invalid tmux exit signal")
		}
		p.ExitSignal = signal
	}
	return p, nil
}

// List distinguishes a missing private server from a failed tmux command. It
// never treats permission or execution failures as an empty session inventory.
func (c *Client) List(ctx context.Context) ([]Pane, error) {
	panes, err := c.listOnce(ctx)
	if err == nil || !strings.HasSuffix(err.Error(), ": server exited unexpectedly") {
		return panes, err
	}
	// Last-session deletion can race server shutdown during either list-panes
	// or its list-sessions confirmation. Retry the complete read once; the exit
	// error itself is never evidence of an empty inventory.
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(20 * time.Millisecond):
	}
	return c.listOnce(ctx)
}

func (c *Client) listOnce(ctx context.Context) ([]Pane, error) {
	out, err := c.run(ctx, "list-panes", "-a", "-F", paneFormat)
	if err != nil {
		// tmux has no structured no-server status. These narrowly matched
		// diagnostics are emitted before any session command is executed.
		message := err.Error()
		if strings.HasSuffix(message, ": no current target") {
			// tmux 3.3a can keep its server alive briefly after the last
			// session disappears. Verify the session inventory before treating
			// a list-panes target error as an empty managed inventory.
			inventory, inventoryErr := c.run(ctx, "list-sessions", "-F", "#{session_name}")
			if inventoryErr == nil && len(bytes.TrimSpace(inventory)) == 0 {
				return nil, nil
			}
			if missingServer(inventoryErr) {
				return nil, nil
			}
			return nil, errors.Join(err, inventoryErr)
		}
		if missingServer(err) {
			return nil, nil
		}
		return nil, err
	}
	var panes []Pane
	for line := range strings.SplitSeq(strings.TrimSuffix(string(out), "\n"), "\n") {
		if !strings.HasPrefix(line, "shiki-sess_") {
			continue
		}
		p, err := parsePane(line)
		if err != nil {
			return nil, err
		}
		panes = append(panes, p)
	}
	return panes, nil
}

func missingServer(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return strings.Contains(message, "no server running on ") || (strings.Contains(message, "error connecting to ") && strings.HasSuffix(message, "(No such file or directory)"))
}

func (c *Client) Inspect(ctx context.Context, id string) (Pane, error) {
	if _, err := Name(id); err != nil {
		return Pane{}, err
	}
	panes, err := c.List(ctx)
	if err != nil {
		return Pane{}, err
	}
	var found *Pane
	for _, p := range panes {
		if p.SessionID == id {
			if found != nil {
				return Pane{}, errors.New("managed session has multiple panes; manual intervention required")
			}
			copy := p
			found = &copy
		}
	}
	if found == nil {
		return Pane{}, ErrNotFound
	}
	return *found, nil
}

func (c *Client) Respawn(ctx context.Context, id string, argv []string) error {
	if len(argv) == 0 || !strings.HasPrefix(argv[0], "/") {
		return errors.New("executable must be absolute")
	}
	p, err := c.Inspect(ctx, id)
	if err != nil {
		return err
	}
	// Passing more than one command argument uses tmux's exec argv form.
	// For a single executable use an explicit exec wrapper with positional args
	// so characters in a binary path are never interpreted by a shell.
	args := []string{"respawn-pane", "-k", "-t", p.PaneID, "/bin/sh", "-c", `exec "$@"`, "shiki"}
	_, err = c.run(ctx, append(args, argv...)...)
	return err
}

// RespawnDead deliberately omits -k. tmux atomically refuses a pane which has
// become live after inspection; automatic recovery must never kill a live task.
func (c *Client) RespawnDead(ctx context.Context, id, expectedPane string, argv []string) error {
	if len(argv) == 0 || !strings.HasPrefix(argv[0], "/") {
		return errors.New("executable must be absolute")
	}
	p, err := c.Inspect(ctx, id)
	if err != nil {
		return err
	}
	if p.PaneID != expectedPane || !p.AbnormalExit() {
		return errors.New("automatic restart requires the same abnormally exited pane")
	}
	args := []string{"respawn-pane", "-t", p.PaneID, "/bin/sh", "-c", `exec "$@"`, "shiki"}
	_, err = c.run(ctx, append(args, argv...)...)
	return err
}

// Stop returns the pane to a shell, using tmux's current pane identity rather
// than signaling a PID recovered from an old state file.
func (c *Client) Stop(ctx context.Context, id string) error {
	return c.Respawn(ctx, id, []string{"/bin/sh"})
}

func (c *Client) Delete(ctx context.Context, id string) error {
	pane, err := c.Inspect(ctx, id)
	if errors.Is(err, ErrNotFound) {
		return nil
	} else if err != nil {
		return err
	}
	_, err = c.run(ctx, "kill-session", "-t", pane.TmuxSessionID)
	return err
}

// DeleteDead evaluates the guard and deletion in tmux's server command queue.
// -F uses its format evaluator, never an external shell. A changed/live pane,
// extra pane/window or changed identity cannot take the true branch. This is
// not a general filesystem/process CAS against an arbitrary privileged actor.
func (c *Client) DeleteDead(ctx context.Context, id string, expected Pane) (bool, error) {
	pane, err := c.Inspect(ctx, id)
	if errors.Is(err, ErrNotFound) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if !pane.Dead || pane.TmuxSessionID != expected.TmuxSessionID || pane.WindowID != expected.WindowID || pane.PaneID != expected.PaneID {
		return false, nil
	}
	if !validTmuxSession.MatchString(pane.TmuxSessionID) || !regexp.MustCompile(`^@[0-9]+$`).MatchString(pane.WindowID) || !regexp.MustCompile(`^%[0-9]+$`).MatchString(pane.PaneID) {
		return false, errors.New("invalid tmux identity")
	}
	guard := fmt.Sprintf("#{&&:#{pane_dead},#{&&:#{==:#{session_id},%s},#{&&:#{==:#{window_id},%s},#{&&:#{==:#{pane_id},%s},#{&&:#{==:#{session_windows},1},#{==:#{window_panes},1}}}}}}", pane.TmuxSessionID, pane.WindowID, pane.PaneID)
	out, err := c.run(ctx, "if-shell", "-F", "-t", pane.PaneID, guard, "display-message -p SHIKI_CLEANED ; kill-session -t "+pane.TmuxSessionID, "display-message -p SHIKI_RETAINED")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(out)) == "SHIKI_CLEANED", nil
}
