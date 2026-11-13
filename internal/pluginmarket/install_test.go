package pluginmarket

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func transactionStore(t *testing.T, base string) *InstallStore {
	t.Helper()
	store, err := OpenInstallStore(t.Context(), base)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func transactionPackage(t *testing.T, base, name, content string) {
	t.Helper()
	if err := os.Mkdir(filepath.Join(base, name), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, name, "marker"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestInterruptedPluginPublicationRecovery(t *testing.T) {
	for _, interrupted := range []string{"prepared", "old-moved", "new-moved", "committed", "rollback-new-moved", "rollback-old-restored"} {
		t.Run(interrupted, func(t *testing.T) {
			base := filepath.Join(t.TempDir(), "plugins")
			store := transactionStore(t, base)
			transactionPackage(t, base, "weather", "old-good")
			transactionPackage(t, base, ".plugin-stage-fixture", "new-good")
			tx, err := store.Begin("weather", "update", ".plugin-stage-fixture", true, "old-revision")
			if err != nil {
				t.Fatal(err)
			}
			if interrupted != "prepared" {
				if err := store.rename(tx.ID, tx.Backup); err != nil {
					t.Fatal(err)
				}
			}
			if interrupted != "prepared" && interrupted != "old-moved" {
				if err := store.rename(tx.Stage, tx.ID); err != nil {
					t.Fatal(err)
				}
			}
			if interrupted == "committed" {
				tx.Phase = "committed"
				if err := store.save(tx); err != nil {
					t.Fatal(err)
				}
			}
			if interrupted == "rollback-new-moved" || interrupted == "rollback-old-restored" {
				if err := store.rename(tx.ID, tx.Stage); err != nil {
					t.Fatal(err)
				}
			}
			if interrupted == "rollback-old-restored" {
				if err := store.rename(tx.Backup, tx.ID); err != nil {
					t.Fatal(err)
				}
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			store = transactionStore(t, base)
			defer store.Close()
			tx, err = store.Pending()
			if err != nil || tx == nil || !tx.RestoreEnabled || tx.RuntimeRevision != "old-revision" {
				t.Fatalf("lost recovery/runtime intent: %#v %v", tx, err)
			}
			if err := store.RecoverPublication(tx); err != nil {
				t.Fatal(err)
			}
			want := "old-good"
			if interrupted == "committed" {
				want = "new-good"
				backup, err := os.ReadFile(filepath.Join(base, tx.Backup, "marker"))
				if err != nil || string(backup) != "old-good" {
					t.Fatalf("prior package lost before runtime completion: %q %v", backup, err)
				}
			}
			content, err := os.ReadFile(filepath.Join(base, "weather", "marker"))
			if err != nil || string(content) != want {
				t.Fatalf("recovery produced %q, want %q: %v", content, want, err)
			}
			if err := store.RecoverPublication(tx); err != nil {
				t.Fatalf("recovery not idempotent: %v", err)
			}
			if err := store.Finish(tx); err != nil {
				t.Fatal(err)
			}
			if pending, err := store.Pending(); err != nil || pending != nil {
				t.Fatalf("finished transaction remains: %#v %v", pending, err)
			}
		})
	}
}

func TestUninstallAndActivationRollbackRetainPriorPackage(t *testing.T) {
	for _, operation := range []string{"uninstall", "update"} {
		t.Run(operation, func(t *testing.T) {
			base := filepath.Join(t.TempDir(), "plugins")
			store := transactionStore(t, base)
			defer store.Close()
			transactionPackage(t, base, "weather", "old-good")
			stage := ""
			if operation == "update" {
				stage = ".plugin-stage-fixture"
				transactionPackage(t, base, stage, "bad-new")
			}
			tx, err := store.Begin("weather", operation, stage, true, "revision")
			if err != nil {
				t.Fatal(err)
			}
			if err := store.Publish(tx); err != nil {
				t.Fatal(err)
			}
			backup, err := os.ReadFile(filepath.Join(base, tx.Backup, "marker"))
			if err != nil || string(backup) != "old-good" {
				t.Fatalf("backup not retained: %q %v", backup, err)
			}
			if operation == "update" {
				if err := store.Rollback(tx); err != nil {
					t.Fatal(err)
				}
				content, err := os.ReadFile(filepath.Join(base, "weather", "marker"))
				if err != nil || string(content) != "old-good" {
					t.Fatalf("failed activation lost previous package: %q %v", content, err)
				}
			} else if _, err := os.Stat(filepath.Join(base, "weather")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("committed uninstall still published: %v", err)
			}
			if err := store.Finish(tx); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestPluginTransactionRefusesCorruptJournal(t *testing.T) {
	base := filepath.Join(t.TempDir(), "plugins")
	store := transactionStore(t, base)
	defer store.Close()
	transactionPackage(t, base, "weather", "old-good")
	for _, data := range []string{
		`{"format":9,"id":"weather","operation":"update","phase":"prepared","stage":".plugin-stage-fixture","backup":".plugin-backup-fixture","restore_enabled":true,"runtime_revision":"r"}`,
		`{"format":1,"id":"weather","operation":"update","phase":"prepared","stage":"../outside","backup":".plugin-backup-fixture","restore_enabled":true,"runtime_revision":"r"}`,
		`{"format":1,"id":"weather","operation":"update","phase":"prepared","stage":".plugin-stage-fixture","backup":".plugin-backup-fixture","restore_enabled":true,"runtime_revision":"r","unknown":true}`,
	} {
		if err := os.WriteFile(filepath.Join(base, installJournal), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Pending(); err == nil {
			t.Fatalf("corrupt transaction accepted: %s", data)
		}
		content, err := os.ReadFile(filepath.Join(base, "weather", "marker"))
		if err != nil || string(content) != "old-good" {
			t.Fatal("journal validation changed prior package", err)
		}
	}
}

func TestConcurrentPluginMutationSerialization(t *testing.T) {
	base := filepath.Join(t.TempDir(), "plugins")
	first := transactionStore(t, base)
	ctx, cancel := context.WithTimeout(t.Context(), 40*time.Millisecond)
	defer cancel()
	if second, err := OpenInstallStore(ctx, base); !errors.Is(err, context.DeadlineExceeded) {
		if second != nil {
			second.Close()
		}
		t.Fatalf("concurrent acquisition was not cancellable: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	var active atomic.Int32
	var group sync.WaitGroup
	errorsFound := make(chan error, 8)
	for range 8 {
		group.Go(func() {
			store, err := OpenInstallStore(t.Context(), base)
			if err != nil {
				errorsFound <- err
				return
			}
			defer store.Close()
			if active.Add(1) != 1 {
				errorsFound <- errors.New("concurrent install/update/uninstall critical sections")
				active.Add(-1)
				return
			}
			defer active.Add(-1)
			run := func() error {
				stage, err := os.MkdirTemp(base, ".plugin-stage-")
				if err != nil {
					return err
				}
				tx, err := store.Begin("weather", "install", filepath.Base(stage), false, "")
				if err != nil {
					return err
				}
				if err := store.Publish(tx); err != nil {
					return err
				}
				if err := store.Finish(tx); err != nil {
					return err
				}
				stage, err = os.MkdirTemp(base, ".plugin-stage-")
				if err != nil {
					return err
				}
				tx, err = store.Begin("weather", "update", filepath.Base(stage), false, "r")
				if err != nil {
					return err
				}
				if err := store.Publish(tx); err != nil {
					return err
				}
				if err := store.Finish(tx); err != nil {
					return err
				}
				tx, err = store.Begin("weather", "uninstall", "", false, "r")
				if err != nil {
					return err
				}
				if err := store.Publish(tx); err != nil {
					return err
				}
				return store.Finish(tx)
			}
			if err := run(); err != nil {
				errorsFound <- err
			}
		})
	}
	group.Wait()
	close(errorsFound)
	for err := range errorsFound {
		t.Error(err)
	}
}
