package pluginmarket

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func testRoot(t *testing.T) (TrustStore, ed25519.PrivateKey) {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}
	store := NewTrustStore()
	if err := store.AddRoot(TrustRoot{KeyID: "root-1", PublicKey: string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))}); err != nil {
		t.Fatal(err)
	}
	return store, private
}

func testEnvelope(t *testing.T, private ed25519.PrivateKey, keyID string, payload any) []byte {
	t.Helper()
	payloadData, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	signature := ed25519.Sign(private, signedMessage(payloadData))
	envelope := SignedEnvelope{
		Format: Format, KeyID: keyID,
		Payload:   base64.StdEncoding.EncodeToString(payloadData),
		Signature: base64.StdEncoding.EncodeToString(signature),
	}
	data, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func testEntry() Entry {
	return Entry{
		ID: "weather", Version: "1.2.3", OS: "linux", Arch: "amd64",
		URL:          "https://downloads.example.test/weather.tar.gz",
		SHA256:       strings.Repeat("a", 64),
		SignatureURL: "https://downloads.example.test/weather.sig.json", KeyID: "root-1",
	}
}

func TestVerifyIndexAndArchive(t *testing.T) {
	store, private := testRoot(t)
	entry := testEntry()
	index := Index{Format: Format, Publisher: "example", GeneratedAt: "2026-11-12T00:00:00Z", Entries: []Entry{entry}}
	indexData := testEnvelope(t, private, "root-1", index)
	verified, err := VerifyIndex(indexData, store)
	if err != nil || len(verified.Entries) != 1 || verified.Entries[0] != entry {
		t.Fatalf("index verification failed: %v", err)
	}

	archive := []byte("plugin archive")
	digest := sha256.Sum256(archive)
	entry.SHA256 = fmt.Sprintf("%x", digest[:])
	name := archiveNameCandidates(entry.ID, entry.Version, entry.OS, entry.Arch)[0]
	metadata := ArchiveMetadata{ID: entry.ID, Version: entry.Version, OS: entry.OS, Arch: entry.Arch, Archive: name, ArchiveSHA256: entry.SHA256, ArchiveSize: int64(len(archive))}
	dir := t.TempDir()
	archivePath := filepath.Join(dir, name)
	if err := os.WriteFile(archivePath, archive, 0600); err != nil {
		t.Fatal(err)
	}
	signatureData := testEnvelope(t, private, "root-1", metadata)
	if err := VerifyArchive(archivePath, signatureData, entry, store); err != nil {
		t.Fatalf("archive verification failed: %v", err)
	}
}

func TestVerifyRejectsUnknownRootAndWrongSignature(t *testing.T) {
	store, private := testRoot(t)
	index := Index{Format: Format, Publisher: "example", GeneratedAt: "2026-11-12T00:00:00Z", Entries: []Entry{testEntry()}}
	unknown := testEnvelope(t, private, "other-root", index)
	if _, err := VerifyIndex(unknown, store); err == nil {
		t.Fatal("unknown root accepted")
	}
	data := testEnvelope(t, private, "root-1", index)
	var envelope SignedEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatal(err)
	}
	signature, _ := base64.StdEncoding.DecodeString(envelope.Signature)
	signature[0] ^= 0x80
	envelope.Signature = base64.StdEncoding.EncodeToString(signature)
	wrong, _ := json.Marshal(envelope)
	if _, err := VerifyIndex(wrong, store); err == nil {
		t.Fatal("wrong signature accepted")
	}
}

func TestStrictJSONAndValidation(t *testing.T) {
	store, private := testRoot(t)
	index := Index{Format: Format, Publisher: "example", GeneratedAt: "2026-11-12T00:00:00Z", Entries: []Entry{testEntry()}}
	data := testEnvelope(t, private, "root-1", index)
	for _, malformed := range []string{
		`{"format":1,"key_id":"root-1","key_id":"root-1","payload":"x","signature":"x"}`,
		`{"format":1,"key_id":"root-1","payload":"x","signature":"x","extra":true}`,
		`{"FORMAT":1,"key_id":"root-1","payload":"x","signature":"x"}`,
		string(data) + ` {}`,
	} {
		if _, err := VerifyIndex([]byte(malformed), store); err == nil {
			t.Fatalf("malformed signed JSON accepted: %s", malformed)
		}
	}
	for _, invalid := range []Entry{
		func() Entry { e := testEntry(); e.ID = "Bad"; return e }(),
		func() Entry { e := testEntry(); e.Version = "../unsafe"; return e }(),
		func() Entry { e := testEntry(); e.OS = "windows"; return e }(),
		func() Entry { e := testEntry(); e.URL = "http://example.test/a"; return e }(),
		func() Entry { e := testEntry(); e.SignatureURL = "https://"; return e }(),
		func() Entry { e := testEntry(); e.SHA256 = strings.Repeat("A", 64); return e }(),
	} {
		if err := ValidateEntry(invalid); err == nil {
			t.Fatalf("invalid entry accepted: %+v", invalid)
		}
	}
}

func TestVerifyArchiveRejectsIdentityDigestSizeAndName(t *testing.T) {
	store, private := testRoot(t)
	entry := testEntry()
	archive := []byte("plugin archive")
	digest := sha256.Sum256(archive)
	entry.SHA256 = fmt.Sprintf("%x", digest[:])
	name := archiveNameCandidates(entry.ID, entry.Version, entry.OS, entry.Arch)[0]
	dir := t.TempDir()
	archivePath := filepath.Join(dir, name)
	if err := os.WriteFile(archivePath, archive, 0600); err != nil {
		t.Fatal(err)
	}
	metadata := ArchiveMetadata{ID: entry.ID, Version: entry.Version, OS: entry.OS, Arch: entry.Arch, Archive: name, ArchiveSHA256: entry.SHA256, ArchiveSize: int64(len(archive))}
	cases := []struct {
		name string
		edit func(*ArchiveMetadata, *Entry, string)
	}{
		{"identity", func(m *ArchiveMetadata, e *Entry, _ string) { m.ID = "other" }},
		{"digest", func(m *ArchiveMetadata, e *Entry, _ string) { m.ArchiveSHA256 = strings.Repeat("b", 64) }},
		{"size", func(m *ArchiveMetadata, e *Entry, _ string) { m.ArchiveSize++ }},
		{"name", func(m *ArchiveMetadata, e *Entry, _ string) { m.Archive = "other-1.2.3-linux-amd64.tar.gz" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mutated := metadata
			mutatedEntry := entry
			tc.edit(&mutated, &mutatedEntry, name)
			data := testEnvelope(t, private, "root-1", mutated)
			if err := VerifyArchive(archivePath, data, mutatedEntry, store); err == nil {
				t.Fatal("mismatched archive accepted")
			}
		})
	}
}

func TestTrustStorePermissionsAndAtomicSave(t *testing.T) {
	store, _ := testRoot(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "roots.json")
	if err := SaveTrustStore(path, store); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadTrustStore(path)
	if err != nil || len(loaded.Roots) != 1 {
		t.Fatalf("trust store load failed: %v", err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("trust store mode = %o, err=%v", info.Mode().Perm(), err)
		}
		parent, err := os.Stat(filepath.Dir(path))
		if err != nil || parent.Mode().Perm() != 0700 {
			t.Fatalf("trust store parent mode = %o, err=%v", parent.Mode().Perm(), err)
		}
	}
	old := []byte("old")
	if err := os.WriteFile(path, old, 0600); err != nil {
		t.Fatal(err)
	}
	if err := SaveTrustStore(path, store); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) == string(old) {
		t.Fatalf("atomic replacement failed: %v", err)
	}
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".trust-store-*"))
	if err != nil || len(matches) != 0 {
		t.Fatalf("temporary trust-store files remain: %v, %v", matches, err)
	}
}
