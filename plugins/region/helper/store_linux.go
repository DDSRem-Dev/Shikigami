//go:build linux

package helper

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

const maxJournalBytes = 32 << 20

type FileStore struct {
	directory string
	strict    bool
}

// NewFileStore is an explicit in-process dependency injection seam. Production
// Run always constructs it with StatePath and strict root ownership checks.
func NewFileStore(directory string, strict bool) *FileStore {
	return &FileStore{directory: directory, strict: strict}
}

func (s *FileStore) WithLock(ctx context.Context, fn func(*Journal, func() error) error) error {
	if s.strict {
		if err := trustedDirectory(s.directory); err != nil {
			return err
		}
	}
	lockPath := filepath.Join(s.directory, "writer.lock")
	fd, err := unix.Open(lockPath, unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	lock := os.NewFile(uintptr(fd), lockPath)
	defer lock.Close()
	if s.strict {
		if err := trustedFileDescriptor(lock); err != nil {
			return err
		}
	}
	for {
		err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			break
		}
		if err != unix.EAGAIN && err != unix.EWOULDBLOCK {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
	}
	defer unix.Flock(fd, unix.LOCK_UN)
	var journal Journal
	path := filepath.Join(s.directory, "journal.json")
	data, err := readNoFollow(path, maxJournalBytes, s.strict)
	if err == nil {
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&journal); err != nil {
			return fmt.Errorf("invalid root region journal: %w", err)
		}
		if decoder.Decode(new(any)) != io.EOF {
			return errors.New("invalid trailing root region journal data")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	save := func() error {
		encoded, err := json.Marshal(&journal)
		if err != nil {
			return err
		}
		if len(encoded) > maxJournalBytes {
			return fail("resource_exhausted", "root region journal exceeds storage limit")
		}
		return atomicFile(s.directory, "journal.json", encoded, 0600)
	}
	return fn(&journal, save)
}

func atomicFile(directory, name string, data []byte, mode os.FileMode) error {
	file, err := os.CreateTemp(directory, ".shiki-region-*")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	ok := false
	defer func() {
		if !ok {
			file.Close()
		}
	}()
	if err := file.Chmod(mode); err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	ok = true
	if err := os.Rename(temporary, filepath.Join(directory, name)); err != nil {
		return err
	}
	parent, err := os.Open(directory)
	if err != nil {
		return err
	}
	defer parent.Close()
	return parent.Sync()
}

func readNoFollow(path string, maximum int, strict bool) ([]byte, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("region file is not a regular file")
	}
	if strict {
		if err := trustedFileDescriptor(file); err != nil {
			return nil, err
		}
	}
	data, err := io.ReadAll(io.LimitReader(file, int64(maximum)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maximum {
		return nil, errors.New("region file exceeds size limit")
	}
	return data, nil
}

func trustedFileDescriptor(file *os.File) error {
	var stat unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &stat); err != nil {
		return err
	}
	if stat.Uid != 0 || stat.Mode&0022 != 0 || stat.Mode&unix.S_IFMT != unix.S_IFREG {
		return errors.New("region privileged file must be root-owned and not group/world writable")
	}
	return nil
}

func trustedDirectory(path string) error {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return err
	}
	if !filepath.IsAbs(resolved) {
		return errors.New("region privileged path must be absolute")
	}
	for current := filepath.Clean(resolved); ; current = filepath.Dir(current) {
		var stat unix.Stat_t
		if err := unix.Lstat(current, &stat); err != nil {
			return err
		}
		if stat.Uid != 0 || stat.Mode&0022 != 0 || stat.Mode&unix.S_IFMT != unix.S_IFDIR {
			return fmt.Errorf("untrusted region privileged directory: %s", current)
		}
		if current == "/" {
			break
		}
	}
	return nil
}

func trustedPath(path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	if err := trustedDirectory(filepath.Dir(resolved)); err != nil {
		return "", err
	}
	file, err := os.Open(resolved)
	if err != nil {
		return "", err
	}
	defer file.Close()
	if err := trustedFileDescriptor(file); err != nil {
		return "", err
	}
	return resolved, nil
}

type Policy struct {
	DaemonUID         uint32 `json:"daemon_uid"`
	PlanTTLSeconds    int    `json:"plan_ttl_seconds"`
	DNSConfirmSeconds int    `json:"dns_confirm_seconds"`
}

func loadPolicy() (Policy, error) {
	var policy Policy
	if err := trustedDirectory(filepath.Dir(PolicyPath)); err != nil {
		return policy, err
	}
	if info, err := os.Lstat(PolicyPath); err != nil {
		return policy, err
	} else if info.Mode().Perm() != 0600 {
		return policy, errors.New("root region helper policy must have mode 0600")
	}
	data, err := readNoFollow(PolicyPath, 4096, true)
	if err != nil {
		return policy, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&policy); err != nil {
		return policy, err
	}
	if decoder.Decode(new(any)) != io.EOF {
		return policy, errors.New("invalid region helper policy trailing content")
	}
	if policy.DaemonUID == 0 {
		return policy, errors.New("region helper requires a configured non-root daemon UID")
	}
	if policy.PlanTTLSeconds == 0 {
		policy.PlanTTLSeconds = 300
	}
	if policy.DNSConfirmSeconds == 0 {
		policy.DNSConfirmSeconds = 120
	}
	if policy.PlanTTLSeconds < 30 || policy.PlanTTLSeconds > 900 || policy.DNSConfirmSeconds < 30 || policy.DNSConfirmSeconds > 600 {
		return policy, errors.New("region helper policy lease bounds are invalid")
	}
	return policy, nil
}

func authorizeInvocation(action string, policy Policy) error {
	if os.Geteuid() != 0 || os.Getuid() != 0 {
		return fail("permission_denied", "region helper must run as root through the scoped installation authorization")
	}
	path, err := os.Executable()
	if err != nil {
		return err
	}
	if filepath.Clean(path) != ExecutablePath {
		return fail("permission_denied", "only the immutable installed region helper may run as root")
	}
	if _, err := trustedPath(ExecutablePath); err != nil {
		return err
	}
	if action == "guard" || action == "prepare-uninstall" || action == "activate" {
		if os.Getenv("SUDO_UID") != "" {
			return fail("permission_denied", "root administrative actions are not daemon sudo actions; clear inherited sudo identity only from an already-root installer/service")
		}
		if action == "guard" {
			if _, err := trustedPath("/etc/systemd/system/" + GuardUnit); err != nil {
				return err
			}
		}
		return nil
	}
	// A direct root administrator is already privileged. Unprivileged access
	// must come from sudo, whose protected SUDO_UID is matched to one daemon.
	if original := os.Getenv("SUDO_UID"); original != "" {
		uid, err := strconv.ParseUint(original, 10, 32)
		if err != nil || uid != uint64(policy.DaemonUID) {
			return fail("permission_denied", "region helper caller does not match the installed daemon UID")
		}
	} else if os.Getenv("SUDO_USER") != "" || strings.Contains(os.Getenv("SUDO_COMMAND"), "\n") {
		return fail("permission_denied", "invalid region helper sudo identity")
	}
	return nil
}

func installedHelperTrusted() error {
	info, err := os.Lstat(ExecutablePath)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode()&0111 == 0 {
		return errors.New("installed region helper must be an ordinary executable, not a symlink")
	}
	if _, err := trustedPath(ExecutablePath); err != nil {
		return err
	}
	if _, err := trustedPath("/usr/bin/sudo"); err != nil {
		return err
	}
	return nil
}
