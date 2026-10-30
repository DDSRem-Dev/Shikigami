package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	frontend "github.com/DDSRem-Dev/Shikigami/internal/client"
	identity "github.com/DDSRem-Dev/Shikigami/test/diagnostics/processidentity"
	process "github.com/shirou/gopsutil/v4/process"
)

type ownedProcess struct {
	mu       sync.Mutex
	cmd      *exec.Cmd
	identity identity.Identity
	done     chan struct{}
	err      error
	log      *os.File
	killed   bool
}

func (h *helper) processOp(ctx context.Context, r request) (any, error) {
	a, err := args[struct {
		Role       string              `json:"role"`
		Identities []identity.Identity `json:"identities"`
	}](r)
	if err != nil {
		return nil, invalid()
	}
	switch r.Op {
	case "process.start":
		if a.Role != "agent" && a.Role != "daemon" {
			return nil, invalid()
		}
		h.mu.Lock()
		defer h.mu.Unlock()
		if h.closed {
			return nil, invalid()
		}
		if old := h.processes[a.Role]; old != nil {
			select {
			case <-old.done:
			default:
				return nil, invalid()
			}
		}
		binary, argv := h.agent, []string{"--config", h.config}
		if a.Role == "daemon" {
			binary = h.daemon
			argv = []string{"serve", "--config", h.daemonConfig}
		}
		if binary == "" {
			return nil, invalid()
		}
		log, err := os.CreateTemp(h.root, a.Role+"-private-*.log")
		if err != nil {
			return nil, err
		}
		cmd := exec.Command(binary, argv...)
		cmd.Dir = h.root
		cmd.Stdout = log
		cmd.Stderr = log
		cmd.Stdin = nil
		if err = cmd.Start(); err != nil {
			log.Close()
			return nil, err
		}
		p := &ownedProcess{cmd: cmd, done: make(chan struct{}), log: log}
		h.processes[a.Role] = p
		go func() { p.err = cmd.Wait(); _ = log.Close(); close(p.done) }()
		p.identity, err = (identity.Inspector{}).Inspect(ctx, int32(cmd.Process.Pid))
		if err != nil {
			_ = cmd.Process.Signal(syscall.SIGTERM)
			select {
			case <-p.done:
			case <-ctx.Done():
			}
			return nil, err
		}
		if p.identity.UID != uint32(os.Getuid()) || !ownedBinary(h.root, p.identity.Executable) {
			return nil, invalid()
		}
		return p.identity, nil
	case "process.stop", "process.kill":
		if r.Op == "process.kill" && a.Role != "agent" {
			return nil, invalid()
		}
		err = h.stopProcess(ctx, a.Role, r.Op == "process.kill")
		return map[string]bool{"exited": err == nil}, err
	case "process.inventory":
		h.mu.Lock()
		var parents []*ownedProcess
		for _, p := range h.processes {
			parents = append(parents, p)
		}
		h.mu.Unlock()
		ids := []identity.Identity{}
		for _, p := range parents {
			alive, err := (identity.Inspector{}).Alive(ctx, p.identity)
			if err != nil {
				return nil, err
			}
			if !alive {
				continue
			}
			ids = append(ids, p.identity)
			parent, err := process.NewProcess(p.identity.PID)
			if err != nil {
				return nil, err
			}
			children, err := parent.ChildrenWithContext(ctx)
			if err != nil {
				return nil, err
			}
			for _, child := range children {
				v, err := (identity.Inspector{}).Inspect(ctx, child.Pid)
				if errors.Is(err, identity.ErrDisappeared) {
					continue
				}
				if err != nil {
					return nil, err
				}
				if v.UID == uint32(os.Getuid()) && v.Parent == p.identity.PID && ownedBinary(h.root, v.Executable) {
					ids = append(ids, v)
				}
			}
		}
		sockets := []string{}
		walkErr := filepath.WalkDir(h.root, func(path string, d os.DirEntry, e error) error {
			if e != nil {
				return e
			}
			if d.Type()&os.ModeSymlink != 0 {
				return nil
			}
			if d.Type()&os.ModeSocket != 0 {
				sockets = append(sockets, path)
			}
			if len(sockets) > 128 {
				return invalid()
			}
			return nil
		})
		return map[string]any{"identities": ids, "sockets": sockets}, walkErr
	case "retired.check":
		remaining := []identity.Identity{}
		if len(a.Identities) > 64 {
			return nil, invalid()
		}
		for _, v := range a.Identities {
			if v.UID != uint32(os.Getuid()) || !ownedBinary(h.root, v.Executable) {
				return nil, invalid()
			}
			alive, err := (identity.Inspector{}).Alive(ctx, v)
			if err != nil {
				return nil, err
			}
			if alive {
				remaining = append(remaining, v)
			}
		}
		return map[string]any{"all_absent": len(remaining) == 0, "remaining": remaining}, nil
	}
	return nil, invalid()
}
func (h *helper) stopProcess(ctx context.Context, role string, kill bool) error {
	if role != "agent" && role != "daemon" {
		return invalid()
	}
	h.mu.Lock()
	p := h.processes[role]
	h.mu.Unlock()
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	select {
	case <-p.done:
		if p.err != nil && !p.killed {
			return errors.New("owned host exited unexpectedly")
		}
		if role == "agent" {
			h.closeRetiredFrontends()
		}
		return nil
	default:
	}
	v, err := (identity.Inspector{}).Inspect(ctx, p.identity.PID)
	if err != nil {
		return err
	}
	if v != p.identity || v.UID != uint32(os.Getuid()) || !ownedBinary(h.root, v.Executable) {
		return invalid()
	}
	sig := syscall.SIGTERM
	if kill {
		sig = syscall.SIGKILL
	}
	if err = p.cmd.Process.Signal(sig); err != nil {
		return err
	}
	p.killed = kill
	select {
	case <-p.done:
		if role == "agent" {
			h.closeRetiredFrontends()
		}
		if !kill && p.err != nil {
			return errors.New("owned host failed normal exit")
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(15 * time.Second):
		return context.DeadlineExceeded
	}
}

// Once our exact host has exited, ReleaseFrontend cannot succeed. Close still
// releases each local endpoint and its stream lifetimes; no new host is started.
func (h *helper) closeRetiredFrontends() {
	h.mu.Lock()
	fronts := h.fronts
	h.fronts = make(map[string]*frontend.Client)
	h.mu.Unlock()
	for _, f := range fronts {
		_ = f.Close()
	}
}
