package plugin

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/DDSRem-Dev/Shikigami/internal/server/storage"
	"google.golang.org/protobuf/types/known/structpb"
)

func TestAbsentOptionalBuiltinsDoNotInventInstalledEntries(t *testing.T) {
	manager, err := NewSupervisor(t.Context(), SupervisorOptions{
		StateDir: filepath.Join(t.TempDir(), "control"), Side: "server",
		Validate: func(string, *structpb.Struct) error { return nil },
		Config:   func(string, *structpb.Struct) (Config, error) { return Config{}, os.ErrNotExist },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	list, err := manager.List()
	if err != nil {
		t.Fatal(err)
	}
	for _, status := range list.Plugins {
		if status.PluginId == "region" || status.PluginId == "browser" {
			t.Fatal("old base release advertised an absent optional artifact", status.PluginId)
		}
	}
}

func TestMissingOptionalArtifactRetainsSavedDisableIntent(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "control")
	store, err := storage.Open(directory, storage.Options{Validate: validatePluginState})
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Update(func(state *storage.Snapshot) error {
		return state.Put("plugins", "browser", &desiredPlugin{ID: "browser", Revision: "retained", DisablePending: true})
	}); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	manager, err := NewSupervisor(t.Context(), SupervisorOptions{
		StateDir: directory, Side: "server",
		Validate: func(string, *structpb.Struct) error { return nil },
		Config:   func(string, *structpb.Struct) (Config, error) { return Config{}, os.ErrNotExist },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	want, err := manager.desired("browser")
	if err != nil || want.Enabled || !want.DisablePending || want.Revision != "retained" {
		t.Fatal("missing artifact erased durable stop intent", want, err)
	}
}

func TestMalformedPresentOptionalArtifactFailsDiscovery(t *testing.T) {
	if !BuiltinAvailable("browser", "server") {
		t.Skip("optional server artifacts are unsupported on this platform")
	}
	invalid := errors.New("invalid optional artifact")
	_, err := NewSupervisor(t.Context(), SupervisorOptions{
		StateDir: filepath.Join(t.TempDir(), "control"), Side: "server",
		Validate: func(string, *structpb.Struct) error { return nil },
		Config:   func(string, *structpb.Struct) (Config, error) { return Config{}, invalid },
	})
	if !errors.Is(err, invalid) {
		t.Fatal("malformed artifact was silently omitted", err)
	}
}
