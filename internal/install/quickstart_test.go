package install

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/DDSRem-Dev/Shikigami/internal/releaseauth"
)

func TestQuickstartSameSourceIgnoresOnlyPrivateCopyPath(t *testing.T) {
	a := releaseauth.Verified{Format: 1, Version: "v1.2.3", OS: "linux", Arch: "amd64", Archive: "/private/first/archive.tar.gz", ArchiveSHA256: "archive", ArchiveSize: 123, ChecksumsSHA256: "payload", KeyFingerprintSHA256: "key"}
	b := a
	b.Archive = "/private/resume/archive.tar.gz"
	if !quickstartSameRelease(a, b) {
		t.Fatal("a second verification of the same signed source caused reinstall")
	}
	for _, mutate := range []func(*releaseauth.Verified){func(v *releaseauth.Verified) { v.Version = "v2" }, func(v *releaseauth.Verified) { v.Arch = "arm64" }, func(v *releaseauth.Verified) { v.ArchiveSHA256 = "other" }, func(v *releaseauth.Verified) { v.ChecksumsSHA256 = "other" }, func(v *releaseauth.Verified) { v.KeyFingerprintSHA256 = "other" }, func(v *releaseauth.Verified) { v.ArchiveSize++ }} {
		changed := b
		mutate(&changed)
		if quickstartSameRelease(a, changed) {
			t.Fatal("changed source was treated as current")
		}
	}
}

func TestQuickstartInterruptedKeyringNeverReprovisionsExistingStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "login.keyring")
	uid := uint32(os.Geteuid())
	provision, err := quickstartRetainedKeyring(path, "dedicated", uid)
	if err != nil || !provision {
		t.Fatalf("absent dedicated: %v %v", provision, err)
	}
	if provision, err = quickstartRetainedKeyring(path, "existing-login", uid); err != nil || provision {
		t.Fatalf("absent login: %v %v", provision, err)
	}
	content := []byte("retained encrypted store")
	if err = os.WriteFile(path, content, 0600); err != nil {
		t.Fatal(err)
	}
	if provision, err = quickstartRetainedKeyring(path, "dedicated", uid); err != nil || provision {
		t.Fatalf("existing store reset: %v %v", provision, err)
	}
	if err = os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err = quickstartRetainedKeyring(path, "dedicated", uid); err == nil {
		t.Fatal("accepted exposed store")
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(content) {
		t.Fatal("inspection modified the keyring")
	}
}
