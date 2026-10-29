package session

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/server/storage"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
)

func TestDiskDirectoriesIncludeProjectsWithoutSessionsAndRespectSelection(t *testing.T) {
	store, err := storage.Open(filepath.Join(t.TempDir(), "state"), storage.Options{Validate: storage.ValidateDaemon("tester")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})
	roots := []string{t.TempDir(), t.TempDir()}
	now := time.Now().Unix()
	if err := store.Update(func(state *storage.Snapshot) error {
		for i, id := range []string{"proj_first", "proj_second"} {
			if err := state.Put(storage.ProjectsBucket, id, &pb.Project{Id: id, Name: id, Path: roots[i], User: "tester", CreatedAt: now}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// This query must never need tmux or a live process to discover projects.
	m := &Manager{store: store}
	all, err := m.DiskDirectories(t.Context(), "")
	slices.Sort(all)
	want := slices.Clone(roots)
	slices.Sort(want)
	if err != nil || !slices.Equal(all, want) {
		t.Fatalf("projects with no sessions: %v, %v", all, err)
	}
	if err := store.Update(func(state *storage.Snapshot) error {
		return state.Put(storage.SessionsBucket, "sess_selected", &pb.Session{Id: "sess_selected", ProjectId: "proj_second", Name: "selected", WorkingDir: roots[1], CreatedAt: now, Lifecycle: "ready"})
	}); err != nil {
		t.Fatal(err)
	}
	selected, err := m.DiskDirectories(t.Context(), "sess_selected")
	if err != nil || !slices.Equal(selected, []string{roots[1]}) {
		t.Fatalf("selected session returned unrelated project roots: %v, %v", selected, err)
	}
	if _, err := m.DiskDirectories(t.Context(), "sess_missing"); connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("missing session silently sampled all projects: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := m.DiskDirectories(ctx, ""); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
