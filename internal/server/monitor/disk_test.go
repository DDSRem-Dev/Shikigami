package monitor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
)

type diskSource struct {
	directories []string
	err         error
	requested   string
}

func (s *diskSource) SampleTargets(context.Context, string) ([]Target, error) { return nil, nil }
func (s *diskSource) DiskDirectories(_ context.Context, session string) ([]string, error) {
	s.requested = session
	return s.directories, s.err
}

// Missing project filesystems must not silently become a healthy state-volume
// measurement, including when no sessions (and therefore no sample PIDs) exist.
func TestDiskSnapshotRequiresEveryRegisteredFilesystem(t *testing.T) {
	state := t.TempDir()
	project := filepath.Join(t.TempDir(), "project")
	if err := os.Mkdir(project, 0700); err != nil {
		t.Fatal(err)
	}
	source := &diskSource{directories: []string{project}}
	m := New(source, state)
	check := func(available bool) {
		t.Helper()
		value, err := m.Snapshot(t.Context(), &pb.StatsRequest{SessionId: "sess_selected"})
		if err != nil {
			t.Fatal(err)
		}
		if source.requested != "sess_selected" || len(value.Sessions) != 0 {
			t.Fatal("disk selection was not independent of session process sampling")
		}
		if (value.DiskFreeBytes != nil) != available {
			t.Fatalf("disk availability=%v, wanted %v", value.DiskFreeBytes, available)
		}
		if value.MemoryUsedBytes == nil {
			t.Fatal("disk uncertainty discarded unrelated memory observation")
		}
	}
	check(true)
	if err := os.Remove(project); err != nil {
		t.Fatal(err)
	}
	check(false)
	if err := os.Mkdir(project, 0700); err != nil {
		t.Fatal(err)
	}
	check(true)
	if err := os.Remove(state); err != nil {
		t.Fatal(err)
	}
	check(false)
}

func TestDiskDirectoryEnumerationErrorIsNotEmptyInventory(t *testing.T) {
	want := errors.New("project inventory unavailable")
	m := New(&diskSource{err: want}, t.TempDir())
	if value, err := m.Snapshot(t.Context(), nil); !errors.Is(err, want) || value != nil {
		t.Fatalf("failed inventory became a partial snapshot: %v, %v", value, err)
	}
}
