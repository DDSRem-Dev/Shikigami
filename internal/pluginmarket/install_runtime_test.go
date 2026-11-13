package pluginmarket

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestJournaledRuntimeRequestSurvivesCrash(t *testing.T) {
	base := filepath.Join(t.TempDir(), "plugins")
	store := transactionStore(t, base)
	tx, err := store.Begin("weather", "update", ".plugin-stage-fixture", true, "before-stop")
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	if err := store.PrepareRuntimeAction(tx, "disable"); err != nil {
		store.Close()
		t.Fatal(err)
	}
	requestID := tx.RuntimeRequestID
	if requestID == "" || tx.RuntimeExpectedRevision != "before-stop" {
		t.Fatal("runtime mutation was not journaled before IPC")
	}
	store.Close()
	store = transactionStore(t, base)
	defer store.Close()
	tx, err = store.Pending()
	if err != nil || tx == nil || tx.RuntimeRequestID != requestID || tx.RuntimeAction != "disable" || tx.RuntimeExpectedRevision != "before-stop" {
		t.Fatalf("recovery changed an in-flight request identity: %#v %v", tx, err)
	}
	if err := store.ObserveRuntimeAction(tx, "accepted-stop", false); err != nil {
		t.Fatal(err)
	}
	pending, err := store.Pending()
	if err != nil || pending.RuntimeRequestID != requestID || pending.RuntimeRevision != "accepted-stop" {
		t.Fatalf("acceptance lost completion/replay intent: %#v %v", pending, err)
	}
	if err := store.ObserveRuntimeAction(tx, "accepted-stop", true); err != nil {
		t.Fatal(err)
	}
	if err := store.AdoptRuntimeState(tx, "newer-user-disable", false); err != nil {
		t.Fatal(err)
	}
	pending, err = store.Pending()
	if err != nil || pending.RestoreEnabled || pending.RuntimeAction != "" || pending.RuntimeRequestID != "" || pending.RuntimeRevision != "newer-user-disable" {
		t.Fatalf("recovery would overwrite latest desired state: %#v %v", pending, err)
	}
}

func TestFailedPublishAndMissingCommittedPackageRecoverOldVersion(t *testing.T) {
	for _, interrupted := range []string{"publish-failed", "committed-package-missing"} {
		t.Run(interrupted, func(t *testing.T) {
			base := filepath.Join(t.TempDir(), "plugins")
			store := transactionStore(t, base)
			defer store.Close()
			transactionPackage(t, base, "weather", "old-good")
			stage := ".plugin-stage-fixture"
			if interrupted != "publish-failed" {
				transactionPackage(t, base, stage, "new-good")
			}
			tx, err := store.Begin("weather", "update", stage, true, "revision")
			if err != nil {
				t.Fatal(err)
			}
			publishErr := store.Publish(tx)
			if interrupted == "publish-failed" {
				if publishErr == nil {
					t.Fatal("missing stage unexpectedly published")
				}
			} else {
				if publishErr != nil {
					t.Fatal(publishErr)
				}
				if err := os.RemoveAll(filepath.Join(base, "weather")); err != nil {
					t.Fatal(err)
				}
			}
			if err := store.RecoverPublication(tx); err != nil {
				t.Fatal(err)
			}
			content, err := os.ReadFile(filepath.Join(base, "weather", "marker"))
			if err != nil || string(content) != "old-good" {
				t.Fatalf("recovery deleted previous good version: %q %v", content, err)
			}
		})
	}
}

func TestInstallStoreRejectsLegacyRootWithoutWritingRelease(t *testing.T) {
	root := t.TempDir()
	release := filepath.Join(root, "release")
	if err := os.Mkdir(release, 0700); err != nil {
		t.Fatal(err)
	}
	plugins := filepath.Join(root, "plugins")
	if err := os.Symlink(release, plugins); err != nil {
		t.Fatal(err)
	}
	if store, err := OpenInstallStore(t.Context(), plugins); err == nil || !strings.Contains(err.Error(), "legacy release symlink") {
		if store != nil {
			store.Close()
		}
		t.Fatalf("installer mutated immutable release through root symlink: %v", err)
	}
	if _, err := os.Stat(filepath.Join(release, ".install.lock")); !os.IsNotExist(err) {
		t.Fatalf("installer created mutable state inside release: %v", err)
	}
}

func TestInstallationRootCheckDoesNotRecreateMovedDirectory(t *testing.T) {
	root := t.TempDir()
	base := filepath.Join(root, "plugins")
	store := transactionStore(t, base)
	defer store.Close()
	transactionPackage(t, base, "weather", "prior-good")
	parked := filepath.Join(root, "parked-plugins")
	if err := os.Rename(base, parked); err != nil {
		t.Fatal(err)
	}
	if err := store.Sync(); err == nil {
		t.Fatal("installer accepted a moved installation root")
	}
	if _, err := os.Lstat(base); !os.IsNotExist(err) {
		t.Fatalf("identity check recreated the missing installation root: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(parked, "weather", "marker"))
	if err != nil || string(content) != "prior-good" {
		t.Fatalf("identity check changed parked plugin data: %q %v", content, err)
	}
	if err := os.Rename(parked, base); err != nil {
		t.Fatal(err)
	}
	if err := store.Sync(); err != nil {
		t.Fatalf("restored root lost its pinned identity: %v", err)
	}
}
