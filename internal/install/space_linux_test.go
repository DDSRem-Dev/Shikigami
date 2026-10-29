//go:build linux

package install

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

// Only the dedicated runner supplies a small tmpfs. Never fill an ordinary disk.
func TestInstallSpaceRejectsBeforeStoppingExistingService(t *testing.T) {
	base := os.Getenv("SHIKI_INSTALL_SPACE_TMPFS")
	if base == "" {
		t.Skip("requires isolated installer-space runner")
	}
	var fs unix.Statfs_t
	if os.Geteuid() == 0 || unix.Statfs(base, &fs) != nil || fs.Type != unix.TMPFS_MAGIC || fs.Blocks*uint64(fs.Bsize) > 32<<20 {
		t.Fatal("requires ordinary user and dedicated <=32MiB tmpfs")
	}
	prefix := filepath.Join(base, "install")
	a, err := OpenArtifacts(prefix)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	t.Cleanup(func() { os.RemoveAll(prefix) })
	plan := testPlan(t)
	if err = os.Chmod(plan.StateDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(plan.Config, []byte("private config"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(plan.StateDir, "data"), bytes.Repeat([]byte("x"), 4<<20), 0600); err != nil {
		t.Fatal(err)
	}
	first, second := bundleFixture(t, "space-a"), bundleFixture(t, "space-b")
	// Make the staged payload larger than the later free-space window.
	if err = os.WriteFile(filepath.Join(second.Directory, "shiki"), bytes.Repeat([]byte("b"), 2<<20), 0700); err != nil {
		t.Fatal(err)
	}
	pin, err := CreateManifest(second.Directory, "space-b", runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Fatal(err)
	}
	second, err = Verify(second.Directory, "space-b", pin)
	if err != nil {
		t.Fatal(err)
	}

	initial := &fakeLifecycle{prefix: prefix}
	if _, err = a.Install(t.Context(), first, plan, false, initial); err != nil {
		t.Fatal(err)
	}
	// The second release is already staged: its complete payload must not be
	// counted again, but the actual pending backup must still fit.
	if _, err = a.Stage(second); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(prefix, "installation.json"))
	if err != nil {
		t.Fatal(err)
	}
	current, err := a.Current()
	if err != nil {
		t.Fatal(err)
	}
	filler, err := os.Create(filepath.Join(base, "bounded-filler"))
	if err != nil {
		t.Fatal(err)
	}
	defer filler.Close()
	t.Cleanup(func() { os.Remove(filler.Name()) })
	block := make([]byte, 64<<10)
	for written := 0; written < 32<<20; written += len(block) {
		if err = unix.Statfs(base, &fs); err != nil {
			t.Fatal(err)
		}
		if fs.Bavail*uint64(fs.Bsize) <= 1<<20 {
			break
		}
		if _, err = filler.Write(block); err != nil {
			t.Fatal(err)
		}
	}
	// Reusing a verified staged release needs no second payload allocation.
	if _, err = a.Stage(second); err != nil {
		t.Fatal("staged release falsely required fresh space", err)
	}
	f := &spaceObservedLifecycle{Native: &Native{}, fake: &fakeLifecycle{prefix: prefix}}
	if result, repeatErr := a.Install(t.Context(), first, plan, false, f); repeatErr != nil || result.State != "unchanged" {
		t.Fatal("unchanged install required backup space", repeatErr)
	}

	_, err = a.Install(t.Context(), second, plan, false, f)
	if len(f.fake.events) != 0 {
		t.Fatalf("low space stopped existing service: events=%v error=%v", f.fake.events, err)
	}
	if !errors.Is(err, syscall.ENOSPC) {
		t.Fatalf("expected low-space failure, got %v", err)
	}
	after, readErr := os.ReadFile(filepath.Join(prefix, "installation.json"))
	if readErr != nil || !bytes.Equal(before, after) {
		t.Fatal("space rejection changed receipt", readErr)
	}
	selected, readErr := a.Current()
	if readErr != nil || selected != current {
		t.Fatal("space rejection changed current", readErr)
	}
	if !reflect.DeepEqual(initial.events, []string{"stop", "backup", "prepare", "start", "probe"}) {
		t.Fatal(initial.events)
	}
	preflightErr := err
	third := bundleFixture(t, "space-c")
	if err = os.WriteFile(filepath.Join(third.Directory, "shiki"), bytes.Repeat([]byte("c"), 2<<20), 0700); err != nil {
		t.Fatal(err)
	}
	pin, err = CreateManifest(third.Directory, "space-c", runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Fatal(err)
	}
	third, err = Verify(third.Directory, "space-c", pin)
	if err != nil {
		t.Fatal(err)
	}
	if _, stageErr := a.Stage(third); !errors.Is(stageErr, syscall.ENOSPC) || !strings.Contains(stageErr.Error(), "insufficient installation space") {
		t.Fatal("new payload did not fail before staging", stageErr)
	}
	if _, statErr := os.Stat(filepath.Join(prefix, releaseName(third.Manifest))); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatal("space rejection installed release", statErr)
	}

	// The estimate is not a reservation: a shutdown flush may need more than
	// the live snapshot. Re-enumerate after Stop and restore the old service.
	if err = os.Remove(filepath.Join(plan.StateDir, "data")); err != nil {
		t.Fatal(err)
	}
	f.afterStop = func() error {
		return os.WriteFile(filepath.Join(plan.StateDir, "shutdown-flush"), bytes.Repeat([]byte("y"), 4<<20), 0600)
	}
	_, lateErr := a.Install(t.Context(), second, plan, false, f)
	if !errors.Is(lateErr, syscall.ENOSPC) || !reflect.DeepEqual(f.fake.events, []string{"stop", "start"}) {
		t.Fatalf("late space loss did not restore service: %v %v", f.fake.events, lateErr)
	}
	after, readErr = os.ReadFile(filepath.Join(prefix, "installation.json"))
	if readErr != nil || !bytes.Equal(before, after) {
		t.Fatal("late space failure changed receipt", readErr)
	}
	selected, readErr = a.Current()
	if readErr != nil || selected != current {
		t.Fatal("late space failure changed current", readErr)
	}
	t.Logf("shutdown-flush increase rechecked after Stop and restarted old service: %v", lateErr)
	t.Logf("ordinary-user tmpfs space rejection preserved current/receipt and called no lifecycle operations: %v", preflightErr)
}

type spaceObservedLifecycle struct {
	*Native
	fake      *fakeLifecycle
	afterStop func() error
}

func (s *spaceObservedLifecycle) Stop(ctx context.Context, p Plan) error {
	if err := s.fake.Stop(ctx, p); err != nil {
		return err
	}
	if s.afterStop != nil {
		return s.afterStop()
	}
	return nil
}
func (s *spaceObservedLifecycle) Start(ctx context.Context, p Plan) error {
	return s.fake.Start(ctx, p)
}
func (s *spaceObservedLifecycle) Prepare(ctx context.Context, p Plan, r string) error {
	return s.fake.Prepare(ctx, p, r)
}
func (s *spaceObservedLifecycle) Probe(ctx context.Context, p Plan) (string, error) {
	return s.fake.Probe(ctx, p)
}
func (s *spaceObservedLifecycle) RemoveService(ctx context.Context, p Plan) error {
	return s.fake.RemoveService(ctx, p)
}
