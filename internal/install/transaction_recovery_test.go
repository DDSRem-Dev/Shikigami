package install

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// Observe the actual link and receipt a restarted service would use, rather
// than inferring the selected executable from the requested recovery version.
type selectionObservingLifecycle struct {
	fakeLifecycle
	artifacts     *Artifacts
	starts        []Selection
	startAttempts int
}

func (f *selectionObservingLifecycle) Start(ctx context.Context, plan Plan) error {
	f.startAttempts++
	current, err := f.artifacts.Current()
	if err != nil {
		return err
	}
	receipt, err := f.artifacts.Receipt()
	if err != nil {
		return err
	}
	f.starts = append(f.starts, Selection{Release: current, Version: receipt.Active.Version, Checksums: receipt.Active.Checksums})
	return f.fakeLifecycle.Start(ctx, plan)
}

func TestRollbackBackupFailureRequiresReadableVerifiedCurrent(t *testing.T) {
	for _, fault := range []string{"unreadable", "unknown", "missing", "corrupt"} {
		t.Run(fault, func(t *testing.T) {
			a := testArtifacts(t)
			plan := testPlan(t)
			f := &selectionObservingLifecycle{fakeLifecycle: fakeLifecycle{prefix: a.Prefix}, artifacts: a}
			for _, version := range []string{"v1", "v2"} {
				if _, err := a.Install(context.Background(), bundleFixture(t, version), plan, false, f); err != nil {
					t.Fatal(err)
				}
			}
			receipt, err := a.Receipt()
			if err != nil {
				t.Fatal(err)
			}
			backupFailure := errors.New("injected backup failure with unknown selection")
			f.backupHook = func() error {
				var err error
				switch fault {
				case "unreadable":
					err = a.root.Close()
				case "unknown", "missing":
					err = os.Remove(filepath.Join(a.Prefix, "current"))
					if err == nil && fault == "unknown" {
						err = os.Symlink("unknown-selection", filepath.Join(a.Prefix, "current"))
					}
				case "corrupt":
					err = os.WriteFile(filepath.Join(a.Prefix, receipt.Active.Release, "shiki-agent"), []byte("changed executable"), 0700)
				}
				if err != nil {
					t.Fatal(err)
				}
				return backupFailure
			}
			f.startAttempts = 0
			if _, err := a.Rollback(context.Background(), f); !errors.Is(err, backupFailure) {
				t.Fatal(err)
			}
			if f.startAttempts != 0 {
				t.Fatalf("started service without a readable verified committed selection: %d attempts", f.startAttempts)
			}
		})
	}
}

func TestRollbackBackupFailureDoesNotRestartUncommittedSelection(t *testing.T) {
	for _, action := range []string{"install", "rollback"} {
		t.Run(action, func(t *testing.T) {
			ctx := context.Background()
			a := testArtifacts(t)
			plan := testPlan(t)
			first, second := bundleFixture(t, "v1"), bundleFixture(t, "v2")
			f := &selectionObservingLifecycle{fakeLifecycle: fakeLifecycle{prefix: a.Prefix}, artifacts: a}
			if _, err := a.Install(ctx, first, plan, false, f); err != nil {
				t.Fatal(err)
			}
			if _, err := a.Stage(second); err != nil {
				t.Fatal(err)
			}
			receipt, err := a.Receipt()
			if err != nil {
				t.Fatal(err)
			}
			next := Selection{Release: releaseName(second.Manifest), Version: second.Manifest.Version, Checksums: second.TrustedChecksums}
			receipt.Pending, receipt.PendingAction = &next, action
			if err := a.writeJSON("installation.json", receipt); err != nil {
				t.Fatal(err)
			}
			if err := a.switchTo(next.Release); err != nil {
				t.Fatal(err)
			}
			privateState := filepath.Join(plan.StateDir, "auth-state")
			if err := os.WriteFile(privateState, []byte("current revocations must survive"), 0600); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(filepath.Join(a.Prefix, "installation.json"))
			if err != nil {
				t.Fatal(err)
			}
			backupFailure := errors.New("injected backup storage failure")
			f.backupHook = func() error { return backupFailure }
			f.starts, f.events = nil, nil
			result, err := a.Rollback(ctx, f)
			if result != nil || !errors.Is(err, backupFailure) {
				t.Fatalf("failed backup was not preserved: result=%v err=%v", result, err)
			}
			if len(f.starts) != 0 {
				t.Errorf("backup failure restarted an uncommitted executable: starts=%+v active=%+v", f.starts, receipt.Active)
			}
			if !reflect.DeepEqual(f.events, []string{"stop", "backup"}) {
				t.Errorf("failure performed activation work: %v", f.events)
			}
			after, err := os.ReadFile(filepath.Join(a.Prefix, "installation.json"))
			if err != nil || string(after) != string(before) {
				t.Errorf("pending receipt changed after backup failure: %v", err)
			}
			if current, err := a.Current(); err != nil || current != next.Release {
				t.Errorf("failed backup silently changed selection: %q %v", current, err)
			}
			if state, err := os.ReadFile(privateState); err != nil || string(state) != "current revocations must survive" {
				t.Errorf("failure restored or damaged private state: %q %v", state, err)
			}
			f.backupHook = nil
			f.starts, f.events = nil, nil
			if _, err := a.Rollback(ctx, f); err != nil {
				t.Fatal("explicit retry did not recover:", err)
			}
			if len(f.starts) != 1 || f.starts[0].Release != receipt.Active.Release {
				t.Errorf("explicit retry did not start retained committed release: %+v", f.starts)
			}
		})
	}
}

func TestRollbackBackupFailureRestartsMatchingCommittedSelection(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := testArtifacts(t)
	plan := testPlan(t)
	f := &selectionObservingLifecycle{fakeLifecycle: fakeLifecycle{prefix: a.Prefix}, artifacts: a}
	for _, version := range []string{"v1", "v2"} {
		if _, err := a.Install(ctx, bundleFixture(t, version), plan, false, f); err != nil {
			t.Fatal(err)
		}
	}
	receipt, err := a.Receipt()
	if err != nil {
		t.Fatal(err)
	}
	backupFailure := errors.New("injected backup cancellation")
	f.backupHook = func() error { cancel(); return backupFailure }
	f.starts, f.events = nil, nil
	if _, err := a.Rollback(ctx, f); !errors.Is(err, backupFailure) {
		t.Fatal(err)
	}
	if len(f.starts) != 1 || f.starts[0] != receipt.Active || f.startContextErr != nil {
		t.Fatalf("committed release not recovered with independent context: starts=%+v context=%v", f.starts, f.startContextErr)
	}
}
