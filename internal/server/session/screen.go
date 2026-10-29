package session

import (
	"connectrpc.com/connect/v2"
	"context"
	"github.com/DDSRem-Dev/Shikigami/internal/server/storage"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	"time"
)

func (m *Manager) GetScreen(ctx context.Context, request *pb.GetSessionRequest) (*pb.SessionScreen, error) {
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
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	state, err := m.store.View()
	if err != nil {
		return nil, storageError(err)
	}
	var value pb.Session
	if err := state.Get(storage.SessionsBucket, request.Id, &value); err != nil {
		return nil, storageError(err)
	}
	if value.Lifecycle != "ready" {
		return nil, rpcError(connect.CodeFailedPrecondition, "session is not ready")
	}
	screen, err := m.tmux.CaptureScreen(ctx, value.Id, value.TmuxPane, value.TmuxWindow)
	if err != nil {
		return nil, storageError(err)
	}
	return &pb.SessionScreen{SessionId: value.Id, Text: screen.Text, Cols: screen.Cols, Rows: screen.Rows, Truncated: screen.Truncated, CapturedAt: time.Now().Unix()}, nil
}
