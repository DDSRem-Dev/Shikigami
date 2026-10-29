package plugin

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	"golang.org/x/sys/unix"
)

func embeddedFixture(t *testing.T) fstest.MapFS {
	t.Helper()
	payload := []byte("verified built-in fixture")
	sum := sha256.Sum256(payload)
	protocol, _ := sdk.BuiltinProtocol("sync", "both")
	manifest := Manifest{Name: "sync", Version: "v1", Executable: "shiki-plugin-sync", SHA256: hex.EncodeToString(sum[:]), Side: "both", Protocol: protocol}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	return fstest.MapFS{"sync.manifest.json": &fstest.MapFile{Data: data}, "shiki-plugin-sync": &fstest.MapFile{Data: payload}}
}
func TestEmbeddedExtractionIsImmutableAndRejectsCorruption(t *testing.T) {
	assets := embeddedFixture(t)
	directory := filepath.Join(t.TempDir(), "plugins")
	first, err := extractBuiltin(assets, directory, "sync", "client", "v1")
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(first.Executable)
	if err != nil {
		t.Fatal(err)
	}
	second, err := extractBuiltin(assets, directory, "sync", "server", "v1")
	if err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(second.Executable)
	if first.Executable != second.Executable || !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("repeat extraction replaced immutable cache")
	}
	if _, err = extractBuiltin(assets, directory, "sync", "client", "v2"); err == nil {
		t.Fatal("different build accepted")
	}
	if err = os.WriteFile(first.Executable, []byte("changed cache"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err = extractBuiltin(assets, directory, "sync", "client", "v1"); err == nil {
		t.Fatal("corrupt cache silently repaired")
	}
	evidence, _ := os.ReadFile(first.Executable)
	if string(evidence) != "changed cache" {
		t.Fatal("corruption evidence overwritten")
	}
}
func TestEmbeddedSpecialFileCannotBlockVerification(t *testing.T) {
	assets := embeddedFixture(t)
	directory := filepath.Join(t.TempDir(), "plugins")
	manifest, err := extractBuiltin(assets, directory, "sync", "client", "v1")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(manifest.Executable); err != nil {
		t.Fatal(err)
	}
	if err = unix.Mkfifo(manifest.Executable, 0600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := extractBuiltin(assets, directory, "sync", "client", "v1"); done <- err }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("FIFO accepted as plugin")
		}
	case <-time.After(time.Second):
		t.Fatal("FIFO blocked cache validation")
	}
}
func TestEmbeddedCacheRejectsSymlinkAncestors(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "plugins")
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(directory, ".embedded")); err != nil {
		t.Fatal(err)
	}
	if _, err := extractBuiltin(embeddedFixture(t), directory, "sync", "client", "v1"); err == nil {
		t.Fatal("symlink cache accepted")
	}
	entries, _ := os.ReadDir(outside)
	if len(entries) != 0 {
		t.Fatal("cache created paths outside its private directory")
	}
}
