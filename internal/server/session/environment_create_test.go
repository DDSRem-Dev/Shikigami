package session

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/server/storage"
	"github.com/DDSRem-Dev/Shikigami/internal/server/tmux"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestSessionEnvironmentFailurePreservesCodeAndCleanupOnReplay(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
		code connect.Code
	}{
		{"local cancellation", context.Canceled, connect.CodeCanceled},
		{"local deadline", context.DeadlineExceeded, connect.CodeDeadlineExceeded},
		{"worker cancellation", sdk.FromGRPC(status.Error(codes.Canceled, "private worker detail")), connect.CodeCanceled},
		{"worker deadline", sdk.FromGRPC(status.Error(codes.DeadlineExceeded, "private worker detail")), connect.CodeDeadlineExceeded},
		{"ordinary environment failure", errors.New("private environment detail"), connect.CodeFailedPrecondition},
	} {
		t.Run(test.name, func(t *testing.T) {
			m, _ := managerFixture(t, 1)
			calls := 0
			m.operations.Lock()
			m.opts.Environment = func(context.Context) (*LaunchEnvironment, error) {
				calls++
				if calls == 1 {
					return nil, fmt.Errorf("region observation: %w", test.err)
				}
				return nil, nil
			}
			m.operations.Unlock()
			noClaude := false
			request := &pb.CreateSessionRequest{ProjectId: "proj_fixture", Name: "reusable", RequestId: "failed-environment", AutoStartClaude: &noClaude}
			_, err := m.Create(t.Context(), request)
			if connect.CodeOf(err) != test.code {
				t.Errorf("environment failure code = %v, want %v", err, test.code)
			}
			var rpcErr *connect.Error
			if !errors.As(err, &rpcErr) || rpcErr.Message() != "session process startup failed" {
				t.Errorf("environment failure did not retain a safe public message: %v", err)
			}
			state, err := m.store.View()
			if err != nil {
				t.Fatal(err)
			}
			record, exists := state.Requests[createNamespace+"\x00"+request.RequestId]
			if !exists || record.Status != "failed" || record.ResourceID == "" {
				t.Fatal("creation failure was not durably recorded", record)
			}
			if record.ErrorCode != test.code.String() {
				t.Errorf("persisted failure code = %q, want %q", record.ErrorCode, test.code.String())
			}
			if len(state.Buckets[storage.SessionsBucket]) != 0 || len(state.Buckets[environmentBucket]) != 0 {
				t.Fatal("confirmed pre-launch failure retained a session or environment reservation")
			}
			if _, err := m.tmux.Inspect(t.Context(), record.ResourceID); !errors.Is(err, tmux.ErrNotFound) {
				t.Fatal("failed environment observation left a tmux process", err)
			}
			// The environment would now succeed, but the same request must
			// return its original failure without trying to launch again.
			if _, err := m.Create(t.Context(), request); connect.CodeOf(err) != test.code {
				t.Errorf("replayed failure code = %v, want %v", err, test.code)
			}
			if calls != 1 {
				t.Fatal("failed request replay repeated environment observation", calls)
			}
			fresh := &pb.CreateSessionRequest{ProjectId: request.ProjectId, Name: request.Name, RequestId: "fresh-environment", AutoStartClaude: &noClaude}
			created, err := m.Create(t.Context(), fresh)
			if err != nil || created.Id == record.ResourceID {
				t.Fatal("confirmed failure did not release the name and single-session quota", created, err)
			}
			if _, err := m.Create(t.Context(), request); connect.CodeOf(err) != test.code || calls != 2 {
				t.Errorf("name reuse changed the old request failure or replayed work: error=%v calls=%d", err, calls)
			}
		})
	}
}
