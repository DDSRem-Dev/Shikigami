package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DDSRem-Dev/Shikigami/internal/client"
)

func TestInstalledVersionRequiresCompleteValidManifest(t *testing.T) {
	market := newInstallerMarket(t)
	market.publish(t, "1.0.0", "weather", "1.0.0", "publisher-1")
	market.install(t)
	path := filepath.Join(market.base, "weather", "manifest.json")
	valid, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{
		`{"version":"1.0.0"}`,
		strings.Replace(string(valid), `"id": "weather"`, `"id": "other"`, 1),
		strings.Replace(string(valid), `"version": "1.0.0"`, `"version": "invalid"`, 1),
		strings.Replace(string(valid), `"binary": "shiki-plugin-weather"`, `"binary": "renamed"`, 1),
		strings.TrimSpace(string(valid)) + ` {}`,
	} {
		if err := os.WriteFile(path, []byte(invalid), 0600); err != nil {
			t.Fatal(err)
		}
		if version, err := metaVersion("weather", market.base); err == nil || version != "" {
			t.Fatalf("corrupt full manifest became an empty successful baseline: %q %v", version, err)
		}
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := metaVersion("weather", market.base); err == nil {
		t.Fatal("missing installed manifest silently became a baseline")
	}
}

func TestArchiveRejectsHiddenPhysicalMetadataAndTrailingExpansion(t *testing.T) {
	manifest := installerManifest("weather", "1.0.0")
	var encoded bytes.Buffer
	gz := gzip.NewWriter(&encoded)
	writer := tar.NewWriter(gz)
	data := installerManifestData(t, manifest)
	if err := writer.WriteHeader(&tar.Header{Name: "manifest.json", Typeflag: tar.TypeReg, Mode: 0600, Size: int64(len(data)), Format: tar.FormatPAX, PAXRecords: map[string]string{"comment": "hidden metadata header"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := extractPluginArchive(t.Context(), installerArchiveFile(t, encoded.Bytes()), t.TempDir()); err == nil || !strings.Contains(err.Error(), "physical tar") {
		t.Fatalf("hidden physical header escaped member/type budget: %v", err)
	}
	archive := installerArchive(t, installerArchiveMember{name: "manifest.json", body: data}, installerArchiveMember{name: manifest.Binary, body: []byte("binary")})
	reader, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	reader.Close()
	encoded.Reset()
	gz = gzip.NewWriter(&encoded)
	if _, err := gz.Write(raw); err != nil {
		t.Fatal(err)
	}
	if _, err := gz.Write(make([]byte, 64<<10)); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := extractPluginArchiveLimited(t.Context(), installerArchiveFile(t, encoded.Bytes()), t.TempDir(), 16<<10, 16); err == nil || !strings.Contains(err.Error(), "total decompression budget") {
		t.Fatalf("expansion after tar terminator escaped total budget: %v", err)
	}
	concatenated := append(append([]byte(nil), archive...), archive...)
	if _, err := extractPluginArchive(t.Context(), installerArchiveFile(t, concatenated), t.TempDir()); err == nil {
		t.Fatal("concatenated gzip stream accepted")
	}
}

func TestInterruptedUpdateRestoresEnabledIntentThroughIPC(t *testing.T) {
	for _, phase := range []string{"prepared", "old-moved", "new-moved", "latest-user-disabled"} {
		t.Run(phase, func(t *testing.T) {
			market := newInstallerMarket(t)
			market.publish(t, "1.0.0", "weather", "1.0.0", "publisher-1")
			market.install(t)
			probe := attachInstallerRuntime(t, market, true)
			store, err := client.OpenPluginInstallStore(t.Context(), market.base)
			if err != nil {
				t.Fatal(err)
			}
			stage, err := os.MkdirTemp(market.base, ".plugin-stage-closure-")
			if err != nil {
				store.Close()
				t.Fatal(err)
			}
			tx, err := store.Begin("weather", "update", filepath.Base(stage), true, "1")
			if err != nil {
				store.Close()
				t.Fatal(err)
			}
			if phase == "old-moved" || phase == "new-moved" {
				if err := os.Rename(filepath.Join(market.base, "weather"), filepath.Join(market.base, tx.Backup)); err != nil {
					store.Close()
					t.Fatal(err)
				}
			}
			if phase == "new-moved" {
				if err := os.Rename(stage, filepath.Join(market.base, "weather")); err != nil {
					store.Close()
					t.Fatal(err)
				}
			}
			if err := store.Sync(); err != nil {
				store.Close()
				t.Fatal(err)
			}
			if err := store.ObserveRuntimeAction(tx, "2", true); err != nil {
				store.Close()
				t.Fatal(err)
			}
			store.Close()
			probe.mu.Lock()
			probe.enabled, probe.state, probe.revision = false, "stopped", 2
			if phase == "latest-user-disabled" {
				probe.revision = 3
			}
			probe.mu.Unlock()
			if err := market.a.withPluginMutation(t.Context(), func(*client.PluginInstallSession, *client.PluginInstallStore) error { return nil }); err != nil {
				t.Fatal(err)
			}
			if version, err := validateInstalledArtifact(t.Context(), "weather", market.base); err != nil || version != "1.0.0" {
				t.Fatalf("interrupted publication lost previous package: %q %v", version, err)
			}
			probe.mu.Lock()
			status := probe.statusLocked()
			probe.mu.Unlock()
			wantEnabled := phase != "latest-user-disabled"
			if status.Enabled != wantEnabled || wantEnabled && status.State != "ready" || !wantEnabled && status.State != "stopped" || status.BuildVersion != "1.0.0" {
				t.Fatalf("journal restoration lost IPC intent or overwrote latest user state: %v", status)
			}
			if _, err := os.Stat(filepath.Join(market.base, ".install-transaction.json")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("recovered journal remains: %v", err)
			}
		})
	}
}
