package session

import (
	"context"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/server/storage"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
)

func (m *Manager) GetLogs(ctx context.Context, request *pb.SessionLogsRequest) (*pb.SessionLogs, error) {
	if request == nil || !storage.ValidResourceID(request.SessionId, "sess") || request.MaxLines > 2000 {
		return nil, rpcError(connect.CodeInvalidArgument, "valid session ID and at most 2000 log lines required")
	}
	lines := request.MaxLines
	if lines == 0 {
		lines = 200
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for !m.operations.TryLock() {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
	defer m.operations.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ctx, release, err := m.operationContext(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	state, err := m.store.View()
	if err != nil {
		return nil, storageError(err)
	}
	var value pb.Session
	if err := state.Get(storage.SessionsBucket, request.SessionId, &value); err != nil {
		return nil, storageError(err)
	}
	if value.Lifecycle != "ready" {
		return nil, rpcError(connect.CodeFailedPrecondition, "session is not ready")
	}
	logs, err := m.tmux.CaptureLogs(ctx, value.Id, value.TmuxPane, value.TmuxWindow, lines)
	if err != nil {
		return nil, err
	}
	return &pb.SessionLogs{SessionId: value.Id, Text: logs.Text, State: logs.State, ReasonCode: logs.ReasonCode, Truncated: logs.Truncated, ExitCode: logs.ExitCode, CapturedAt: time.Now().Unix()}, nil
}
