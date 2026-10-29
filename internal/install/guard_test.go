package install

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/DDSRem-Dev/Shikigami/internal/client"
	"github.com/DDSRem-Dev/Shikigami/pkg/config"
)

func TestInstallGuardExcludesCLIStartupAcrossBackup(t *testing.T) {
	// Keep the UDS path within the platform limit even on macOS test runners.
	directory, err := os.MkdirTemp("/tmp", "shiki-guard-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(directory) })
	plan := Plan{Role: "client", StateDir: filepath.Join(directory, "state"), Config: filepath.Join(directory, "client.yaml")}
	cfg := config.DefaultClient()
	cfg.Agent.StateDir = plan.StateDir
	cfg.Agent.Socket = filepath.Join(directory, "missing.sock")
	if err = config.SaveClient(plan.Config, cfg); err != nil {
		t.Fatal(err)
	}
	native := &Native{}
	release, err := native.Acquire(t.Context(), plan)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	opts := client.Options{AutoStart: true, ExplicitStart: true, Timeout: time.Second, AgentBinary: filepath.Join(directory, "must-not-exist")}
	assertBlocked := func() {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
		defer cancel()
		frontend, err := client.Open(ctx, plan.Config, opts)
		if frontend != nil {
			frontend.Close()
		}
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("CLI attempted startup during installation: %v", err)
		}
	}
	assertBlocked()
	if _, err = native.Backup(t.Context(), plan, directory); err != nil {
		t.Fatalf("backup rejected the installer's own startup guard: %v", err)
	}
	assertBlocked()
	// Backup must release host instance/state locks before managed startup,
	// but it must not release the installer-owned startup guard.
	instance, err := os.OpenFile(filepath.Join(plan.StateDir, "instance.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if err = syscall.Flock(int(instance.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatalf("host cannot start after backup: %v", err)
	}
	if _, err = native.Backup(t.Context(), plan, directory); err == nil {
		t.Fatal("startup guard caused backup to ignore another live writer")
	}
	if err = syscall.Flock(int(instance.Fd()), syscall.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	release()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	frontend, err := client.Open(ctx, plan.Config, opts)
	if frontend != nil {
		frontend.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "cannot start shiki-agent") {
		t.Fatalf("CLI did not regain startup ownership after guard release: %v", err)
	}
}

func TestInstallGuardWaitCancelsAndRejectsUnsafeLock(t *testing.T) {
	plan := Plan{Role: "client", StateDir: filepath.Join(t.TempDir(), "state")}
	first := &Native{}
	release, err := first.Acquire(t.Context(), plan)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	second := &Native{}
	ctx, cancel := context.WithTimeout(t.Context(), 75*time.Millisecond)
	defer cancel()
	if unlock, err := second.Acquire(ctx, plan); !errors.Is(err, context.DeadlineExceeded) {
		if unlock != nil {
			unlock()
		}
		t.Fatalf("contended acquisition did not cancel: %v", err)
	}
	release()
	unlock, err := second.Acquire(t.Context(), plan)
	if err != nil {
		t.Fatal(err)
	}
	unlock()
	lockPath := filepath.Join(plan.StateDir, "start.lock")
	if err = os.Remove(lockPath); err != nil {
		t.Fatal(err)
	}
	if err = syscall.Mkfifo(lockPath, 0600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		unlock, err := second.Acquire(t.Context(), plan)
		if unlock != nil {
			unlock()
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("FIFO accepted as startup lock")
		}
	case <-time.After(time.Second):
		t.Fatal("FIFO blocked startup guard")
	}
}

func TestBackupDoesNotSkipReplacementStartupLock(t *testing.T) {
	directory := t.TempDir()
	plan := Plan{Role: "client", StateDir: filepath.Join(directory, "state"), Config: filepath.Join(directory, "absent.yaml")}
	native := &Native{}
	release, err := native.Acquire(t.Context(), plan)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	path := filepath.Join(plan.StateDir, "start.lock")
	if err = os.Rename(path, filepath.Join(plan.StateDir, "retained-lock")); err != nil {
		t.Fatal(err)
	}
	replacement, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer replacement.Close()
	if err = syscall.Flock(int(replacement.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	if _, err = native.Backup(t.Context(), plan, directory); err == nil {
		t.Fatal("backup skipped a replacement lock owned by another writer")
	}
}
