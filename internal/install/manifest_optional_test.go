package install

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	host "github.com/DDSRem-Dev/Shikigami/internal/plugin"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
)

func optionalBundleFixture(t *testing.T) *Bundle {
	t.Helper()
	bundle := bundleFixture(t, "v1.2.3-optional")
	ids := []string{"browser"}
	if runtime.GOOS == "linux" {
		ids = append(ids, "region")
	}
	for _, id := range ids {
		name := "shiki-plugin-" + id
		payload := []byte("inert optional fixture " + id)
		if err := os.WriteFile(filepath.Join(bundle.Directory, name), payload, 0700); err != nil {
			t.Fatal(err)
		}
		side := "both"
		if id == "region" {
			side = "server"
		}
		if id == "browser" && runtime.GOOS == "darwin" {
			side = "client"
		}
		protocol, err := sdk.BuiltinProtocol(id, side)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(payload)
		metadata := host.Manifest{Name: id, Version: bundle.Manifest.Version, Executable: name, SHA256: hex.EncodeToString(sum[:]), Side: side, Protocol: protocol}
		data, err := json.Marshal(metadata)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(bundle.Directory, id+".manifest.json"), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if runtime.GOOS == "linux" {
		for _, name := range linuxSupportNames {
			if err := os.WriteFile(filepath.Join(bundle.Directory, name), []byte("inert signed support "+name), 0700); err != nil {
				t.Fatal(err)
			}
		}
	}
	pin, err := CreateManifest(bundle.Directory, bundle.Manifest.Version, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Fatal(err)
	}
	bundle, err = Verify(bundle.Directory, bundle.Manifest.Version, pin)
	if err != nil {
		t.Fatal(err)
	}
	return bundle
}

func TestOptionalPayloadsAuthenticateAndStageWithoutAdvertisingOldAssets(t *testing.T) {
	legacy := bundleFixture(t, "v1.0.0")
	for _, name := range []string{"browser.manifest.json", "region.manifest.json", "shiki-region-helper", "shiki-browser-runtime"} {
		if legacy.Manifest.Has(name) {
			t.Fatalf("old release advertises missing artifact %s", name)
		}
	}
	bundle := optionalBundleFixture(t)
	artifacts := testArtifacts(t)
	release, err := artifacts.Stage(bundle)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range bundle.Manifest.Files {
		if err = verifyFile(filepath.Join(artifacts.Prefix, release), entry); err != nil {
			t.Fatal(err)
		}
	}
	if current, err := artifacts.Current(); err != nil || current != "" {
		t.Fatal("staging activated a release", current, err)
	}
	if !bundle.Manifest.Has("browser.manifest.json") {
		t.Fatal("browser payload not recorded")
	}
	if runtime.GOOS == "linux" && (!bundle.Manifest.Has("region.manifest.json") || !bundle.Manifest.Has("shiki-region-helper") || !bundle.Manifest.Has("shiki-browser-runtime")) {
		t.Fatal("Linux support group is incomplete")
	}
}

func TestOptionalPayloadGroupsCannotBePartialOrCrossPlatform(t *testing.T) {
	bundle := optionalBundleFixture(t)
	for _, removed := range []string{"shiki-plugin-browser", "browser.manifest.json", "shiki-browser-runtime", "shiki-plugin-region", "region.manifest.json", "shiki-region-helper"} {
		if !bundle.Manifest.Has(removed) {
			continue
		}
		var files []File
		for _, entry := range bundle.Manifest.Files {
			if entry.Name != removed {
				files = append(files, entry)
			}
		}
		if err := validatePayloadSet(files, runtime.GOOS); err == nil {
			t.Fatalf("partial group accepted after removing %s", removed)
		}
	}
	base := bundleFixture(t, "v1.0.1").Manifest.Files
	for _, linuxOnly := range []string{"shiki-region-helper", "shiki-browser-runtime", "shiki-plugin-region", "region.manifest.json"} {
		files := append(append([]File{}, base...), File{Name: linuxOnly})
		if err := validatePayloadSet(files, "darwin"); err == nil {
			t.Fatalf("Darwin release accepts Linux-only artifact %s", linuxOnly)
		}
	}
}
