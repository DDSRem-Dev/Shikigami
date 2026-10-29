package session

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/server/project"
	"github.com/DDSRem-Dev/Shikigami/internal/server/storage"
	"github.com/DDSRem-Dev/Shikigami/internal/server/tmux"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
)

func TestConfigurationSnapshotSourceDeletionRestoreAndFiles(t *testing.T) {
	m, root := managerFixture(t, 10)
	directory := filepath.Join(root, "src")
	if e := os.Mkdir(directory, 0700); e != nil {
		t.Fatal(e)
	}
	file := filepath.Join(directory, "current.txt")
	if e := os.WriteFile(file, []byte("before snapshot"), 0600); e != nil {
		t.Fatal(e)
	}
	no := false
	source, e := m.Create(t.Context(), &pb.CreateSessionRequest{ProjectId: "proj_fixture", Name: "original", WorkingDir: "src", Description: "session description", Tags: []string{"frontend", "work"}, Metadata: map[string]string{"color": "blue", "role": "frontend", "Token": "shiki_secret", "environment": "KEY=secret", "api_key": "secret", "language": "shiki_forbidden"}, AutoStartClaude: &no, RequestId: "source"})
	if e != nil {
		t.Fatal(e)
	}
	snapshot, e := m.CreateSnapshot(t.Context(), &pb.CreateSnapshotRequest{SessionId: source.Id, Description: "saved settings"})
	if e != nil {
		t.Fatal(e)
	}
	if snapshot.WorkingDir != "src" || snapshot.SchemaVersion != 1 || snapshot.SessionDescription != source.Description || snapshot.Description != "saved settings" || snapshot.GetAutoStartClaude() || !reflect.DeepEqual(snapshot.Metadata, map[string]string{"color": "blue", "role": "frontend"}) {
		t.Fatalf("wrong configuration snapshot: %v", snapshot)
	}
	encoded, e := json.Marshal(snapshot)
	if e != nil {
		t.Fatal(e)
	}
	for _, secret := range [][]byte{[]byte("shiki_secret"), []byte("KEY=secret"), []byte("api_key"), []byte("shiki_forbidden"), []byte("before snapshot")} {
		if bytes.Contains(encoded, secret) {
			t.Fatalf("snapshot retained forbidden content %q", secret)
		}
	}
	if e = os.WriteFile(file, []byte("current shared project contents"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = m.Delete(t.Context(), &pb.DeleteSessionRequest{Id: source.Id}); e != nil {
		t.Fatal(e)
	}
	if _, e = m.tmux.Inspect(t.Context(), source.Id); !errors.Is(e, tmux.ErrNotFound) {
		t.Fatalf("source tmux session retained: %v", e)
	}
	projects, e := project.New(m.store, root, "tester")
	if e != nil {
		t.Fatal(e)
	}
	defer projects.Close()
	if _, e = projects.Delete(t.Context(), &pb.DeleteProjectRequest{Id: "proj_fixture"}); connect.CodeOf(e) != connect.CodeFailedPrecondition {
		t.Fatalf("project with snapshot deleted: %v", e)
	}
	request := &pb.RestoreSnapshotRequest{SnapshotId: snapshot.Id, NewSessionName: "copy", RequestId: "restore-once"}
	type result struct {
		session *pb.Session
		err     error
	}
	results := make(chan result, 4)
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() { value, e := m.RestoreSnapshot(t.Context(), request); results <- result{value, e} })
	}
	wg.Wait()
	close(results)
	var restored *pb.Session
	for result := range results {
		if result.err != nil {
			t.Fatal(result.err)
		}
		if restored != nil && restored.Id != result.session.Id {
			t.Fatal("concurrent restores created duplicate sessions")
		}
		restored = result.session
	}
	if restored.Id == source.Id || restored.WorkingDir != directory || restored.Description != source.Description || !reflect.DeepEqual(restored.Tags, source.Tags) || restored.GetAutoStartClaude() {
		t.Fatalf("restore lost launch configuration: %v", restored)
	}
	if _, e = m.tmux.Inspect(t.Context(), restored.Id); e != nil {
		t.Fatalf("restore did not create a real tmux session: %v", e)
	}
	if data, e := os.ReadFile(file); e != nil || string(data) != "current shared project contents" {
		t.Fatalf("restore changed project files: %q %v", data, e)
	}
	if _, e = m.RestoreSnapshot(t.Context(), &pb.RestoreSnapshotRequest{SnapshotId: snapshot.Id, NewSessionName: "different", RequestId: request.RequestId}); connect.CodeOf(e) != connect.CodeInvalidArgument {
		t.Fatalf("restore request mismatch accepted: %v", e)
	}
	if _, e = m.Delete(t.Context(), &pb.DeleteSessionRequest{Id: restored.Id}); e != nil {
		t.Fatal(e)
	}
	if _, e = m.DeleteSnapshot(t.Context(), &pb.DeleteSnapshotRequest{SnapshotId: snapshot.Id}); e != nil {
		t.Fatal(e)
	}
	if _, e = m.DeleteSnapshot(t.Context(), &pb.DeleteSnapshotRequest{SnapshotId: snapshot.Id}); e != nil {
		t.Fatal("snapshot deletion not idempotent", e)
	}
	replay, e := m.RestoreSnapshot(t.Context(), request)
	if e != nil || replay.Id != restored.Id {
		t.Fatalf("completed restore depended on deleted source/snapshot: %v %v", replay, e)
	}
	if _, e = m.tmux.Inspect(t.Context(), restored.Id); !errors.Is(e, tmux.ErrNotFound) {
		t.Fatalf("replay resurrected deleted session: %v", e)
	}
	if _, e = projects.Delete(t.Context(), &pb.DeleteProjectRequest{Id: "proj_fixture"}); e != nil {
		t.Fatal(e)
	}
}
func TestSnapshotRestoreRevalidatesMissingAndEscapingDirectory(t *testing.T) {
	m, root := managerFixture(t, 10)
	directory := filepath.Join(root, "work")
	if e := os.Mkdir(directory, 0700); e != nil {
		t.Fatal(e)
	}
	no := false
	source, e := m.Create(t.Context(), &pb.CreateSessionRequest{ProjectId: "proj_fixture", Name: "source", WorkingDir: "work", AutoStartClaude: &no, RequestId: "source"})
	if e != nil {
		t.Fatal(e)
	}
	snapshot, e := m.CreateSnapshot(t.Context(), &pb.CreateSnapshotRequest{SessionId: source.Id})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = m.Delete(t.Context(), &pb.DeleteSessionRequest{Id: source.Id}); e != nil {
		t.Fatal(e)
	}
	if e = os.Remove(directory); e != nil {
		t.Fatal(e)
	}
	request := &pb.RestoreSnapshotRequest{SnapshotId: snapshot.Id, NewSessionName: "restored", RequestId: "missing"}
	if _, e = m.RestoreSnapshot(t.Context(), request); connect.CodeOf(e) != connect.CodeNotFound {
		t.Fatalf("missing directory: %v", e)
	}
	if e = os.Mkdir(directory, 0700); e != nil {
		t.Fatal(e)
	}
	if _, e = m.RestoreSnapshot(t.Context(), request); connect.CodeOf(e) != connect.CodeNotFound {
		t.Fatalf("failed restore changed outcome on retry: %v", e)
	}
	if e = os.Remove(directory); e != nil {
		t.Fatal(e)
	}
	outside := t.TempDir()
	if e = os.Symlink(outside, directory); e != nil {
		t.Fatal(e)
	}
	request.RequestId = "escape"
	if _, e = m.RestoreSnapshot(t.Context(), request); e == nil {
		t.Fatal("snapshot restored through an escaping symlink")
	}
	sessions, e := m.List(t.Context(), &pb.ListSessionsRequest{})
	if e != nil || len(sessions.Sessions) != 0 {
		t.Fatalf("failed restore created session: %v %v", sessions, e)
	}
}
func TestSnapshotPersistenceAndUnsupportedSchema(t *testing.T) {
	mux, e := tmux.New("")
	if e != nil {
		t.Skipf("tmux unavailable: %v", e)
	}
	dir := filepath.Join(t.TempDir(), "state")
	root, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	state, e := storage.Open(dir, storage.Options{Validate: storage.ValidateDaemon("tester")})
	if e != nil {
		t.Fatal(e)
	}
	if e = state.Update(func(s *storage.Snapshot) error {
		return s.Put(storage.ProjectsBucket, "proj_persist", &pb.Project{Id: "proj_persist", Name: "persistent", Path: root, User: "tester", CreatedAt: time.Now().Unix()})
	}); e != nil {
		t.Fatal(e)
	}
	m, e := NewManager(t.Context(), state, mux, ManagerOptions{User: "tester", ClaudeCommand: []string{"/bin/sleep", "30"}})
	if e != nil {
		t.Fatal(e)
	}
	no := false
	source, e := m.Create(t.Context(), &pb.CreateSessionRequest{ProjectId: "proj_persist", Name: "source", AutoStartClaude: &no, RequestId: "source"})
	if e != nil {
		t.Fatal(e)
	}
	snapshot, e := m.CreateSnapshot(t.Context(), &pb.CreateSnapshotRequest{SessionId: source.Id})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = m.Delete(t.Context(), &pb.DeleteSessionRequest{Id: source.Id}); e != nil {
		t.Fatal(e)
	}
	m.Close()
	state.Close()
	state, e = storage.Open(dir, storage.Options{Validate: storage.ValidateDaemon("tester")})
	if e != nil {
		t.Fatal(e)
	}
	defer state.Close()
	m, e = NewManager(t.Context(), state, mux, ManagerOptions{User: "tester", ClaudeCommand: []string{"/bin/sleep", "30"}})
	if e != nil {
		t.Fatal(e)
	}
	defer m.Close()
	list, e := m.ListSnapshots(t.Context(), &pb.ListSnapshotsRequest{SessionId: source.Id})
	if e != nil || len(list.Snapshots) != 1 || list.Snapshots[0].Id != snapshot.Id {
		t.Fatalf("snapshot did not persist: %v %v", list, e)
	}
	restored, e := m.RestoreSnapshot(t.Context(), &pb.RestoreSnapshotRequest{SnapshotId: snapshot.Id, NewSessionName: "restored", RequestId: "restore"})
	if e != nil {
		t.Fatal(e)
	}
	defer mux.Delete(context.Background(), restored.Id)
	// A deliberately unvalidated store models an unsupported snapshot received
	// from a future migration; the operation itself must still reject its schema.
	foreign, e := storage.Open(filepath.Join(t.TempDir(), "future"), storage.Options{})
	if e != nil {
		t.Fatal(e)
	}
	defer foreign.Close()
	future, e := NewManager(t.Context(), foreign, mux, ManagerOptions{User: "tester", ClaudeCommand: []string{"/bin/sleep", "30"}})
	if e != nil {
		t.Fatal(e)
	}
	defer future.Close()
	snapshot.SchemaVersion = 2
	if e = foreign.Update(func(s *storage.Snapshot) error { return s.Put(storage.SnapshotsBucket, snapshot.Id, snapshot) }); e != nil {
		t.Fatal(e)
	}
	if _, e = future.RestoreSnapshot(t.Context(), &pb.RestoreSnapshotRequest{SnapshotId: snapshot.Id, NewSessionName: "future", RequestId: "future"}); connect.CodeOf(e) != connect.CodeFailedPrecondition {
		t.Fatalf("unsupported schema accepted: %v", e)
	}
}
