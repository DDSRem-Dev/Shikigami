package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DDSRem-Dev/Shikigami/internal/client"
	"github.com/DDSRem-Dev/Shikigami/internal/pluginmarket"
	"github.com/DDSRem-Dev/Shikigami/pkg/config"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
)

func installerManifest(id, version string) sdk.PluginManifest {
	return sdk.PluginManifest{SchemaVersion: "1", ID: id, Version: version, DisplayName: "Installer fixture", Description: "signed installer behavior fixture", Capabilities: []string{"ui.action.v1"}, Sides: []string{"client"}, Binary: "shiki-plugin-" + id, BinaryFlags: []string{"--side=${side}"}}
}

type installerArchiveMember struct {
	name string
	body []byte
	kind byte
}

func installerArchive(t *testing.T, members ...installerArchiveMember) []byte {
	t.Helper()
	var data bytes.Buffer
	gz := gzip.NewWriter(&data)
	writer := tar.NewWriter(gz)
	for _, member := range members {
		kind := member.kind
		if kind == 0 {
			kind = tar.TypeReg
		}
		header := &tar.Header{Name: member.name, Mode: 0700, Typeflag: kind, Size: int64(len(member.body)), Format: tar.FormatUSTAR}
		if kind != tar.TypeReg {
			header.Size = 0
		}
		if err := writer.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if kind == tar.TypeReg {
			if _, err := writer.Write(member.body); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func installerManifestData(t *testing.T, manifest sdk.PluginManifest) []byte {
	t.Helper()
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(data, '\n')
}

func installerArchiveFile(t *testing.T, data []byte) *os.File {
	t.Helper()
	name := filepath.Join(t.TempDir(), "fixture.tar.gz")
	if err := os.WriteFile(name, data, 0600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(name)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { file.Close() })
	return file
}

func installerApp(t *testing.T) (*app, string) {
	t.Helper()
	home, err := os.MkdirTemp("/tmp", "shiki-install-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })
	t.Setenv("HOME", home)
	cfg := config.DefaultClient()
	cfg.PluginsDir = filepath.Join(home, "custom-packages")
	cfg.Agent.Socket = filepath.Join(home, "agent.sock")
	cfg.Agent.StateDir = filepath.Join(home, "agent-state")
	cfg.Agent.AutoStart = true
	cfg.Tailscale.Enabled = false
	name := filepath.Join(home, "config.yaml")
	if err := config.SaveClient(name, cfg); err != nil {
		t.Fatal(err)
	}
	return &app{configPath: name, timeout: time.Second, output: &bytes.Buffer{}, errorOutput: io.Discard}, cfg.PluginsDir
}

type installerMarket struct {
	a                         *app
	base                      string
	server                    *httptest.Server
	trust                     client.PluginMarketTrustStore
	private                   map[string]ed25519.PrivateKey
	mu                        sync.Mutex
	index, archive, signature []byte
	archiveRequests           atomic.Int32
}

func installerSigned(t *testing.T, private ed25519.PrivateKey, keyID string, payload any) []byte {
	t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	message := append([]byte("Shikigami plugin distribution\x00format=1\x00"), data...)
	envelope := pluginmarket.SignedEnvelope{Format: 1, KeyID: keyID, Payload: base64.StdEncoding.EncodeToString(data), Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(private, message))}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func newInstallerMarket(t *testing.T) *installerMarket {
	t.Helper()
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("marketplace platform contract supports Linux and macOS")
	}
	a, base := installerApp(t)
	market := &installerMarket{a: a, base: base, trust: client.NewPluginMarketTrustStore(), private: map[string]ed25519.PrivateKey{}}
	for _, keyID := range []string{"publisher-1", "publisher-2"} {
		public, private, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		der, err := x509.MarshalPKIXPublicKey(public)
		if err != nil {
			t.Fatal(err)
		}
		if err := client.AddPluginMarketTrustRoot(&market.trust, client.PluginMarketTrustRoot{KeyID: keyID, PublicKey: string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))}); err != nil {
			t.Fatal(err)
		}
		market.private[keyID] = private
	}
	path, err := pluginTrustStorePath()
	if err != nil {
		t.Fatal(err)
	}
	if err := client.SavePluginMarketTrustStore(path, market.trust); err != nil {
		t.Fatal(err)
	}
	market.server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		market.mu.Lock()
		defer market.mu.Unlock()
		switch r.URL.Path {
		case "/index":
			_, _ = w.Write(market.index)
		case "/archive":
			market.archiveRequests.Add(1)
			_, _ = w.Write(market.archive)
		case "/signature":
			_, _ = w.Write(market.signature)
		default:
			http.NotFound(w, r)
		}
	}))
	original := http.DefaultTransport
	http.DefaultTransport = market.server.Client().Transport
	t.Cleanup(func() { market.server.Close(); http.DefaultTransport = original })
	return market
}

func (market *installerMarket) publish(t *testing.T, entryVersion, innerID, innerVersion, keyID string) {
	t.Helper()
	manifest := installerManifest(innerID, innerVersion)
	archive := installerArchive(t, installerArchiveMember{name: "manifest.json", body: installerManifestData(t, manifest)}, installerArchiveMember{name: manifest.Binary, body: []byte("fixture executable " + innerVersion)})
	digest := sha256.Sum256(archive)
	entry := client.PluginMarketEntry{ID: "weather", Version: entryVersion, OS: runtime.GOOS, Arch: runtime.GOARCH, URL: market.server.URL + "/archive", SHA256: fmt.Sprintf("%x", digest[:]), SignatureURL: market.server.URL + "/signature", KeyID: keyID}
	metadata := pluginmarket.ArchiveMetadata{ID: entry.ID, Version: entry.Version, OS: entry.OS, Arch: entry.Arch, Archive: fmt.Sprintf("%s-%s-%s-%s.tar.gz", entry.ID, entry.Version, entry.OS, entry.Arch), ArchiveSHA256: entry.SHA256, ArchiveSize: int64(len(archive))}
	index := client.PluginMarketIndex{Format: 1, Publisher: "installer test", GeneratedAt: "2026-11-13T00:00:00Z", Entries: []client.PluginMarketEntry{entry}}
	market.mu.Lock()
	defer market.mu.Unlock()
	market.index = installerSigned(t, market.private["publisher-1"], "publisher-1", index)
	market.archive = archive
	market.signature = installerSigned(t, market.private[keyID], keyID, metadata)
}

func (market *installerMarket) install(t *testing.T) {
	t.Helper()
	if err := market.a.installFromMarket(t.Context(), "weather", "", market.server.URL+"/index"); err != nil {
		t.Fatal(err)
	}
}

func (market *installerMarket) update(t *testing.T, override string) error {
	t.Helper()
	return market.a.withPluginMutation(t.Context(), func(session *client.PluginInstallSession, store *client.PluginInstallStore) error {
		return market.a.updateOnePlugin(t.Context(), session, store, "weather", "", override)
	})
}

func TestMarketplaceInstallRetainsExactManifestAndTrust(t *testing.T) {
	market := newInstallerMarket(t)
	market.publish(t, "1.2.3-beta.9+build.7", "weather", "1.2.3-beta.9+build.7", "publisher-1")
	market.install(t)
	version, err := metaVersion("weather", market.base)
	if err != nil || version != "1.2.3-beta.9+build.7" {
		t.Fatalf("complete installed manifest baseline was lost: %q %v", version, err)
	}
	manifestBytes, err := os.ReadFile(filepath.Join(market.base, "weather", "manifest.json"))
	if err != nil || !bytes.Equal(manifestBytes, installerManifestData(t, installerManifest("weather", version))) {
		t.Fatalf("installed manifest was normalized or changed: %s %v", manifestBytes, err)
	}
	if got, err := validateInstalledArtifact(t.Context(), "weather", market.base); err != nil || got != version {
		t.Fatalf("integrity receipt does not bind installed bytes: %q %v", got, err)
	}
	meta, err := readInstallMeta("weather", market.base)
	fingerprint, fingerprintErr := client.PluginMarketSigningKeyFingerprint(market.trust, "publisher-1")
	if err != nil || fingerprintErr != nil || meta.SourceKind != "market" || meta.KeySHA256 != fingerprint || meta.KeyID != "publisher-1" || meta.MarketURL != market.server.URL+"/index" {
		t.Fatalf("publisher/source continuity was not saved: %#v %v %v", meta, err, fingerprintErr)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(market.base), ".shiki", "plugins", "weather")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("installer ignored configured plugins_dir: %v", err)
	}
}

func TestMarketplaceRefusesDowngradeAndSkipsSameVersion(t *testing.T) {
	market := newInstallerMarket(t)
	market.publish(t, "1.2.3", "weather", "1.2.3", "publisher-1")
	market.install(t)
	before := market.archiveRequests.Load()
	if err := market.update(t, ""); err != nil {
		t.Fatalf("same version should be a no-op: %v", err)
	}
	market.publish(t, "1.2.2", "weather", "1.2.2", "publisher-1")
	if err := market.update(t, ""); err == nil || !strings.Contains(err.Error(), "downgrade") {
		t.Fatalf("marketplace downgrade accepted: %v", err)
	}
	if market.archiveRequests.Load() != before {
		t.Fatal("same/downgraded version unnecessarily downloaded an archive")
	}
	if got, err := metaVersion("weather", market.base); err != nil || got != "1.2.3" {
		t.Fatalf("baseline changed: %q %v", got, err)
	}
}

func TestSignedInnerIdentityCannotOverwriteAnyPlugin(t *testing.T) {
	for _, mismatch := range []string{"id", "version"} {
		t.Run(mismatch, func(t *testing.T) {
			market := newInstallerMarket(t)
			market.publish(t, "1.0.0", "weather", "1.0.0", "publisher-1")
			market.install(t)
			victim := filepath.Join(market.base, "victim")
			if err := os.Mkdir(victim, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(victim, "keep"), []byte("prior good unrelated plugin"), 0600); err != nil {
				t.Fatal(err)
			}
			innerID, innerVersion := "weather", "1.1.0"
			if mismatch == "id" {
				innerID = "victim"
			} else {
				innerVersion = "9.0.0"
			}
			market.publish(t, "1.1.0", innerID, innerVersion, "publisher-1")
			if err := market.update(t, ""); err == nil || !strings.Contains(err.Error(), "authenticated marketplace") {
				t.Fatalf("signed inner %s mismatch accepted: %v", mismatch, err)
			}
			if version, err := metaVersion("weather", market.base); err != nil || version != "1.0.0" {
				t.Fatalf("requested plugin overwritten: %q %v", version, err)
			}
			if data, err := os.ReadFile(filepath.Join(victim, "keep")); err != nil || string(data) != "prior good unrelated plugin" {
				t.Fatalf("wrong signed inner ID overwrote unrelated plugin: %q %v", data, err)
			}
		})
	}
}

func TestMarketplacePublisherKeyCannotChangeEvenWithOverride(t *testing.T) {
	market := newInstallerMarket(t)
	market.publish(t, "1.0.0", "weather", "1.0.0", "publisher-1")
	market.install(t)
	market.publish(t, "1.1.0", "weather", "1.1.0", "publisher-2")
	if err := market.update(t, market.server.URL+"/index"); err == nil || !strings.Contains(err.Error(), "key substitution") {
		t.Fatalf("explicit market override silently replaced publisher: %v", err)
	}
	changed := client.NewPluginMarketTrustStore()
	root := market.trust.Roots[1]
	root.KeyID = "publisher-1"
	if err := client.AddPluginMarketTrustRoot(&changed, root); err != nil {
		t.Fatal(err)
	}
	path, _ := pluginTrustStorePath()
	if err := client.SavePluginMarketTrustStore(path, changed); err != nil {
		t.Fatal(err)
	}
	if err := market.update(t, ""); err == nil || !strings.Contains(err.Error(), "key material changed") {
		t.Fatalf("same key ID with different material silently accepted: %v", err)
	}
	if version, err := metaVersion("weather", market.base); err != nil || version != "1.0.0" {
		t.Fatalf("key failure overwrote plugin: %q %v", version, err)
	}
}

func TestBadInstallMetadataIsNotTrustedManualFallback(t *testing.T) {
	market := newInstallerMarket(t)
	market.publish(t, "1.0.0", "weather", "1.0.0", "publisher-1")
	market.install(t)
	path := filepath.Join(market.base, "weather", ".install-meta.json")
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, corrupt := range []string{"{}", "{", strings.Replace(string(original), `"format": 1`, `"format": 99`, 1), strings.Replace(string(original), `"source_kind": "market"`, `"source_kind": "manual"`, 1), strings.TrimSpace(string(original)) + ` {}`} {
		if err := os.WriteFile(path, []byte(corrupt), 0600); err != nil {
			t.Fatal(err)
		}
		if err := market.update(t, ""); err == nil || errors.Is(err, errPluginNotMarket) {
			t.Fatalf("corrupt metadata silently became a manual/trusted fallback: %s: %v", corrupt, err)
		}
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := market.update(t, ""); err == nil || errors.Is(err, errPluginNotMarket) {
		t.Fatalf("missing metadata silently skipped: %v", err)
	}
}

func TestArchiveTotalBudgetAndAmbiguousContent(t *testing.T) {
	manifest := installerManifest("weather", "1.0.0")
	valid := []installerArchiveMember{{name: "manifest.json", body: installerManifestData(t, manifest)}, {name: manifest.Binary, body: []byte("executable")}}
	file := installerArchiveFile(t, installerArchive(t, valid...))
	if _, err := extractPluginArchiveLimited(t.Context(), file, t.TempDir(), 1024, 16); err == nil || !strings.Contains(err.Error(), "total decompression budget") {
		t.Fatalf("aggregate expansion budget ignored: %v", err)
	}
	if _, err := extractPluginArchiveLimited(t.Context(), file, t.TempDir(), pluginMaxExpandedBytes, 1); err == nil || !strings.Contains(err.Error(), "member count") {
		t.Fatalf("member budget ignored: %v", err)
	}
	for _, extra := range []installerArchiveMember{{name: "readme.txt", body: []byte("unexpected")}, {name: "manifest.json", body: installerManifestData(t, manifest)}, {name: "../escape", body: []byte("unsafe")}, {name: "link", kind: tar.TypeSymlink}} {
		members := append(append([]installerArchiveMember(nil), valid...), extra)
		file := installerArchiveFile(t, installerArchive(t, members...))
		if _, err := extractPluginArchive(t.Context(), file, t.TempDir()); err == nil {
			t.Fatalf("unexpected/ambiguous archive content accepted: %s", extra.name)
		}
	}
	wrongBinary := append([]installerArchiveMember(nil), valid...)
	wrongBinary[1].name = "shiki-plugin-other"
	if _, err := extractPluginArchive(t.Context(), installerArchiveFile(t, installerArchive(t, wrongBinary...)), t.TempDir()); err == nil {
		t.Fatal("installer retained obsolete single-file binary fallback")
	}
	wrapped := []installerArchiveMember{{name: "package/", kind: tar.TypeDir}, {name: "package/manifest.json", body: installerManifestData(t, manifest)}, {name: "package/" + manifest.Binary, body: []byte("executable")}}
	if _, err := extractPluginArchive(t.Context(), installerArchiveFile(t, installerArchive(t, wrapped...)), t.TempDir()); err != nil {
		t.Fatalf("unambiguous common package root rejected: %v", err)
	}
}

func TestPluginDownloadRejectsEveryHTTPRedirect(t *testing.T) {
	var plaintextRequests atomic.Int32
	plaintext := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		plaintextRequests.Add(1)
		_, _ = w.Write([]byte("must never download"))
	}))
	defer plaintext.Close()
	secure := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/first" {
			http.Redirect(w, r, "/second", http.StatusFound)
			return
		}
		http.Redirect(w, r, plaintext.URL, http.StatusFound)
	}))
	defer secure.Close()
	original := http.DefaultTransport
	http.DefaultTransport = secure.Client().Transport
	t.Cleanup(func() { http.DefaultTransport = original })
	if _, err := fetchPluginBytes(t.Context(), secure.URL+"/first", 1024); err == nil {
		t.Fatal("index/signature HTTPS-to-HTTP redirect accepted")
	}
	if file, cleanup, err := fetchPluginSource(t.Context(), secure.URL+"/first", "fixture.tar.gz"); err == nil {
		file.Close()
		cleanup()
		t.Fatal("archive HTTPS-to-HTTP redirect accepted")
	}
	if plaintextRequests.Load() != 0 {
		t.Fatal("redirect policy contacted plaintext destination before refusing")
	}
}

func TestPluginDownloadsArePrivateSnapshotsAndCancelable(t *testing.T) {
	name := filepath.Join(t.TempDir(), "source.tar.gz")
	if err := os.WriteFile(name, []byte("original snapshot"), 0600); err != nil {
		t.Fatal(err)
	}
	first, cleanupFirst, err := fetchPluginSource(t.Context(), name, "same-name.tar.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanupFirst()
	if err := os.WriteFile(name, []byte("changed source"), 0600); err != nil {
		t.Fatal(err)
	}
	second, cleanupSecond, err := fetchPluginSource(t.Context(), name, "same-name.tar.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanupSecond()
	if filepath.Dir(first.Name()) == filepath.Dir(second.Name()) {
		t.Fatal("canonical archive filename reused a shared download directory")
	}
	data, err := io.ReadAll(first)
	if err != nil || string(data) != "original snapshot" {
		t.Fatalf("local archive was reopened instead of snapshotted: %q %v", data, err)
	}
	info, err := os.Stat(filepath.Dir(first.Name()))
	if err != nil || info.Mode().Perm() != 0700 {
		t.Fatalf("download directory is not private: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, cleanup, err := fetchPluginSource(ctx, name, "cancel.tar.gz"); !errors.Is(err, context.Canceled) {
		if cleanup != nil {
			cleanup()
		}
		t.Fatalf("local snapshot ignored cancellation: %v", err)
	}
}

func TestPluginVersionSelectionUsesNumericPrereleases(t *testing.T) {
	entries := []client.PluginMarketEntry{{ID: "weather", Version: "1.0.0-beta.9", OS: runtime.GOOS, Arch: runtime.GOARCH}, {ID: "weather", Version: "1.0.0-beta.10", OS: runtime.GOOS, Arch: runtime.GOARCH}}
	entry, err := choosePluginEntry(client.PluginMarketIndex{Entries: entries}, "weather", "")
	if err != nil || entry.Version != "1.0.0-beta.10" {
		t.Fatalf("wrong prerelease selected: %#v %v", entry, err)
	}
	entries[1].Version = "nonsense"
	if _, err := choosePluginEntry(client.PluginMarketIndex{Entries: entries}, "weather", ""); err == nil {
		t.Fatal("invalid signed semantic version accepted")
	}
}
