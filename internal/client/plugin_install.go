package client

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// PluginInstallSession never starts an agent. With no agent, it exclusively
// holds the agent lifetime lock so publication cannot race a service startup.
// A live agent must be managed over IPC before changing its installed code.
type PluginInstallSession struct {
	Client           *Client
	Directory        string
	startup, offline *os.File
}

func OpenPluginInstallSession(ctx context.Context, path string, timeout time.Duration) (*PluginInstallSession, error) {
	path, cfg, err := LoadConfig(path)
	if err != nil {
		return nil, err
	}
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	startup, err := startupLock(ctx, cfg.Agent.StateDir)
	if err != nil {
		return nil, err
	}
	session := &PluginInstallSession{Directory: cfg.PluginsDir, startup: startup}
	c, err := dial(ctx, path, cfg, timeout)
	if err == nil {
		session.Client = c
		return session, nil
	}
	if !agentUnavailable(err) || ctx.Err() != nil {
		session.Close()
		return nil, err
	}
	lock, lockErr := privateFile(filepath.Join(cfg.Agent.StateDir, "instance.lock"), os.O_CREATE|os.O_RDWR)
	if lockErr != nil {
		session.Close()
		return nil, lockErr
	}
	if lockErr = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); lockErr != nil {
		lock.Close()
		session.Close()
		return nil, fmt.Errorf("agent is running or stopping but IPC is unavailable; no plugin files changed: %w", err)
	}
	session.offline = lock
	return session, nil
}

func (s *PluginInstallSession) Close() error {
	var err error
	if s.Client != nil {
		err = s.Client.Close()
		s.Client = nil
	}
	if s.offline != nil {
		err = errors.Join(err, s.offline.Close())
		s.offline = nil
	}
	if s.startup != nil {
		err = errors.Join(err, s.startup.Close())
		s.startup = nil
	}
	return err
}
