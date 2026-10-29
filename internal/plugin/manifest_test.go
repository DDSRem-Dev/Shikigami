package plugin

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	"github.com/DDSRem-Dev/Shikigami/pkg/protocol/version"
)

func TestInstalledManifestSelectsRequiredSideCapabilities(t *testing.T) {
	directory := t.TempDir()
	for _, id := range []string{"sync", "port"} {
		offer, err := sdk.BuiltinProtocol(id, "both")
		if err != nil {
			t.Fatal(err)
		}
		manifest := Manifest{Name: id, Version: "dev", Side: "both", Executable: "shiki-plugin-" + id, Protocol: offer}
		write := func() {
			data, err := json.Marshal(manifest)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(directory, id+".manifest.json"), data, 0600); err != nil {
				t.Fatal(err)
			}
		}
		write()
		for _, side := range []string{"client", "server"} {
			selected, err := ReadInstalledManifest(directory, id, side, "dev")
			if err != nil {
				t.Fatal(err)
			}
			matching, _ := sdk.BuiltinProtocol(id, side)
			if _, err := version.Negotiate(selected.Protocol, matching, "dev"); err != nil {
				t.Fatal("matching side rejected", err)
			}
			opposite := "client"
			if side == "client" {
				opposite = "server"
			}
			wrong, _ := sdk.BuiltinProtocol(id, opposite)
			if _, err := version.Negotiate(selected.Protocol, wrong, "dev"); err == nil {
				t.Fatal("wrong worker boundary accepted")
			}
		}
		manifest.Version = "other-release"
		write()
		if _, err := ReadInstalledManifest(directory, id, "client", "dev"); !errors.Is(err, ErrIncompatible) {
			t.Fatal("version mismatch not classified", err)
		}
	}
}
