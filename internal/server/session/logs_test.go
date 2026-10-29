package session

import (
	"context"
	"errors"
	"testing"
	"time"

	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
)

func TestLogCaptureDeadlineIncludesLifecycleLock(t *testing.T) {
	m, _ := managerFixture(t, 1)
	m.operations.Lock()
	defer m.operations.Unlock()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := m.GetLogs(ctx, &pb.SessionLogsRequest{SessionId: "sess_waiting"}); done <- err }()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("log query retained its handler while waiting for lifecycle lock")
	}
}
