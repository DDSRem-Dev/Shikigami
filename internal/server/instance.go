package server

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/DDSRem-Dev/Shikigami/internal/server/storage"
)

// The resource Store's exclusive state.lock is the daemon instance lock. Hold
// it before this function and until every listener/worker has been closed.
// Token commands use a separate short-lived auth store and do not take it.
func claimDaemonSocket(stateDir, socket string) error {
	runDir := filepath.Dir(socket)
	if err := storage.EnsurePrivateDir(runDir); err != nil {
		return err
	}
	pidPath := filepath.Join(stateDir, "run", "daemon.pid")
	previousPID := 0
	if data, err := storage.ReadPrivateFile(pidPath); err == nil {
		previousPID, err = strconv.Atoi(strings.TrimSpace(string(data)))
		if err != nil || previousPID <= 0 || len(data) > 64 {
			return errors.New("invalid daemon PID record; retain it for diagnosis")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if info, err := os.Lstat(socket); err == nil {
		owner, ok := info.Sys().(*syscall.Stat_t)
		if !ok || owner.Uid != uint32(os.Geteuid()) || info.Mode()&os.ModeSocket == 0 || info.Mode().Perm() != 0600 {
			return errors.New("unsafe daemon admin socket; refusing removal")
		}
		conn, dialErr := net.DialTimeout("unix", socket, 200*time.Millisecond)
		if dialErr == nil {
			conn.Close()
			return errors.New("daemon admin socket is still serving")
		}
		if !errors.Is(dialErr, syscall.ECONNREFUSED) && !errors.Is(dialErr, os.ErrNotExist) {
			return fmt.Errorf("cannot verify stale daemon socket: %w", dialErr)
		}
		if previousPID == 0 || !errors.Is(syscall.Kill(previousPID, 0), syscall.ESRCH) {
			return errors.New("previous daemon process is not confirmed dead; refusing socket removal")
		}
		if err = os.Remove(socket); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return storage.AtomicWrite(runDir, "daemon.pid", []byte(strconv.Itoa(os.Getpid())+"\n"))
}
