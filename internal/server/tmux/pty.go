package tmux

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"
)

const cleanupTimeout = 5 * time.Second

type Attachment struct {
	file    *os.File
	cmd     *exec.Cmd
	cancel  context.CancelFunc
	done    chan struct{}
	err     error
	once    sync.Once
	writeMu sync.Mutex
}

func validSize(rows, cols uint32) bool { return rows > 0 && cols > 0 && rows <= 1000 && cols <= 1000 }

func (c *Client) Attach(parent context.Context, id string, rows, cols uint32) (*Attachment, error) {
	if !validSize(rows, cols) {
		return nil, errors.New("terminal size must be between 1 and 1000")
	}
	pane, err := c.Inspect(parent, id)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(parent)
	cmd := c.command(ctx, "attach-session", "-t", pane.TmuxSessionID)
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)})
	if err != nil {
		cancel()
		return nil, err
	}
	// creack/pty opens a blocking descriptor. Re-wrap a nonblocking duplicate
	// so Go's poller can cancel reads/writes and enforce write deadlines.
	fd, err := unix.FcntlInt(f.Fd(), unix.F_DUPFD_CLOEXEC, 0)
	if err == nil {
		err = unix.SetNonblock(fd, true)
		if err != nil {
			_ = unix.Close(fd)
		}
	}
	if err != nil {
		cancel()
		_ = f.Close()
		_ = cmd.Wait()
		return nil, err
	}
	_ = f.Close()
	f = os.NewFile(uintptr(fd), "shiki-attach-pty")
	a := &Attachment{file: f, cmd: cmd, cancel: cancel, done: make(chan struct{})}
	go func() { a.err = cmd.Wait(); _ = f.Close(); close(a.done) }()
	return a, nil
}

func (a *Attachment) Read(p []byte) (int, error) { return a.file.Read(p) }
func (a *Attachment) Write(p []byte) (int, error) {
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	if err := a.file.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return 0, err
	}
	return a.file.Write(p)
}
func (a *Attachment) Resize(rows, cols uint32) error {
	if !validSize(rows, cols) {
		return errors.New("terminal size must be between 1 and 1000")
	}
	// pty.Setsize calls File.Fd(), which changes a Go-pollable file back to
	// blocking mode. Keep the descriptor nonblocking so resize cannot disable
	// cancellation/deadlines on later reads and writes.
	raw, err := a.file.SyscallConn()
	if err != nil {
		return err
	}
	var ioctlErr error
	if err = raw.Control(func(fd uintptr) {
		ioctlErr = unix.IoctlSetWinsize(int(fd), unix.TIOCSWINSZ, &unix.Winsize{Row: uint16(rows), Col: uint16(cols)})
	}); err != nil {
		return err
	}
	return ioctlErr
}
func (a *Attachment) Done() <-chan struct{} { return a.done }
func (a *Attachment) Wait() error           { <-a.done; return a.err }

// Close reaps only the ordinary attach child. The independent tmux server and
// the pane command survive cancellation, including parent process failure.
func (a *Attachment) Close() error {
	a.once.Do(func() { a.cancel(); _ = a.file.Close() })
	<-a.done
	return nil
}
