package session

import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/server/storage"
	"github.com/DDSRem-Dev/Shikigami/internal/server/tmux"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

func managerFixture(t *testing.T, maxSessions int) (*Manager, string) {
	t.Helper()
	mux, err := tmux.New("")
	if err != nil {
		t.Skipf("tmux unavailable: %v", err)
	}
	store, err := storage.Open(filepath.Join(t.TempDir(), "state"), storage.Options{Validate: storage.ValidateDaemon("tester")})
	if err != nil {
		t.Fatal(err)
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Update(func(state *storage.Snapshot) error {
		return state.Put(storage.ProjectsBucket, "proj_fixture", &pb.Project{Id: "proj_fixture", Name: "test", Path: root, User: "tester", CreatedAt: time.Now().Unix()})
	}); err != nil {
		t.Fatal(err)
	}
	m, err := NewManager(t.Context(), store, mux, ManagerOptions{User: "tester", ClaudeCommand: []string{"/bin/sleep", "30"}, MaxSessions: maxSessions})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = m.Close()
		state, err := store.View()
		if err == nil {
			for id := range state.Buckets[storage.SessionsBucket] {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				if err := mux.Delete(ctx, id); err != nil {
					t.Error(err)
				}
				cancel()
			}
		}
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})
	return m, root
}

func TestSessionConcurrentCreationIdempotencyAndQuota(t *testing.T) {
	m, _ := managerFixture(t, 1)
	request := &pb.CreateSessionRequest{ProjectId: "proj_fixture", Name: "main", RequestId: "create-once"}
	type result struct {
		value *pb.Session
		err   error
	}
	results := make(chan result, 4)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Go(func() { value, err := m.Create(t.Context(), request); results <- result{value, err} })
	}
	wg.Wait()
	close(results)
	id := ""
	for result := range results {
		if result.err != nil {
			t.Fatal(result.err)
		}
		if id != "" && id != result.value.Id {
			t.Fatal("same request created multiple sessions")
		}
		id = result.value.Id
	}
	if _, err := m.Create(t.Context(), &pb.CreateSessionRequest{ProjectId: "proj_fixture", Name: "different", RequestId: "create-once"}); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatal("idempotency parameter mismatch", err)
	}
	if _, err := m.Create(t.Context(), &pb.CreateSessionRequest{ProjectId: "proj_fixture", Name: "second", RequestId: "quota"}); connect.CodeOf(err) != connect.CodeResourceExhausted {
		t.Fatal("quota exceeded", err)
	}
	list, err := m.List(t.Context(), &pb.ListSessionsRequest{})
	if err != nil || len(list.GetSessions()) != 1 {
		t.Fatalf("list: %v %v", list, err)
	}
}

func TestSessionStopRestartRecoverAndDeletePreserveFiles(t *testing.T) {
	m, root := managerFixture(t, 10)
	marker := filepath.Join(root, "keep")
	if err := os.WriteFile(marker, []byte("keep project contents"), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := m.Create(t.Context(), &pb.CreateSessionRequest{ProjectId: "proj_fixture", Name: "main", RequestId: "lifecycle", Description: "before"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Update(t.Context(), &pb.UpdateSessionRequest{Id: s.Id, UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"description"}}}); err != nil {
		t.Fatal(err)
	}
	get, err := m.Get(t.Context(), &pb.GetSessionRequest{Id: s.Id})
	if err != nil || get.Description != "" {
		t.Fatal("field mask did not clear description", err)
	}
	stopped, err := m.StopClaude(t.Context(), &pb.SessionActionRequest{SessionId: s.Id})
	if err != nil || stopped.ClaudeStatus != "stopped" {
		t.Fatal("stop", err)
	}
	if _, err := m.RestartClaude(t.Context(), &pb.SessionActionRequest{SessionId: s.Id}); err != nil {
		t.Fatal(err)
	}
	before, err := m.tmux.Inspect(t.Context(), s.Id)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := NewManager(t.Context(), m.store, m.tmux, m.opts)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if _, err := second.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	after, err := m.tmux.Inspect(t.Context(), s.Id)
	if err != nil || after.PID != before.PID {
		t.Fatal("manager restart killed process", err)
	}
	if err := m.tmux.Delete(t.Context(), s.Id); err != nil {
		t.Fatal(err)
	}
	report, err := second.Recover(t.Context())
	if err != nil || len(report.MissingSessions) != 1 {
		t.Fatal("missing process not retained as configuration", err)
	}
	if _, err := m.tmux.Inspect(t.Context(), s.Id); !errors.Is(err, tmux.ErrNotFound) {
		t.Fatal("recovery silently restarted process", err)
	}
	if _, err := second.RestartClaude(t.Context(), &pb.SessionActionRequest{SessionId: s.Id}); err != nil {
		t.Fatal("explicit restart after host process loss", err)
	}
	if _, err := second.Delete(t.Context(), &pb.DeleteSessionRequest{Id: s.Id}); err != nil {
		t.Fatal(err)
	}
	if _, err := second.Delete(t.Context(), &pb.DeleteSessionRequest{Id: s.Id}); err != nil {
		t.Fatal("delete not idempotent", err)
	}
	contents, err := os.ReadFile(marker)
	if err != nil || string(contents) != "keep project contents" {
		t.Fatal("delete changed project files")
	}
}

func TestInterruptedCreationRecoversAsStableFailure(t *testing.T) {
	m, root := managerFixture(t, 10)
	request := &pb.CreateSessionRequest{ProjectId: "proj_fixture", Name: "interrupted", RequestId: "interrupted"}
	id := "sess_" + rand.Text()
	err := m.store.Update(func(state *storage.Snapshot) error {
		record, _, err := state.Request(createNamespace, request.RequestId, request)
		if err != nil {
			return err
		}
		record.ResourceID = id
		state.Requests[createNamespace+"\x00"+request.RequestId] = record
		return state.Put(storage.SessionsBucket, record.ResourceID, &pb.Session{Id: record.ResourceID, ProjectId: "proj_fixture", Name: "interrupted", WorkingDir: root, CreatedAt: time.Now().Unix(), Lifecycle: "creating", ClaudeStatus: "starting"})
	})
	if err != nil {
		t.Fatal(err)
	}
	report, err := m.Recover(t.Context())
	if err != nil || len(report.FailedCreations) != 1 {
		t.Fatalf("recovery %v %v", report, err)
	}
	if _, err := m.Create(t.Context(), request); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatal("interrupted request was recreated", err)
	}
}

func TestSessionDirectoryCannotEscapeViaSymlinkOrPrefix(t *testing.T) {
	m, root := managerFixture(t, 10)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	for _, directory := range []string{filepath.Join(root, "escape"), root + "-other", "../../outside"} {
		_, err := m.Create(t.Context(), &pb.CreateSessionRequest{ProjectId: "proj_fixture", Name: "escape", WorkingDir: directory, RequestId: directory})
		if err == nil {
			t.Fatal("accepted escaped directory", directory)
		}
	}
}
