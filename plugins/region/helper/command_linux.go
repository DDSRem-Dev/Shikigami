//go:build linux

package helper

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const commandDrainTimeout = time.Second

// runOwnedCommand is the sole waiter for a command started in its own process
// group. WNOWAIT pins the leader's PID, including after exit, until its entire
// group has been signaled. Never signal a numeric process group after Wait has
// reaped its leader: that ID could already identify an unrelated process.
func runOwnedCommand(ctx context.Context, command *exec.Cmd) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.WaitDelay = commandDrainTimeout
	if err := command.Start(); err != nil {
		return err
	}
	exited := make(chan error, 1)
	go func() {
		var info unix.Siginfo
		for {
			err := unix.Waitid(unix.P_PID, command.Process.Pid, &info, unix.WEXITED|unix.WNOWAIT, nil)
			if err == unix.EINTR {
				continue
			}
			exited <- err
			return
		}
	}()
	var waitErr, killErr error
	select {
	case waitErr = <-exited:
	case <-ctx.Done():
		// Signal through the live os.Process handle (pidfd on supported Linux),
		// then wait without reaping before touching the numeric group ID.
		killErr = ignoreFinishedProcess(command.Process.Kill())
		waitErr = <-exited
	}
	if waitErr == nil {
		killErr = errors.Join(killErr, ignoreFinishedProcess(unix.Kill(-command.Process.Pid, unix.SIGKILL)))
	} else {
		// Without a pinned child identity, no numeric group kill is safe.
		killErr = errors.Join(killErr, ignoreFinishedProcess(command.Process.Kill()))
	}
	// WaitDelay also bounds escaped descendants retaining stdout/stderr. Group
	// cleanup happens before this call can reap the leader and release its ID.
	runErr := command.Wait()
	return errors.Join(ctx.Err(), waitErr, killErr, runErr)
}

func ignoreFinishedProcess(err error) error {
	if errors.Is(err, os.ErrProcessDone) || errors.Is(err, unix.ESRCH) {
		return nil
	}
	return err
}
