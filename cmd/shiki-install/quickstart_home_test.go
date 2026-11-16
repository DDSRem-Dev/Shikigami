package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	host "github.com/DDSRem-Dev/Shikigami/internal/plugin"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	"os"
	"path/filepath"
	"testing"
)

func TestQuickstartSecuresOnlyOwnedHomeWriteBits(t *testing.T) {
	home := t.TempDir()
	file := filepath.Join(home, "keep")
	if err := os.WriteFile(file, []byte("preserve"), 0666); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(file, 0666); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(home, 0775); err != nil {
		t.Fatal(err)
	}
	if err := quickstartSecureHome(home, uint32(os.Geteuid())); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(home)
	child, _ := os.Stat(file)
	if info.Mode().Perm() != 0755 || child.Mode().Perm() != 0666 {
		t.Fatal("unexpected recursive or broad permission change")
	}
	link := filepath.Join(t.TempDir(), "home")
	if err := os.Symlink(home, link); err != nil {
		t.Fatal(err)
	}
	if err := quickstartSecureHome(link, uint32(os.Geteuid())); err == nil {
		t.Fatal("followed home symlink")
	}
	if err := quickstartSecureHome(home, uint32(os.Geteuid()+1)); err == nil {
		t.Fatal("accepted another owner")
	}
}

func TestQuickstartZshPATHContinuesAfterUnsafeFile(t *testing.T) {
	t.Setenv("SHELL", "/bin/zsh")
	home := t.TempDir()
	target := filepath.Join(t.TempDir(), "untouched")
	if err := os.WriteFile(target, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(home, ".zprofile")); err != nil {
		t.Fatal(err)
	}
	if err := quickstartShellPath(home); err == nil {
		t.Fatal("unsafe shell file not reported")
	}
	data, _ := os.ReadFile(filepath.Join(home, ".zshrc"))
	before, _ := os.ReadFile(target)
	if !bytes.Contains(data, []byte(quickstartPathBlock)) || string(before) != "preserve" {
		t.Fatal("safe file skipped or unsafe target changed")
	}
}

func TestQuickstartHomeRepairRestoresBuiltinTrust(t *testing.T) {
	home := t.TempDir()
	plugins := filepath.Join(home, ".shiki", "plugins")
	release := filepath.Join(home, "release")
	for _, p := range []string{plugins, release} {
		if err := os.MkdirAll(p, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(release, filepath.Join(plugins, ".builtin")); err != nil {
		t.Fatal(err)
	}
	payload := []byte("inert trusted fixture")
	if err := os.WriteFile(filepath.Join(release, "shiki-plugin-sync"), payload, 0700); err != nil {
		t.Fatal(err)
	}
	protocol, err := sdk.BuiltinProtocol("sync", "both")
	if err != nil {
		t.Fatal(err)
	}
	manifest := host.Manifest{Name: "sync", Version: "v0.0.1-alpha.6", Side: "both", Executable: "shiki-plugin-sync", SHA256: fmt.Sprintf("%x", sha256.Sum256(payload)), Protocol: protocol}
	data, _ := json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(release, "sync.manifest.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(home, 0775); err != nil {
		t.Fatal(err)
	}
	if _, err := host.ResolveBuiltinManifest(plugins, "sync", "client", manifest.Version); err == nil {
		t.Fatal("unsafe home unexpectedly trusted")
	}
	if err := quickstartSecureHome(home, uint32(os.Getuid())); err != nil {
		t.Fatal(err)
	}
	if _, err := host.ResolveBuiltinManifest(plugins, "sync", "client", manifest.Version); err != nil {
		t.Fatal("approved home repair did not restore plugin trust", err)
	}
}
