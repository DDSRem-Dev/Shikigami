package session

import (
	"context"
	"encoding/json"
	"sort"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/server/monitor"
	"github.com/DDSRem-Dev/Shikigami/internal/server/storage"
	"github.com/DDSRem-Dev/Shikigami/internal/server/tmux"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
)

// SampleTargets never reads persisted ClaudePid. Only a unique live pane whose
// exact window/pane matches the registered session can supply a sampling PID.
func (m *Manager) SampleTargets(ctx context.Context, sessionID string) ([]monitor.Target, error) {
	m.operations.Lock()
	defer m.operations.Unlock()
	ctx, release, err := m.operationContext(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	state, err := m.store.View()
	if err != nil {
		return nil, storageError(err)
	}
	if sessionID != "" {
		if !storage.ValidResourceID(sessionID, "sess") {
			return nil, rpcError(connect.CodeInvalidArgument, "valid session ID required")
		}
		if _, ok := state.Buckets[storage.SessionsBucket][sessionID]; !ok {
			return nil, rpcError(connect.CodeNotFound, "session not found")
		}
	}
	panes, err := m.tmux.List(ctx)
	if err != nil {
		return nil, err
	}
	byID := map[string][]tmux.Pane{}
	for _, pane := range panes {
		byID[pane.SessionID] = append(byID[pane.SessionID], pane)
	}
	var result []monitor.Target
	for id, data := range state.Buckets[storage.SessionsBucket] {
		if sessionID != "" && id != sessionID {
			continue
		}
		var value pb.Session
		if err := json.Unmarshal(data, &value); err != nil {
			return nil, err
		}
		target := monitor.Target{SessionID: id, Directory: value.WorkingDir}
		if list := byID[id]; value.Lifecycle == "ready" && len(list) == 1 {
			pane := list[0]
			if !pane.Dead && pane.PID > 0 && value.TmuxPane == pane.PaneID && value.TmuxWindow == pane.WindowID {
				target.PID = int32(pane.PID)
				target.Identity = pane.TmuxSessionID + "/" + pane.WindowID + "/" + pane.PaneID
			}
		}
		result = append(result, target)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].SessionID < result[j].SessionID })
	return result, nil
}

// DiskDirectories includes registered project roots even if no session exists.
func (m *Manager) DiskDirectories(ctx context.Context, sessionID string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	state, err := m.store.View()
	if err != nil {
		return nil, storageError(err)
	}
	projectID := ""
	if sessionID != "" {
		var value pb.Session
		if err := state.Get(storage.SessionsBucket, sessionID, &value); err != nil {
			return nil, storageError(err)
		}
		projectID = value.ProjectId
	}
	var result []string
	for id, data := range state.Buckets[storage.ProjectsBucket] {
		if projectID != "" && id != projectID {
			continue
		}
		var value pb.Project
		if err := json.Unmarshal(data, &value); err != nil {
			return nil, err
		}
		result = append(result, value.Path)
	}
	return result, nil
}
