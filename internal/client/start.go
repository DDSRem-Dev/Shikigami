package client

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/pkg/config"
)

func startAndConnect(ctx context.Context, path string, cfg *config.Client, opts Options) (*Client, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	lock, e := startupLock(ctx, cfg.Agent.StateDir)
	if e != nil {
		return nil, e
	}
	defer lock.Close()
	// Socket shutdown precedes worker/node cleanup. Do not launch a child
	// while the previous instance still owns its lifetime lock: that child
	// would exit immediately and leave this caller waiting for nobody.
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if c, e := dial(ctx, path, cfg, opts.Timeout); e == nil {
			return c, nil
		} else if !agentUnavailable(e) {
			return nil, e
		}
		released, e := instanceReleased(cfg.Agent.StateDir)
		if e != nil {
			return nil, e
		}
		if released {
			break
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("existing agent has not released its instance lock: %w", ctx.Err())
		case <-time.After(25 * time.Millisecond):
		}
	}
	binary := opts.AgentBinary
	if binary == "" {
		executable, e := os.Executable()
		if e != nil {
			return nil, e
		}
		candidate := filepath.Join(filepath.Dir(executable), "shiki-agent")
		if info, e := os.Stat(candidate); e == nil && !info.IsDir() && info.Mode().Perm()&0111 != 0 {
			binary = candidate
		} else {
			binary, e = exec.LookPath("shiki-agent")
			if e != nil {
				return nil, errors.New("shiki-agent executable was not found beside shiki or on PATH")
			}
		}
	}
	logPath := filepath.Join(cfg.Agent.StateDir, "agent.log")
	log, e := privateFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY)
	if e != nil {
		return nil, e
	}
	defer log.Close()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	args := []string{"--config", path}
	if opts.RecoverCrashLoop {
		args = append(args, "--recover-crash-loop")
	}
	command := exec.Command(binary, args...)
	command.Stdin = nil
	command.Stdout = log
	command.Stderr = log
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if e = command.Start(); e != nil {
		return nil, fmt.Errorf("cannot start shiki-agent: %w", e)
	}
	exited := make(chan error, 1)
	go func() { exited <- command.Wait() }()
	// A competing system-service start may win the agent's instance lock.
	// Always require a real IPC handshake rather than trusting child exit status.
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	var last error
	for {
		probe, stop := context.WithTimeout(ctx, 500*time.Millisecond)
		c, e := dial(probe, path, cfg, opts.Timeout)
		stop()
		if e == nil {
			return c, nil
		}
		last = e
		if !agentUnavailable(e) && connectivityPermanent(e) {
			return nil, e
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("agent did not become ready within 10 seconds; inspect %s: %w", logPath, last)
		case <-ticker.C:
		case <-exited:
			exited = nil
		}
	}
}
func connectivityPermanent(e error) bool {
	return !errors.Is(e, context.DeadlineExceeded) && !errors.Is(e, context.Canceled) && connect.CodeOf(e) != connect.CodeDeadlineExceeded && connect.CodeOf(e) != connect.CodeCanceled
}
func startupLock(ctx context.Context, dir string) (*os.File, error) {
	if e := os.MkdirAll(dir, 0700); e != nil {
		return nil, e
	}
	info, e := os.Lstat(dir)
	if e != nil {
		return nil, e
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.IsDir() || info.Mode().Perm() != 0700 || owner.Uid != uint32(os.Getuid()) {
		return nil, errors.New("agent state directory must be owned by this user with mode 0700")
	}
	file, e := privateFile(filepath.Join(dir, "start.lock"), os.O_CREATE|os.O_RDWR)
	if e != nil {
		return nil, e
	}
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		e = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if e == nil {
			return file, nil
		}
		if !errors.Is(e, syscall.EWOULDBLOCK) && !errors.Is(e, syscall.EAGAIN) {
			file.Close()
			return nil, e
		}
		select {
		case <-ctx.Done():
			file.Close()
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}
func privateFile(path string, flags int) (*os.File, error) {
	file, e := os.OpenFile(path, flags|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0600)
	if e != nil {
		return nil, e
	}
	info, e := file.Stat()
	if e != nil {
		file.Close()
		return nil, e
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || owner.Uid != uint32(os.Getuid()) {
		file.Close()
		return nil, errors.New("private file has unsafe ownership or permissions")
	}
	return file, nil
}

// instanceReleased probes ownership without modifying the daemon's lock record.
// The agent holds this lock until all handlers, workers and node have exited.
func instanceReleased(stateDir string) (bool, error) {
	file, err := privateFile(filepath.Join(stateDir, "instance.lock"), os.O_RDONLY|syscall.O_NONBLOCK)
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	defer file.Close()
	err = syscall.Flock(int(file.Fd()), syscall.LOCK_SH|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
}

// WaitStopped observes complete shutdown, including background worker cleanup.
// An absent socket alone is not evidence that another agent can start safely.
func (c *Client) WaitStopped(ctx context.Context) error {
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		_, err := os.Lstat(c.Config.Agent.Socket)
		if errors.Is(err, os.ErrNotExist) {
			released, err := instanceReleased(c.Config.Agent.StateDir)
			if err != nil {
				return err
			}
			if released {
				return nil
			}
		} else if err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("agent shutdown is incomplete; inspect agent.log: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}
