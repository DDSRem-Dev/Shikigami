package project

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/server/storage"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
)

type fixture struct {
	manager        *Manager
	store          *storage.Store
	base, stateDir string
}

func setup(t *testing.T) fixture {
	t.Helper()
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state")
	base := filepath.Join(dir, "projects")
	store, err := storage.Open(stateDir, storage.Options{Validate: storage.ValidateDaemon("test-user")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	manager, err := New(store, base, "test-user")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { manager.Close() })
	return fixture{manager, store, base, stateDir}
}
func create(t *testing.T, f fixture, name string) *pb.Project {
	t.Helper()
	project, err := f.manager.Create(t.Context(), &pb.CreateProjectRequest{Name: name, Path: filepath.Join(f.base, name), RequestId: "create-" + name})
	if err != nil {
		t.Fatal(err)
	}
	return project
}

func TestConcurrentCreationIsDurableAndIdempotent(t *testing.T) {
	f := setup(t)
	request := &pb.CreateProjectRequest{Name: "app", Path: filepath.Join(f.base, "app"), RequestId: "same-request", Metadata: map[string]string{"language": "go"}}
	ids := make(chan string, 24)
	var wg sync.WaitGroup
	for range 24 {
		wg.Go(func() {
			project, err := f.manager.Create(t.Context(), request)
			if err != nil {
				t.Error(err)
				return
			}
			ids <- project.Id
		})
	}
	wg.Wait()
	close(ids)
	first := ""
	for id := range ids {
		if first == "" {
			first = id
		} else if id != first {
			t.Fatal("same request created multiple resources")
		}
	}
	list, err := f.manager.List(t.Context(), &pb.ListProjectsRequest{})
	if err != nil || len(list.Projects) != 1 {
		t.Fatalf("projects %+v, %v", list, err)
	}
	request.Metadata["language"] = "rust"
	if _, err = f.manager.Create(t.Context(), request); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("different parameters reused ID: %v", err)
	}
	stored, err := f.manager.Get(t.Context(), &pb.GetProjectRequest{Id: first})
	if err != nil || stored.Metadata["language"] != "go" {
		t.Fatal("caller map mutation changed persisted metadata")
	}
	if err = f.manager.Close(); err != nil {
		t.Fatal(err)
	}
	if err = f.store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := storage.Open(f.stateDir, storage.Options{Validate: storage.ValidateDaemon("test-user")})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	m, err := New(reopened, f.base, "test-user")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	request.Metadata["language"] = "go"
	result, err := m.Create(t.Context(), request)
	if err != nil || result.Id != first {
		t.Fatalf("restart lost request result: %+v %v", result, err)
	}
}

func TestNameAndRootUniquenessAndFailedRequestReplay(t *testing.T) {
	f := setup(t)
	project := create(t, f, "app")
	for _, request := range []*pb.CreateProjectRequest{
		{Name: "app", Path: filepath.Join(f.base, "other"), RequestId: "same-name"},
		{Name: "nested", Path: filepath.Join(project.Path, "nested"), RequestId: "nested"},
		{Name: "parent", Path: f.base, RequestId: "parent"},
	} {
		if _, err := f.manager.Create(t.Context(), request); connect.CodeOf(err) != connect.CodeAlreadyExists {
			t.Fatalf("overlap/name accepted: %v", err)
		}
	}
	if _, err := os.Stat(filepath.Join(project.Path, "nested")); !os.IsNotExist(err) {
		t.Fatal("rejected nested root created directories")
	}
	if _, err := f.manager.Delete(t.Context(), &pb.DeleteProjectRequest{Id: project.Id}); err != nil {
		t.Fatal(err)
	}
	// The prior failed result stays failed even after the conflicting project
	// is deleted. A caller must intentionally use a fresh request ID.
	if _, err := f.manager.Create(t.Context(), &pb.CreateProjectRequest{Name: "app", Path: filepath.Join(f.base, "other"), RequestId: "same-name"}); connect.CodeOf(err) != connect.CodeAlreadyExists {
		t.Fatalf("failed request was silently retried: %v", err)
	}
	if _, err := f.manager.Create(t.Context(), &pb.CreateProjectRequest{Name: "app", Path: filepath.Join(f.base, "other"), RequestId: "fresh"}); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteChecksRelationshipsAndPreservesFiles(t *testing.T) {
	f := setup(t)
	project := create(t, f, "app")
	file := filepath.Join(project.Path, "keep.txt")
	if err := os.WriteFile(file, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	session := &pb.Session{Id: "sess_one", Name: "main", ProjectId: project.Id, WorkingDir: project.Path, CreatedAt: time.Now().Unix(), Lifecycle: "creating"}
	if err := f.store.Update(func(state *storage.Snapshot) error { return state.Put(storage.SessionsBucket, session.Id, session) }); err != nil {
		t.Fatal(err)
	}
	if _, err := f.manager.Delete(t.Context(), &pb.DeleteProjectRequest{Id: project.Id}); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatal("deleted project with session", err)
	}
	got, err := f.manager.Get(t.Context(), &pb.GetProjectRequest{Id: project.Id})
	if err != nil || len(got.SessionIds) != 1 || got.SessionIds[0] != session.Id {
		t.Fatal("session IDs not derived")
	}
	state, _ := f.store.View()
	var stored pb.Project
	if err = state.Get(storage.ProjectsBucket, project.Id, &stored); err != nil || len(stored.SessionIds) != 0 {
		t.Fatal("derived IDs persisted")
	}
	snapshot := &pb.Snapshot{Id: "snap_one", SessionId: session.Id, ProjectId: project.Id, SchemaVersion: 1, CreatedAt: time.Now().Unix()}
	if err = f.store.Update(func(state *storage.Snapshot) error {
		delete(state.Buckets[storage.SessionsBucket], session.Id)
		return state.Put(storage.SnapshotsBucket, snapshot.Id, snapshot)
	}); err != nil {
		t.Fatal(err)
	}
	if _, err = f.manager.Delete(t.Context(), &pb.DeleteProjectRequest{Id: project.Id}); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatal("deleted project with snapshot", err)
	}
	if err = f.store.Update(func(state *storage.Snapshot) error {
		delete(state.Buckets[storage.SnapshotsBucket], snapshot.Id)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err = f.manager.Delete(t.Context(), &pb.DeleteProjectRequest{Id: project.Id}); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(file)
	if err != nil || string(data) != "keep" {
		t.Fatal("project deletion modified files")
	}
}

func TestExistingAssociationAndNewDirectoryConfinement(t *testing.T) {
	f := setup(t)
	existing := t.TempDir()
	marker := filepath.Join(existing, "keep")
	if err := os.WriteFile(marker, []byte("untouched"), 0600); err != nil {
		t.Fatal(err)
	}
	associated, err := f.manager.Create(t.Context(), &pb.CreateProjectRequest{Name: "existing", Path: existing, RequestId: "associate"})
	if err != nil {
		t.Fatal(err)
	}
	canonical, _ := filepath.EvalSymlinks(existing)
	if associated.Path != canonical {
		t.Fatal("existing directory not canonicalized")
	}
	data, _ := os.ReadFile(marker)
	if string(data) != "untouched" {
		t.Fatal("association changed existing data")
	}
	if _, err = f.manager.Create(t.Context(), &pb.CreateProjectRequest{Name: "outside", Path: f.base + "2/new", RequestId: "outside"}); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatal("textual base prefix accepted", err)
	}
	if err = os.Symlink(existing, filepath.Join(f.base, "escape")); err != nil {
		t.Fatal(err)
	}
	if _, err = f.manager.Create(t.Context(), &pb.CreateProjectRequest{Name: "escape", Path: filepath.Join(f.base, "escape", "new"), RequestId: "escape"}); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("symlink escaped creation base: %v", err)
	}
	if _, err = os.Stat(filepath.Join(existing, "new")); !os.IsNotExist(err) {
		t.Fatal("created outside root")
	}
	if err = os.Symlink(existing, filepath.Join(f.base, "alias")); err != nil {
		t.Fatal(err)
	}
	if _, err = f.manager.Create(t.Context(), &pb.CreateProjectRequest{Name: "alias", Path: filepath.Join(f.base, "alias"), RequestId: "alias"}); connect.CodeOf(err) != connect.CodeAlreadyExists {
		t.Fatal("same existing directory registered twice through alias", err)
	}
}

func TestPinnedBaseAndWorkingDirectoryRejectSymlinkEscape(t *testing.T) {
	f := setup(t)
	outside := t.TempDir()
	moved := f.base + "-moved"
	if err := os.Rename(f.base, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, f.base); err != nil {
		t.Fatal(err)
	}
	if _, err := f.manager.Create(t.Context(), &pb.CreateProjectRequest{Name: "changed-base", Path: filepath.Join(f.base, "new"), RequestId: "changed-base"}); err == nil {
		t.Fatal("replaced project base accepted")
	}
	if _, err := os.Stat(filepath.Join(outside, "new")); !os.IsNotExist(err) {
		t.Fatal("base replacement redirected directory creation")
	}
	projectDir := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(projectDir, "escape")); err != nil {
		t.Fatal(err)
	}
	if _, root, err := OpenWorkingDirectory(projectDir, filepath.Join(projectDir, "escape")); err == nil {
		root.Close()
		t.Fatal("working directory escaped through symlink")
	}
	if _, root, err := OpenWorkingDirectory(projectDir, projectDir+"2"); err == nil {
		root.Close()
		t.Fatal("working directory prefix confused")
	}
	child := filepath.Join(projectDir, "child")
	if err := os.Mkdir(child, 0700); err != nil {
		t.Fatal(err)
	}
	canonicalProject, _ := filepath.EvalSymlinks(projectDir)
	canonicalChild, _ := filepath.EvalSymlinks(child)
	got, root, err := OpenWorkingDirectory(canonicalProject, canonicalChild)
	if err != nil {
		t.Fatal(err)
	}
	root.Close()
	if got != canonicalChild {
		t.Fatal("wrong working directory")
	}
}

func TestPersistenceFailureCannotReportSuccessOrRemoveFiles(t *testing.T) {
	f := setup(t)
	project := create(t, f, "existing")
	marker := filepath.Join(project.Path, "keep")
	if err := os.WriteFile(marker, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(f.stateDir, "state.previous.json")
	if err := os.Remove(backup); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(backup, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := f.manager.Delete(t.Context(), &pb.DeleteProjectRequest{Id: project.Id}); connect.CodeOf(err) != connect.CodeUnavailable {
		t.Fatalf("failed commit reported success: %v", err)
	}
	if _, err := f.manager.Get(t.Context(), &pb.GetProjectRequest{Id: project.Id}); err != nil {
		t.Fatal("failed delete mutated in-memory state")
	}
	if _, err := f.manager.Create(t.Context(), &pb.CreateProjectRequest{Name: "uncommitted", Path: filepath.Join(f.base, "uncommitted"), RequestId: "uncommitted"}); connect.CodeOf(err) != connect.CodeUnavailable {
		t.Fatalf("failed create commit reported success: %v", err)
	}
	list, err := f.manager.List(t.Context(), nil)
	if err != nil || len(list.Projects) != 1 {
		t.Fatal("failed create persisted partial metadata")
	}
	data, err := os.ReadFile(marker)
	if err != nil || string(data) != "keep" {
		t.Fatal("storage failure changed project files")
	}
	// Do not roll back directories after uncertain commits: another process
	// may have written files there. They remain available for explicit adoption.
	if info, err := os.Stat(filepath.Join(f.base, "uncommitted")); err != nil || !info.IsDir() {
		t.Fatal("failed persistence removed project directory")
	}
}

func TestPermissionsCancellationAndUserFilter(t *testing.T) {
	f := setup(t)
	if _, err := f.manager.List(t.Context(), &pb.ListProjectsRequest{User: "root"}); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatal("foreign user filter accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := f.manager.Create(ctx, &pb.CreateProjectRequest{Name: "cancel", Path: filepath.Join(f.base, "cancel"), RequestId: "cancel"}); err == nil {
		t.Fatal("canceled create succeeded")
	}
	if os.Geteuid() != 0 {
		locked := filepath.Join(f.base, "locked")
		if err := os.Mkdir(locked, 0500); err != nil {
			t.Fatal(err)
		}
		defer os.Chmod(locked, 0700)
		if _, err := f.manager.Create(t.Context(), &pb.CreateProjectRequest{Name: "locked", Path: locked, RequestId: "locked"}); connect.CodeOf(err) != connect.CodePermissionDenied {
			t.Fatalf("unwritable directory accepted: %v", err)
		}
	}
	if _, err := f.manager.Create(t.Context(), &pb.CreateProjectRequest{Name: "valid", Path: filepath.Join(f.base, "valid"), RequestId: strings.Repeat("x", 257)}); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatal("unbounded request ID accepted")
	}
}
