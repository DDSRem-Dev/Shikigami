//go:build darwin || linux

package client

import (
	"context"
	"errors"
	"io"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

// terminalFD owns a duplicate descriptor during raw mode. Nonblocking IO plus
// bounded poll waits allow cancellation without leaving a reader behind. The
// original file-description flags are restored after all terminal pumps join.
type terminalFD struct {
	fd, flags int
	// Only a console prompt needs the initial job-control read. Stream pumps
	// retain readiness-first behavior throughout their lifetime.
	readFirst bool
}

func openTerminalFD(file *os.File) (*terminalFD, error) {
	fd, e := unix.Dup(int(file.Fd()))
	if e != nil {
		return nil, e
	}
	unix.CloseOnExec(fd)
	flags, e := unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
	if e != nil {
		unix.Close(fd)
		return nil, e
	}
	if e = unix.SetNonblock(fd, true); e != nil {
		unix.Close(fd)
		return nil, e
	}
	return &terminalFD{fd: fd, flags: flags}, nil
}
func (f *terminalFD) Close() error {
	_, e := unix.FcntlInt(uintptr(f.fd), unix.F_SETFL, f.flags)
	return errors.Join(e, unix.Close(f.fd))
}
func (f *terminalFD) wait(ctx context.Context, event int16) error {
	for {
		if e := ctx.Err(); e != nil {
			return e
		}
		descriptors := []unix.PollFd{{Fd: int32(f.fd), Events: event}}
		n, e := unix.Poll(descriptors, 50)
		if errors.Is(e, unix.EINTR) {
			continue
		}
		if e != nil {
			return e
		}
		if n == 0 {
			continue
		}
		if descriptors[0].Revents&unix.POLLNVAL != 0 {
			// Darwin cannot poll /dev/tty, although nonblocking read/write work.
			// Distinguish that device limitation from an actually closed fd and
			// retry the nonblocking operation at a bounded, cancellable interval.
			if _, err := unix.FcntlInt(uintptr(f.fd), unix.F_GETFL, 0); err != nil {
				return os.ErrClosed
			}
			timer := time.NewTimer(50 * time.Millisecond)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
				return nil
			}
		}
		return nil
	}
}
func (f *terminalFD) Read(ctx context.Context, data []byte) (int, error) {
	for {
		if e := ctx.Err(); e != nil {
			return 0, e
		}
		// A first console read delivers SIGTTIN when sudo's PTY monitor still
		// owns the foreground. Poll alone never requests that handoff.
		if !f.readFirst {
			if e := f.wait(ctx, unix.POLLIN); e != nil {
				return 0, e
			}
		}
		f.readFirst = false
		n, e := unix.Read(f.fd, data)
		if errors.Is(e, unix.EAGAIN) || errors.Is(e, unix.EINTR) {
			continue
		}
		if n == 0 && e == nil {
			return 0, io.EOF
		}
		if n < 0 {
			n = 0
		}
		return n, e
	}
}
func (f *terminalFD) Write(ctx context.Context, data []byte) error {
	for len(data) > 0 {
		if e := f.wait(ctx, unix.POLLOUT); e != nil {
			return e
		}
		n, e := unix.Write(f.fd, data)
		if n > 0 {
			data = data[n:]
		}
		if errors.Is(e, unix.EAGAIN) || errors.Is(e, unix.EINTR) {
			continue
		}
		if e != nil {
			return e
		}
		if n == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}
func (f *terminalFD) DiscardPending() error {
	data := make([]byte, 4096)
	for {
		n, e := unix.Read(f.fd, data)
		if errors.Is(e, unix.EAGAIN) {
			return nil
		}
		if errors.Is(e, unix.EINTR) {
			continue
		}
		if e != nil {
			return e
		}
		if n == 0 {
			return io.EOF
		}
		if n, e := unix.Poll([]unix.PollFd{{Fd: int32(f.fd), Events: unix.POLLIN}}, 0); e != nil || n == 0 {
			return e
		}
	}
}
