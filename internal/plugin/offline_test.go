package plugin

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/DDSRem-Dev/Shikigami/internal/server/storage"
)

func TestOfflineBrowserDisableRequiresDaemonOwnershipAndNoArtifact(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "daemon")
	host, err := storage.Open(stateDir, storage.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	controlDir := filepath.Join(stateDir, "plugin-control")
	control, err := storage.Open(controlDir, storage.Options{Validate: validatePluginState})
	if err != nil {
		t.Fatal(err)
	}
	if err = control.Update(func(state *storage.Snapshot) error {
		return state.Put("plugins", "browser", &desiredPlugin{ID: "browser", Enabled: true, Revision: "before-disable"})
	}); err != nil {
		t.Fatal(err)
	}
	if err = control.Close(); err != nil {
		t.Fatal(err)
	}
	if err = DisableOffline(controlDir, "browser"); !errors.Is(err, storage.ErrLocked) {
		t.Fatal("offline mutation admitted while daemon owned state", err)
	}
	if err = host.Close(); err != nil {
		t.Fatal(err)
	}
	// There is deliberately no browser executable, manifest or user service.
	// Durable disable does not depend on an available plugin artifact.
	if err = DisableOffline(controlDir, "browser"); err != nil {
		t.Fatal(err)
	}
	control, err = storage.Open(controlDir, storage.Options{Validate: validatePluginState})
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close()
	state, err := control.View()
	if err != nil {
		t.Fatal(err)
	}
	var want desiredPlugin
	if err = state.Get("plugins", "browser", &want); err != nil {
		t.Fatal(err)
	}
	if want.Enabled || !want.DisablePending || want.Revision == "before-disable" {
		t.Fatal("disable intent did not survive unavailable artifact", want)
	}
}
