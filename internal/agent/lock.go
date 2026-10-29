package agent

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/DDSRem-Dev/Shikigami/internal/server/storage"
)

var ErrAlreadyRunning = errors.New("agent is already running or still shutting down")

// InstanceLock must be acquired before creating the IPC listener or opening a
// tsnet state directory. A dial failure alone never permits deleting a socket.
type InstanceLock struct {
	file *os.File
	once sync.Once
	err  error
}

func AcquireInstance(stateDir, socket string) (*InstanceLock, error) {
	if !filepath.IsAbs(stateDir) || !filepath.IsAbs(socket) {
		return nil, errors.New("agent state and socket paths must be absolute")
	}
	if err := storage.EnsurePrivateDir(stateDir); err != nil {
		return nil, err
	}
	if err := storage.EnsurePrivateDir(filepath.Dir(socket)); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(stateDir, "instance.lock"), os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0600)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (*InstanceLock, error) { _ = f.Close(); return nil, err }
	stat, err := f.Stat()
	if err != nil {
		return fail(err)
	}
	owner, ok := stat.Sys().(*syscall.Stat_t)
	if !ok || owner.Uid != uint32(os.Getuid()) || !stat.Mode().IsRegular() || stat.Mode().Perm() != 0600 {
		return fail(errors.New("unsafe agent lock owner or permissions"))
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return fail(ErrAlreadyRunning)
		}
		return fail(err)
	}
	record, err := io.ReadAll(io.LimitReader(f, 65))
	if err != nil {
		return fail(err)
	}
	previousPID := 0
	if len(record) > 0 {
		previousPID, err = strconv.Atoi(strings.TrimSpace(string(record)))
		if err != nil || previousPID <= 0 || len(record) > 64 {
			return fail(errors.New("invalid agent lock record; retain it for diagnosis"))
		}
	}
	if err := recoverSocket(socket, previousPID); err != nil {
		return fail(err)
	}
	if err := f.Truncate(0); err != nil {
		return fail(err)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return fail(err)
	}
	if _, err := fmt.Fprintln(f, os.Getpid()); err != nil {
		return fail(err)
	}
	if err := f.Sync(); err != nil {
		return fail(err)
	}
	return &InstanceLock{file: f}, nil
}

func recoverSocket(path string, previousPID int) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok || owner.Uid != uint32(os.Getuid()) || info.Mode()&os.ModeSocket == 0 || info.Mode().Perm() != 0600 {
		return errors.New("unsafe existing agent socket; refusing removal")
	}
	conn, err := net.DialTimeout("unix", path, 200*time.Millisecond)
	if err == nil {
		_ = conn.Close()
		return ErrAlreadyRunning
	}
	if !errors.Is(err, syscall.ECONNREFUSED) && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("cannot verify stale socket: %w", err)
	}
	if previousPID > 0 {
		if err := syscall.Kill(previousPID, 0); !errors.Is(err, syscall.ESRCH) {
			return ErrAlreadyRunning
		}
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// Close follows listener shutdown. Clearing the record records orderly shutdown;
// crashes leave the PID for the next lock owner to check before unlinking.
func (l *InstanceLock) Close() error {
	l.once.Do(func() {
		l.err = l.file.Truncate(0)
		l.err = errors.Join(l.err, l.file.Sync())
		l.err = errors.Join(l.err, syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN), l.file.Close())
	})
	return l.err
}
