package install

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// Acquire excludes CLI auto-start for the whole client installation transaction.
// The agent itself does not acquire start.lock, so a service start remains able
// to acquire instance.lock after Backup releases its state locks.
func (n *Native) Acquire(ctx context.Context, plan Plan) (func(), error) {
	if plan.Role != "client" {
		return func() {}, nil
	}
	if n.startLock != nil {
		return nil, errors.New("client startup guard is already held")
	}
	if !filepath.IsAbs(plan.StateDir) {
		return nil, errors.New("client state directory must be absolute")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(plan.StateDir, 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(plan.StateDir)
	if err != nil {
		return nil, err
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.IsDir() || info.Mode().Perm() != 0700 || owner.Uid != uint32(os.Getuid()) {
		return nil, errors.New("client state directory must be owned by this user with mode 0700")
	}
	root, err := os.OpenRoot(plan.StateDir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	pinned, err := root.Stat(".")
	if err != nil || !os.SameFile(info, pinned) {
		return nil, errors.New("client state directory changed while acquiring startup guard")
	}
	file, err := root.OpenFile("start.lock", os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0600)
	if err != nil {
		return nil, err
	}
	info, err = file.Stat()
	if err != nil {
		file.Close()
		return nil, err
	}
	owner, ok = info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || owner.Uid != uint32(os.Getuid()) {
		file.Close()
		return nil, errors.New("client startup lock must be an owned private regular file")
	}
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err = ctx.Err(); err != nil {
			file.Close()
			return nil, err
		}
		err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			n.startLock = file
			var once sync.Once
			return func() {
				once.Do(func() {
					file.Close()
					n.startLock = nil
				})
			}, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			file.Close()
			return nil, err
		}
		select {
		case <-ctx.Done():
			file.Close()
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

// ownsStartupLock compares opened file identities, never just a replaceable
// pathname. Other state locks must still be checked for live host/plugin owners.
func (n *Native) ownsStartupLock(file *os.File) (bool, error) {
	if n.startLock == nil {
		return false, nil
	}
	held, err := n.startLock.Stat()
	if err != nil {
		return false, err
	}
	candidate, err := file.Stat()
	if err != nil {
		return false, err
	}
	return os.SameFile(held, candidate), nil
}
