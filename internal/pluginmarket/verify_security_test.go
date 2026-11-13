package pluginmarket

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestArchiveVerificationKeepsVerifiedDescriptorAndCancels(t *testing.T) {
	store, private := testRoot(t)
	entry := testEntry()
	archive := []byte("the verified archive bytes")
	digest := sha256.Sum256(archive)
	entry.SHA256 = fmt.Sprintf("%x", digest[:])
	name := archiveNameCandidates(entry.ID, entry.Version, entry.OS, entry.Arch)[0]
	metadata := ArchiveMetadata{ID: entry.ID, Version: entry.Version, OS: entry.OS, Arch: entry.Arch, Archive: name, ArchiveSHA256: entry.SHA256, ArchiveSize: int64(len(archive))}
	signature := testEnvelope(t, private, entry.KeyID, metadata)
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, archive, 0600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := VerifyArchive(ctx, file, signature, entry, store); !errors.Is(err, context.Canceled) {
		t.Fatalf("verification ignored cancellation: %v", err)
	}
	if err := VerifyArchive(t.Context(), file, signature, entry, store); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path, path+".verified"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("substituted after verification"), 0600); err != nil {
		t.Fatal(err)
	}
	verified, err := io.ReadAll(file)
	if err != nil || string(verified) != string(archive) {
		t.Fatalf("verified bytes were reopened/substituted: %q %v", verified, err)
	}
}

func TestPublisherFingerprintPinsActualKeyMaterial(t *testing.T) {
	first, _ := testRoot(t)
	second, _ := testRoot(t)
	one, err := SigningKeyFingerprint(first, "root-1")
	if err != nil {
		t.Fatal(err)
	}
	two, err := SigningKeyFingerprint(second, "root-1")
	if err != nil {
		t.Fatal(err)
	}
	if one == two || len(one) != 64 || len(two) != 64 {
		t.Fatalf("publisher key replacement was not pinned: %q / %q", one, two)
	}
	if _, err := SigningKeyFingerprint(first, "unknown"); err == nil {
		t.Fatal("unknown publisher key fingerprint accepted")
	}
}
