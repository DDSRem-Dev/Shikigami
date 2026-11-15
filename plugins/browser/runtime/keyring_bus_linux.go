//go:build linux

package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const maxSecretsBusOwnerBytes = 4096

type secretsBusOwner struct {
	PID   int    `json:"pid"`
	Start string `json:"start"`
}

func procStartTime(pid int) (string, error) {
	if pid <= 0 {
		return "", errors.New("invalid owned secret-bus PID")
	}
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return "", err
	}
	end := strings.LastIndex(string(data), ") ")
	if end < 0 {
		return "", errors.New("invalid owned process identity")
	}
	fields := strings.Fields(string(data[end+2:]))
	if len(fields) < 20 {
		return "", errors.New("incomplete owned process identity")
	}
	return fields[19], nil
}
func writeSecretsBusConfig(path, bus string) error {
	// No service directories: if this SecretService exits, D-Bus must not
	// silently activate the user's unrelated/default keyring as a fallback.
	content := fmt.Sprintf(`<busconfig><type>session</type><listen>unix:path=%s</listen><auth>EXTERNAL</auth><policy context="default"><deny user="*"/><allow user="%d"/><allow own="*"/><allow send_destination="*"/><allow receive_sender="*"/></policy></busconfig>`, bus, os.Getuid())
	return atomicFile(path, []byte(content))
}
func ensureDedicatedSecretsBus(ctx context.Context, location keyringLocation) (bool, error) {
	if err := PrivateDir(location.Run); err != nil {
		return false, err
	}
	ownerPath := filepath.Join(location.Run, "bus-owner.json")
	if info, err := os.Lstat(location.Bus); err == nil {
		if info.Mode()&os.ModeSocket == 0 || !owned(info) || info.Mode().Perm()&0077 != 0 {
			return false, errors.New("private secret bus endpoint is not a user-owned private socket")
		}
		if connection, err := dialPrivate(ctx, location.Bus); err == nil {
			connection.Close()
			return false, nil
		}
		var owner secretsBusOwner
		if err := ReadJSON(ownerPath, &owner, maxSecretsBusOwnerBytes); err != nil {
			return false, errors.New("unclaimed secret-bus socket; administrator must verify and remove stale endpoint")
		}
		started, err := procStartTime(owner.PID)
		if err == nil && started == owner.Start {
			return false, errors.New("recorded secret bus owner is still alive; refusing stale-socket removal")
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return false, errors.New("secret-bus owner cannot be proven exited")
		}
		if err := os.Remove(location.Bus); err != nil {
			return false, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	// Persist the initiating process before fork. A failed systemd invocation
	// kills its entire cgroup; startup ownership survives a partial fork.
	started, err := procStartTime(os.Getpid())
	if err != nil {
		return false, err
	}
	if err := AtomicJSON(ownerPath, secretsBusOwner{PID: os.Getpid(), Start: started}, maxSecretsBusOwnerBytes); err != nil {
		return false, err
	}
	config := filepath.Join(location.Run, "bus.conf")
	if err := writeSecretsBusConfig(config, location.Bus); err != nil {
		return false, err
	}
	if err := verifyExecutable("/usr/bin/dbus-daemon"); err != nil {
		return false, err
	}
	pidFile, err := os.CreateTemp(location.Run, ".bus-pid-")
	if err != nil {
		return false, err
	}
	defer pidFile.Close()
	defer os.Remove(pidFile.Name())
	command := exec.CommandContext(ctx, "/usr/bin/dbus-daemon", "--config-file="+config, "--fork", "--print-pid=1")
	command.Env = keyringEnvironment(location)
	command.Stdout, command.Stderr = pidFile, nil
	if err := command.Run(); err != nil {
		return false, errors.New("private secret bus did not start")
	}
	if _, err := pidFile.Seek(0, io.SeekStart); err != nil {
		return false, err
	}
	output, err := io.ReadAll(io.LimitReader(pidFile, 64))
	if err != nil {
		return false, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(output)))
	if err != nil {
		return false, errors.New("private secret bus did not return an owned process identity")
	}
	started, err = procStartTime(pid)
	if err != nil {
		return false, err
	}
	if err := AtomicJSON(ownerPath, secretsBusOwner{PID: pid, Start: started}, maxSecretsBusOwnerBytes); err != nil {
		return false, err
	}
	if err := awaitPath(ctx, location.Bus, 5*time.Second); err != nil {
		return false, err
	}
	if err := os.Chmod(location.Bus, 0600); err != nil {
		return false, err
	}
	connection, err := dialPrivate(ctx, location.Bus)
	if err != nil {
		return false, err
	}
	connection.Close()
	return true, nil
}

func stopVerificationChild(process *child) {
	if process == nil {
		return
	}
	_ = process.command.Process.Signal(syscall.SIGTERM)
	select {
	case <-process.done:
	case <-time.After(3 * time.Second):
		_ = process.command.Process.Kill()
		<-process.done
	}
}
