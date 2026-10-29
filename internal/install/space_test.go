package install

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestBackupInventoryMatchesArchiveAndExcludesProjectAndLinks(t *testing.T) {
	prefix, state := t.TempDir(), t.TempDir()
	plan := Plan{StateDir: state, Config: filepath.Join(t.TempDir(), "config"), ProjectsDir: filepath.Join(state, "projects")}
	if err := os.WriteFile(plan.Config, []byte("configuration"), 0600); err != nil {
		t.Fatal(err)
	}
	long := strings.Repeat("l", 180)
	for name, data := range map[string][]byte{"normal": []byte("state"), long: bytes.Repeat([]byte("x"), 513)} {
		if err := os.WriteFile(filepath.Join(state, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(plan.ProjectsDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(plan.ProjectsDir, "excluded"), []byte("project"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(plan.Config, filepath.Join(state, "link")); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mkfifo(filepath.Join(state, "fifo"), 0600); err != nil {
		t.Fatal(err)
	}
	n := &Native{}
	inventory, err := n.collectBackup(t.Context(), plan, false)
	if err != nil {
		t.Fatal(err)
	}
	size := inventory.bytes
	inventory.Close()
	archive, err := n.Backup(t.Context(), plan, prefix)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(archive)
	if err != nil || uint64(info.Size()) != size {
		t.Fatalf("estimate %d actual %v error %v", size, info, err)
	}
	entries := readBackup(t, archive)
	if string(entries["state/normal"]) != "state" || len(entries["state/"+long]) != 513 {
		t.Fatal("state lost", entries)
	}
	for name := range entries {
		if strings.Contains(name, "projects") || strings.Contains(name, "link") || strings.Contains(name, "fifo") {
			t.Fatal("unsafe backup entry", name)
		}
	}
}

func TestBackupReobservesStateAfterLivePreflight(t *testing.T) {
	prefix, state := t.TempDir(), t.TempDir()
	plan := Plan{StateDir: state, Config: filepath.Join(t.TempDir(), "absent-config")}
	if err := os.WriteFile(filepath.Join(state, "existing"), []byte("before"), 0600); err != nil {
		t.Fatal(err)
	}
	n := &Native{}
	if err := os.Chmod(prefix, 0700); err != nil {
		t.Fatal(err)
	}
	artifacts, err := OpenArtifacts(prefix)
	if err != nil {
		t.Fatal(err)
	}
	defer artifacts.Close()
	if err := n.Preflight(t.Context(), plan, artifacts, nil); err != nil {
		t.Fatal(err)
	}
	// A normal service shutdown flushes state after the live estimate.
	if err := os.WriteFile(filepath.Join(state, "existing"), []byte("after-shutdown-larger"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "new"), []byte("final-flush"), 0600); err != nil {
		t.Fatal(err)
	}
	archive, err := n.Backup(t.Context(), plan, prefix)
	if err != nil {
		t.Fatal(err)
	}
	entries := readBackup(t, archive)
	if string(entries["state/existing"]) != "after-shutdown-larger" || string(entries["state/new"]) != "final-flush" {
		t.Fatal("stale live inventory used for backup", entries)
	}
}

func TestBackupInventoryLimitsCancellationAndRootSymlink(t *testing.T) {
	state := t.TempDir()
	plan := Plan{StateDir: state}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	n := &Native{}
	if _, err := n.collectBackup(ctx, plan, false); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled backup inventory", err)
	}
	f, err := os.Create(filepath.Join(state, "oversized"))
	if err != nil {
		t.Fatal(err)
	}
	err = f.Truncate(maxBackupBytes + 1)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = n.collectBackup(t.Context(), plan, false); err == nil || !strings.Contains(err.Error(), "8 GiB") {
		t.Fatal("unbounded backup size", err)
	}
	link := filepath.Join(t.TempDir(), "linked-state")
	if err = os.Symlink(state, link); err != nil {
		t.Fatal(err)
	}
	plan.StateDir = link
	if _, err = n.collectBackup(t.Context(), plan, false); err == nil {
		t.Fatal("backup followed symlink root")
	}
}

func TestSpaceObservationErrorsAndArithmeticFailClosed(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "regular")
	if err := os.WriteFile(file, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := availableSpace(file); err == nil {
		t.Fatal("regular target directory accepted")
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}
	if _, err := availableSpace(link); err == nil {
		t.Fatal("symlink destination accepted")
	}
	parent, err := availableSpace(dir)
	if err != nil {
		t.Fatal(err)
	}
	missing, err := availableSpace(filepath.Join(dir, "not-created", "child"))
	if err != nil || missing.device != parent.device {
		t.Fatal("wrong target filesystem", missing, err)
	}
	if _, err = addSpace(math.MaxUint64, 1); err == nil {
		t.Fatal("addition overflow accepted")
	}
	if _, err = multiplySpace(math.MaxUint64, 2); err == nil {
		t.Fatal("filesystem overflow accepted")
	}
	if err = checkSpace(t.Context(), []spaceNeed{{path: dir, bytes: math.MaxUint64}}); err == nil {
		t.Fatal("block-rounded overflow accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err = checkSpace(ctx, []spaceNeed{{path: dir, bytes: 1}}); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled space observation", err)
	}
}

func readBackup(t *testing.T, path string) map[string][]byte {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	r := tar.NewReader(f)
	result := map[string][]byte{}
	for {
		h, err := r.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if h.Typeflag != tar.TypeReg {
			continue
		}
		data, err := io.ReadAll(r)
		if err != nil {
			t.Fatal(err)
		}
		result[h.Name] = data
	}
	return result
}

func TestSpaceObservationUsesPinnedArtifactDirectory(t *testing.T) {
	a := testArtifacts(t)
	original := a.Prefix + "-original"
	if err := os.Rename(a.Prefix, original); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), a.Prefix); err != nil {
		t.Fatal(err)
	}
	needs, err := a.stageNeeds(bundleFixture(t, "pinned"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = availableSpace(a.Prefix); err == nil {
		t.Fatal("replacement path unexpectedly accepted")
	}
	expected, err := availableSpace(original)
	if err != nil {
		t.Fatal(err)
	}
	for _, need := range needs {
		actual, err := need.observe()
		if err != nil || actual.device != expected.device {
			t.Fatal("stage measured replaced path instead of fixed directory", actual, err)
		}
	}
}

func TestBackupRejectsDestinationInsideStateBeforeMutation(t *testing.T) {
	state := t.TempDir()
	prefix := filepath.Join(state, "install")
	a, err := OpenArtifacts(prefix)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	plan := Plan{StateDir: state}
	native := &Native{}
	if err = native.Preflight(t.Context(), plan, a, nil); err == nil {
		t.Fatal("live preflight accepted archive inside source")
	}
	if _, err = native.Backup(t.Context(), plan, prefix); err == nil {
		t.Fatal("backup accepted archive inside source")
	}
	if _, err = os.Stat(filepath.Join(prefix, "backups")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("unsafe backup mutated destination", err)
	}
	alias := filepath.Join(t.TempDir(), "state-alias")
	if err = os.Symlink(state, alias); err != nil {
		t.Fatal(err)
	}
	plan.StateDir = alias
	if err = validateBackupDestination(plan, prefix); err == nil {
		t.Fatal("alias bypassed archive/source containment")
	}
}
