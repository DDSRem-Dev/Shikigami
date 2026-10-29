package session

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/server/storage"
	"github.com/DDSRem-Dev/Shikigami/internal/server/tmux"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
)

func (m *Manager) Delete(ctx context.Context, request *pb.DeleteSessionRequest) (*pb.Empty, error) {
	if request == nil || !storage.ValidResourceID(request.Id, "sess") {
		return nil, rpcError(connect.CodeInvalidArgument, "valid session ID required")
	}
	m.operations.Lock()
	defer m.operations.Unlock()
	ctx, release, err := m.operationContext(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	var absent bool
	err = m.store.Update(func(state *storage.Snapshot) error {
		value := new(pb.Session)
		if err := state.Get(storage.SessionsBucket, request.Id, value); errors.Is(err, os.ErrNotExist) {
			absent = true
			return nil
		} else if err != nil {
			return err
		}
		value.Lifecycle = "deleting"
		return state.Put(storage.SessionsBucket, value.Id, value)
	})
	if err != nil {
		return nil, storageError(err)
	}
	if absent {
		return &pb.Empty{}, nil
	}
	if err := m.finishDelete(ctx, request.Id); err != nil {
		return nil, storageError(err)
	}
	return &pb.Empty{}, nil
}

func (m *Manager) finishDelete(ctx context.Context, id string) error {
	delete(m.cleanupSeen, id)
	m.activityMu.Lock()
	delete(m.activity, id)
	m.activityMu.Unlock()
	m.mu.Lock()
	bridge := m.bridges[id]
	delete(m.bridges, id)
	m.mu.Unlock()
	if bridge != nil {
		if err := bridge.Close(); err != nil {
			return err
		}
	}
	if err := m.tmux.Delete(ctx, id); err != nil {
		return err
	}
	return m.store.Update(func(state *storage.Snapshot) error {
		delete(state.Buckets[storage.SessionsBucket], id)
		delete(state.Buckets[restartBucket], id)
		delete(state.Buckets[cleanupBucket], id)
		return nil
	})
}

func (m *Manager) StopClaude(ctx context.Context, request *pb.SessionActionRequest) (*pb.Session, error) {
	return m.processAction(ctx, request, false)
}
func (m *Manager) RestartClaude(ctx context.Context, request *pb.SessionActionRequest) (*pb.Session, error) {
	return m.processAction(ctx, request, true)
}

func (m *Manager) processAction(ctx context.Context, request *pb.SessionActionRequest, restart bool) (*pb.Session, error) {
	if request == nil || !storage.ValidResourceID(request.SessionId, "sess") {
		return nil, rpcError(connect.CodeInvalidArgument, "valid session ID required")
	}
	m.operations.Lock()
	defer m.operations.Unlock()
	return m.processActionLocked(ctx, request, restart, false)
}

func (m *Manager) processActionLocked(ctx context.Context, request *pb.SessionActionRequest, restart, automatic bool) (*pb.Session, error) {
	ctx, release, err := m.operationContext(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	if !automatic {
		if _, err := m.recover(ctx); err != nil {
			return nil, err
		}
	}
	state, err := m.store.View()
	if err != nil {
		return nil, err
	}
	value := new(pb.Session)
	if err := state.Get(storage.SessionsBucket, request.SessionId, value); err != nil {
		return nil, storageError(err)
	}
	if value.Lifecycle != "ready" {
		return nil, rpcError(connect.CodeFailedPrecondition, "session is not ready")
	}
	if !automatic {
		if err := m.setRestartIntent(value.Id, restart); err != nil {
			return nil, err
		}
	}
	if automatic {
		pane, err := m.tmux.Inspect(ctx, value.Id)
		if err != nil {
			return nil, err
		}
		if !pane.AbnormalExit() || pane.PaneID != value.TmuxPane || pane.WindowID != value.TmuxWindow {
			return nil, rpcError(connect.CodeFailedPrecondition, "automatic restart requires a verified abnormal exit")
		}
	}
	if !restart && value.ClaudeStatus == "stopped" {
		return value, nil
	}
	if restart {
		if err := m.checkClaude(); err != nil {
			return nil, err
		}
		project := new(pb.Project)
		if err := state.Get(storage.ProjectsBucket, value.ProjectId, project); err != nil {
			return nil, storageError(err)
		}
		if _, err := sessionDirectory(project.Path, value.WorkingDir); err != nil {
			return nil, err
		}
		count := 0
		for id, data := range state.Buckets[storage.SessionsBucket] {
			if id == value.Id {
				continue
			}
			var other pb.Session
			if err := json.Unmarshal(data, &other); err != nil {
				return nil, err
			}
			if other.ClaudeStatus == "running" || other.ClaudeStatus == "starting" || other.ClaudeStatus == "unknown" {
				count++
			}
		}
		if count >= m.opts.MaxClaude {
			return nil, rpcError(connect.CodeResourceExhausted, "Claude process quota reached")
		}
	}
	// Persist intent before the external process operation. A crash leaves an
	// honest unknown/starting state for reconciliation, never a stale PID signal.
	if restart {
		value.ClaudeStatus = "starting"
	} else {
		// A crash before tmux.Stop must not label a still-live Claude process
		// stopped or free its quota. Confirm stopped only after the operation.
		value.ClaudeStatus = "unknown"
	}
	if err := m.store.Update(func(state *storage.Snapshot) error { return state.Put(storage.SessionsBucket, value.Id, value) }); err != nil {
		return nil, err
	}
	if restart {
		if automatic {
			err = m.tmux.RespawnDead(ctx, value.Id, value.TmuxPane, m.opts.ClaudeCommand)
		} else {
			err = m.tmux.Respawn(ctx, value.Id, m.opts.ClaudeCommand)
		}
		if errors.Is(err, tmux.ErrNotFound) && !automatic {
			_, err = m.tmux.Create(ctx, value.Id, value.WorkingDir, m.opts.ClaudeCommand)
		}
	} else {
		err = m.tmux.Stop(ctx, value.Id)
	}
	if err != nil {
		value.ClaudeStatus = "unknown"
		persist := m.store.Update(func(state *storage.Snapshot) error { return state.Put(storage.SessionsBucket, value.Id, value) })
		return nil, storageError(errors.Join(err, persist))
	}
	if restart {
		pane, err := m.tmux.Inspect(ctx, value.Id)
		if err != nil {
			return nil, err
		}
		// Explicit restart runs Claude even when the creation default was false.
		value.ClaudeStatus = "running"
		applyPane(value, pane)
		value.TmuxWindow, value.TmuxPane = pane.WindowID, pane.PaneID
	} else {
		value.ClaudeStatus = "stopped"
	}
	if err := m.store.Update(func(state *storage.Snapshot) error { return state.Put(storage.SessionsBucket, value.Id, value) }); err != nil {
		return nil, err
	}
	if restart {
		delete(m.crashNotified, value.Id)
	}
	return value, nil
}

type RecoveryReport struct {
	OrphanPanes     []tmux.Pane
	MissingSessions []string
	FailedCreations []string
}

// Recover reconciles committed configuration with the actual tmux namespace.
// It never recreates a process after host reboot or kills unclaimed panes.
func (m *Manager) Recover(ctx context.Context) (*RecoveryReport, error) {
	m.operations.Lock()
	defer m.operations.Unlock()
	ctx, release, err := m.operationContext(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	return m.recover(ctx)
}
func (m *Manager) recover(ctx context.Context) (*RecoveryReport, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	state, err := m.store.View()
	if err != nil {
		return nil, storageError(err)
	}
	panes, err := m.tmux.List(ctx)
	if err != nil {
		return nil, err
	}
	report := new(RecoveryReport)
	byID := map[string]tmux.Pane{}
	for _, pane := range panes {
		if _, managed := state.Buckets[storage.SessionsBucket][pane.SessionID]; !managed {
			report.OrphanPanes = append(report.OrphanPanes, pane)
			continue
		}
		if _, exists := byID[pane.SessionID]; exists {
			return nil, rpcError(connect.CodeFailedPrecondition, "managed session has multiple panes")
		}
		byID[pane.SessionID] = pane
	}
	for id, data := range state.Buckets[storage.SessionsBucket] {
		value := new(pb.Session)
		if err := json.Unmarshal(data, value); err != nil {
			return nil, err
		}
		if value.Lifecycle == "deleting" {
			if err := m.finishDelete(ctx, id); err != nil {
				return nil, err
			}
			continue
		}
		pane, present := byID[id]
		wasCreating := value.Lifecycle == "creating"
		if present {
			if wasCreating {
				value.Lifecycle = "ready"
			}
			// A process may have crashed between creating an empty tmux shell
			// and respawning Claude, or between persisting a restart intent and
			// acting on it. A live pane alone cannot prove which command runs.
			if value.ClaudeStatus == "starting" || value.Lifecycle == "failed" {
				value.ClaudeStatus = "unknown"
			}
			applyPane(value, pane)
		} else {
			report.MissingSessions = append(report.MissingSessions, id)
			value.ClaudeStatus = "stopped"
			value.TmuxWindow, value.TmuxPane = "", ""
			if wasCreating {
				value.Lifecycle = "failed"
				report.FailedCreations = append(report.FailedCreations, id)
			}
		}
		if err := m.store.Update(func(current *storage.Snapshot) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if !present && value.Lifecycle == "failed" {
				// No tmux resource remains: retain the immutable failed request,
				// release its name and resource reservation in this transaction.
				delete(current.Buckets[storage.SessionsBucket], id)
			} else if err := current.Put(storage.SessionsBucket, id, value); err != nil {
				return err
			}
			if wasCreating {
				for key, record := range current.Requests {
					if record.ResourceID != id || record.Status != "pending" || !strings.HasPrefix(key, createNamespace+"\x00") {
						continue
					}
					errorCode := ""
					if !present {
						errorCode = "failed_precondition"
					}
					if err := current.CompleteRequest(createNamespace, strings.TrimPrefix(key, createNamespace+"\x00"), id, value, errorCode); err != nil {
						return err
					}
				}
			}
			return nil
		}); err != nil {
			return nil, err
		}
	}
	return report, nil
}
