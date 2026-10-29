package client

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	storage "github.com/DDSRem-Dev/Shikigami/pkg/plugin/state"
	"golang.org/x/sys/unix"
)

// Every file operation is relative to this descriptor. Checking the configured
// path detects replacement; a replacement after the check still cannot redirect
// the operation to the replacement directory.
type stagingDirectory struct {
	mu   sync.Mutex
	path string
	dir  *os.File
}

const maxStagingEntries = 2*maxFiles + 1

func openStagingDirectory(path string) (*stagingDirectory, error) {
	if err := storage.EnsurePrivateDir(path); err != nil {
		return nil, err
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	fd, err := unix.Open(absolute, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	s := &stagingDirectory{path: absolute, dir: os.NewFile(uintptr(fd), absolute)}
	if err := s.check(); err != nil {
		s.dir.Close()
		return nil, err
	}
	return s, nil
}

// Caller holds mu, or owns a not-yet-published helper.
func (s *stagingDirectory) check() error {
	if s.dir == nil {
		return os.ErrClosed
	}
	var pinned, current unix.Stat_t
	if err := unix.Fstat(int(s.dir.Fd()), &pinned); err != nil {
		return err
	}
	if pinned.Mode&unix.S_IFMT != unix.S_IFDIR || pinned.Mode&0777 != 0700 || pinned.Uid != uint32(os.Geteuid()) {
		return errors.New("unsafe pinned staging directory")
	}
	fd, err := unix.Open(s.path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return fmt.Errorf("staging directory replaced: %w", err)
	}
	err = unix.Fstat(fd, &current)
	closeErr := unix.Close(fd)
	if err != nil || closeErr != nil {
		return errors.Join(err, closeErr)
	}
	if current.Dev != pinned.Dev || current.Ino != pinned.Ino {
		return errors.New("staging directory identity changed")
	}
	return nil
}

func privateStage(stat *unix.Stat_t) bool {
	return stat.Mode&unix.S_IFMT == unix.S_IFREG && stat.Mode&0777 == 0600 && stat.Uid == uint32(os.Geteuid())
}

func (s *stagingDirectory) CreateTemp() (*os.File, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(); err != nil {
		return nil, err
	}
	for range 10 {
		var random [16]byte
		if _, err := rand.Read(random[:]); err != nil {
			return nil, err
		}
		name := "transfer-" + hex.EncodeToString(random[:])
		fd, err := unix.Openat(int(s.dir.Fd()), name, unix.O_CREAT|unix.O_EXCL|unix.O_RDWR|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0600)
		if errors.Is(err, unix.EEXIST) {
			continue
		}
		if err != nil {
			return nil, err
		}
		f := os.NewFile(uintptr(fd), name)
		if err = unix.Fchmod(fd, 0600); err == nil {
			err = s.check()
		}
		if err != nil {
			f.Close()
			_ = unix.Unlinkat(int(s.dir.Fd()), name, 0)
			return nil, err
		}
		return f, nil
	}
	return nil, errors.New("cannot allocate unique staging file")
}

func (s *stagingDirectory) Open(name string) (*os.File, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !validStage(name) {
		return nil, errors.New("invalid staging filename")
	}
	if err := s.check(); err != nil {
		return nil, err
	}
	fd, err := unix.Openat(int(s.dir.Fd()), name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), name)
	var stat unix.Stat_t
	err = unix.Fstat(fd, &stat)
	if err == nil && !privateStage(&stat) {
		err = errors.New("unsafe staging file")
	}
	if err == nil {
		err = s.check()
	}
	if err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

func (s *stagingDirectory) Sync() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(); err != nil {
		return err
	}
	return errors.Join(s.dir.Sync(), s.check())
}

func (s *stagingDirectory) Remove(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !validStage(name) {
		return errors.New("invalid staging filename")
	}
	if err := s.check(); err != nil {
		return err
	}
	return s.remove(name)
}

func (s *stagingDirectory) remove(name string) error {
	var stat unix.Stat_t
	if err := unix.Fstatat(int(s.dir.Fd()), name, &stat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return err
	}
	if !privateStage(&stat) {
		return errors.New("unsafe staging file")
	}
	// Unlink never follows a replacement symlink and cannot remove a directory.
	return unix.Unlinkat(int(s.dir.Fd()), name, 0)
}

func (s *stagingDirectory) Cleanup(retain map[string]bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(); err != nil {
		return err
	}
	for name := range retain {
		if !validStage(name) {
			return errors.New("invalid retained staging filename")
		}
	}
	// Open a fresh description: dup would share a directory read cursor.
	fd, err := unix.Openat(int(s.dir.Fd()), ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return err
	}
	directory := os.NewFile(uintptr(fd), s.path)
	defer directory.Close()
	var candidates []string
	count := 0
	for {
		entries, readErr := directory.ReadDir(128)
		count += len(entries)
		if count > maxStagingEntries {
			return errors.New("staging directory entry limit exceeded")
		}
		for _, entry := range entries {
			name := entry.Name()
			if !validStage(name) || retain[name] {
				continue
			}
			var stat unix.Stat_t
			if err := unix.Fstatat(int(s.dir.Fd()), name, &stat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
				return err
			}
			if !privateStage(&stat) {
				return errors.New("unsafe staging file")
			}
			candidates = append(candidates, name)
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	// Validate the complete bounded inventory before removing any orphan.
	if err := s.check(); err != nil {
		return err
	}
	for _, name := range candidates {
		if err := s.remove(name); err != nil {
			return err
		}
	}
	if len(candidates) != 0 {
		return errors.Join(s.dir.Sync(), s.check())
	}
	return nil
}

func (s *stagingDirectory) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dir == nil {
		return nil
	}
	err := s.dir.Close()
	s.dir = nil
	return err
}
