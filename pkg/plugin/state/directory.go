package state

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

var ErrDirectoryChanged = errors.New("state directory identity or permissions changed")

// stateDirectory pins the same directory as the writer lock. Path checks reject
// replacement; relative operations remain inside the original inode even when
// replacement races a check. No operation follows the directory pathname again.
type stateDirectory struct {
	file *os.File
	path string
	info os.FileInfo
}

func openStateDirectory(path string) (*stateDirectory, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	d := &stateDirectory{file: f, path: path, info: info}
	if err := d.check(); err != nil {
		f.Close()
		return nil, err
	}
	return d, nil
}
func (d *stateDirectory) check() error {
	current, err := os.Lstat(d.path)
	if err != nil {
		return errors.Join(ErrDirectoryChanged, err)
	}
	if !current.IsDir() || !os.SameFile(d.info, current) || current.Mode().Perm() != d.info.Mode().Perm() {
		return ErrDirectoryChanged
	}
	return nil
}
func validStateName(name string) bool {
	return name != "." && filepath.IsLocal(name) && filepath.Base(name) == name
}
func (d *stateDirectory) openPrivate(name string, flags int) (*os.File, error) {
	if !validStateName(name) {
		return nil, errors.New("invalid state filename")
	}
	fd, err := unix.Openat(int(d.file.Fd()), name, flags|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0600)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), name)
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || !ok || stat.Uid != uint32(os.Geteuid()) {
		f.Close()
		return nil, errors.New("state file must be owned by this user and mode 0600")
	}
	return f, nil
}
func (d *stateDirectory) readPrivate(name string) ([]byte, error) {
	f, err := d.openPrivate(name, os.O_RDONLY)
	if err != nil {
		return nil, err
	}
	data, readErr := io.ReadAll(io.LimitReader(f, maxStateBytes+1))
	closeErr := f.Close()
	if len(data) > maxStateBytes {
		return nil, ErrStateTooLarge
	}
	return data, errors.Join(readErr, closeErr)
}
func (d *stateDirectory) exists(name string) (bool, error) {
	if !validStateName(name) {
		return false, errors.New("invalid state filename")
	}
	var stat unix.Stat_t
	err := unix.Fstatat(int(d.file.Fd()), name, &stat, unix.AT_SYMLINK_NOFOLLOW)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}
func (d *stateDirectory) rename(old, new string) error {
	if !validStateName(old) || !validStateName(new) {
		return errors.New("invalid state filename")
	}
	return unix.Renameat(int(d.file.Fd()), old, int(d.file.Fd()), new)
}
