//go:build linux

package runtime

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"syscall"
)

const InstallLock = "/usr/lib/shikigami/shiki-browser-runtime.install-lock"

// The installer creates this inode once and never replaces it. Runtime owns a
// shared lease for its entire lifetime, independently of the plugin process.
func acquireInstallLease() (*os.File, error) {
	if err := verifyExecutableChain(InstallLock, false); err != nil {
		return nil, err
	}
	before, err := os.Lstat(InstallLock)
	if err != nil {
		return nil, errors.New("browser runtime installation lock is missing; run administrator browser setup")
	}
	stat, ok := before.Sys().(*syscall.Stat_t)
	if !ok || !before.Mode().IsRegular() || stat.Uid != 0 || stat.Nlink != 1 || before.Mode().Perm()&0022 != 0 {
		return nil, errors.New("browser installation lock is not an immutable root-owned ordinary file")
	}
	file, err := os.Open(InstallLock)
	if err != nil {
		return nil, err
	}
	after, err := file.Stat()
	if err != nil || !os.SameFile(before, after) {
		file.Close()
		return nil, errors.New("browser installation lock changed during open")
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_SH|syscall.LOCK_NB); err != nil {
		file.Close()
		return nil, errors.New("browser runtime support installation is in progress; retry an explicit start after maintenance")
	}
	return file, nil
}

func executableDigest(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	const maximum = 256 << 20
	if !info.Mode().IsRegular() || info.Size() > maximum {
		return "", errors.New("runtime executable exceeds its verification bound")
	}
	hash := sha256.New()
	size, err := io.Copy(hash, io.LimitReader(file, maximum+1))
	if err != nil {
		return "", err
	}
	if size > maximum {
		return "", errors.New("runtime executable grew beyond verification bound")
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
