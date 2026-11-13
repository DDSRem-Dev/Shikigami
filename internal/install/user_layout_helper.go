package install

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/DDSRem-Dev/Shikigami/pkg/plugin/buildinfo"
)

// Acquire pins the new helper before current can select an older public helper.
// The startup lock remains held until every layout/config/service step finishes.
func (n *Native) Acquire(ctx context.Context, plan Plan) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if n.layoutHelper != nil {
		return nil, errors.New("installer helper is already pinned")
	}
	path, err := os.Executable()
	if err != nil {
		return nil, err
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return nil, err
	}
	helper, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	info, err := helper.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 1<<30 {
		helper.Close()
		return nil, errors.New("installer helper must be a bounded regular executable")
	}
	release, err := n.acquireStartupGuard(ctx, plan)
	if err != nil {
		helper.Close()
		return nil, err
	}
	n.layoutHelper = helper
	n.layoutHelperPath = path
	var once sync.Once
	return func() {
		once.Do(func() {
			release()
			helper.Close()
			n.layoutHelper = nil
			n.layoutHelperPath = ""
		})
	}, nil
}

func layoutExecutableAccessible(path string, expected os.FileInfo, uid, gid int) bool {
	for current := path; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return false
		}
		if current == path && !os.SameFile(expected, info) {
			return false
		}
		owner, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			return false
		}
		bit := os.FileMode(0001)
		if owner.Uid == uint32(uid) {
			bit = 0100
		} else if owner.Gid == uint32(gid) {
			bit = 0010
		}
		if info.Mode().Perm()&bit == 0 {
			return false
		}
		if current == filepath.Dir(current) {
			return true
		}
	}
}

type layoutHelperReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r layoutHelperReader) Read(buffer []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(buffer)
}

func (n *Native) layoutExecutable(ctx context.Context, uid, gid int) (string, func() error, error) {
	if n.layoutHelper == nil {
		return "", nil, errors.New("layout helper was not pinned before the installation transaction")
	}
	info, err := n.layoutHelper.Stat()
	if err != nil {
		return "", nil, err
	}
	if layoutExecutableAccessible(n.layoutHelperPath, info, uid, gid) {
		return n.layoutHelperPath, func() error { return nil }, nil
	}
	// sudo bootstrap's helper may live in a root-private directory. Only in that
	// case make a bounded, root-owned executable copy accessible to the target
	// UID; never execute the old helper with new private operations or root UID.
	parent, err := os.OpenRoot("/tmp")
	if err != nil {
		return "", nil, err
	}
	parentInfo, err := parent.Stat(".")
	if err != nil {
		parent.Close()
		return "", nil, err
	}
	owner, ok := parentInfo.Sys().(*syscall.Stat_t)
	if !ok || owner.Uid != 0 || parentInfo.Mode().Perm()&0022 != 0 && parentInfo.Mode()&os.ModeSticky == 0 {
		parent.Close()
		return "", nil, errors.New("temporary helper parent must be root-owned and protected against path replacement")
	}
	name := ".shiki-plugin-layout-" + rand.Text()
	if err = parent.Mkdir(name, 0700); err != nil {
		parent.Close()
		return "", nil, err
	}
	root, err := parent.OpenRoot(name)
	if err != nil {
		parent.Remove(name)
		parent.Close()
		return "", nil, err
	}
	var once sync.Once
	var cleanupErr error
	cleanup := func() error {
		once.Do(func() {
			cleanupErr = root.Remove("shiki-install")
			if errors.Is(cleanupErr, os.ErrNotExist) {
				cleanupErr = nil
			}
			cleanupErr = errors.Join(cleanupErr, root.Close(), parent.Remove(name), parent.Close())
		})
		return cleanupErr
	}
	file, err := root.OpenFile("shiki-install", os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0700)
	if err != nil {
		cleanup()
		return "", nil, err
	}
	source := layoutHelperReader{ctx: ctx, reader: io.NewSectionReader(n.layoutHelper, 0, info.Size())}
	_, copyErr := io.CopyN(file, source, info.Size())
	if copyErr == nil {
		copyErr = file.Chmod(0755)
	}
	if copyErr == nil {
		copyErr = file.Sync()
	}
	err = errors.Join(copyErr, file.Close())
	if err == nil {
		directory, openErr := root.Open(".")
		if openErr != nil {
			err = openErr
		} else {
			err = errors.Join(directory.Chmod(0711), directory.Close())
		}
	}
	if err != nil {
		cleanup()
		return "", nil, err
	}
	return filepath.Join("/tmp", name, "shiki-install"), cleanup, nil
}

func (n *Native) userLayoutCall(ctx context.Context, plan Plan, operation string) error {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	request := UserOperation{Prefix: n.Prefix, Plan: plan, ExpectedVersion: buildinfo.Version}
	input, err := json.Marshal(request)
	if err != nil {
		return err
	}
	defer clear(input)
	if os.Geteuid() != 0 {
		return UserHelper(ctx, operation, bytes.NewReader(input), io.Discard)
	}
	_, uid, gid, err := accountFor(plan)
	if err != nil {
		return err
	}
	path, cleanup, err := n.layoutExecutable(ctx, uid, gid)
	if err != nil {
		return err
	}
	defer cleanup()
	cmd := command(ctx, plan, true, path, "_user-"+operation)
	cmd.Stdin = bytes.NewReader(input)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err = cmd.Start(); err != nil {
		return err
	}
	// Start confirms exec succeeded. A private temporary copy is no longer needed
	// on disk, even while the new helper invokes the selected old config helper.
	cleanupErr := cleanup()
	return errors.Join(cmd.Wait(), cleanupErr)
}
