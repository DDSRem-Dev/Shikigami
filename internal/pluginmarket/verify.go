package pluginmarket

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
)

var (
	envelopeKeys        = []string{"format", "key_id", "payload", "signature"}
	indexKeys           = []string{"format", "publisher", "generated_at", "entries"}
	entryKeys           = []string{"id", "version", "os", "arch", "url", "sha256", "signature_url", "key_id"}
	archiveMetadataKeys = []string{"id", "version", "os", "arch", "archive", "archive_sha256", "archive_size"}
)

func parseEnvelope(data []byte, payloadLimit int) (SignedEnvelope, []byte, ed25519.PublicKey, error) {
	var envelope SignedEnvelope
	if len(data) == 0 || len(data) > MaxEnvelopeBytes {
		return SignedEnvelope{}, nil, nil, errors.New("invalid signature envelope")
	}
	if err := decodeExactObject(data, envelopeKeys, &envelope); err != nil || envelope.Format != Format || validateKeyID(envelope.KeyID) != nil {
		return SignedEnvelope{}, nil, nil, errors.New("invalid signature envelope")
	}
	payload, err := decodeCanonicalBase64(envelope.Payload, payloadLimit)
	if err != nil {
		return SignedEnvelope{}, nil, nil, errors.New("invalid signed payload encoding")
	}
	signature, err := decodeCanonicalBase64(envelope.Signature, ed25519.SignatureSize)
	if err != nil || len(signature) != ed25519.SignatureSize {
		return SignedEnvelope{}, nil, nil, errors.New("invalid signature encoding")
	}
	// Signature is checked by the caller after looking up the appropriate root.
	_ = signature
	return envelope, payload, nil, nil
}

func envelopeSignature(data []byte) ([]byte, error) {
	var envelope SignedEnvelope
	if err := decodeExactObject(data, envelopeKeys, &envelope); err != nil {
		return nil, errors.New("invalid signature envelope")
	}
	signature, err := decodeCanonicalBase64(envelope.Signature, ed25519.SignatureSize)
	if err != nil || len(signature) != ed25519.SignatureSize {
		return nil, errors.New("invalid signature encoding")
	}
	return signature, nil
}

func verifyEnvelope(data []byte, store TrustStore, payloadLimit int) (SignedEnvelope, []byte, error) {
	envelope, payload, _, err := parseEnvelope(data, payloadLimit)
	if err != nil {
		return SignedEnvelope{}, nil, err
	}
	keys, err := trustedKeys(store)
	if err != nil {
		return SignedEnvelope{}, nil, errors.New("invalid trust store")
	}
	public, ok := keys[envelope.KeyID]
	if !ok {
		return SignedEnvelope{}, nil, errors.New("unknown signing key")
	}
	signature, err := envelopeSignature(data)
	if err != nil || !ed25519.Verify(public, signedMessage(payload), signature) {
		return SignedEnvelope{}, nil, errors.New("signature authentication failed")
	}
	return envelope, payload, nil
}

type indexWire struct {
	Format      int             `json:"format"`
	Publisher   string          `json:"publisher"`
	GeneratedAt string          `json:"generated_at"`
	Entries     json.RawMessage `json:"entries"`
}

func parseIndexPayload(data []byte) (Index, error) {
	var wire indexWire
	if err := decodeExactObject(data, indexKeys, &wire); err != nil || wire.Format != Format {
		return Index{}, errors.New("invalid index payload")
	}
	if bytes.Equal(bytes.TrimSpace(wire.Entries), []byte("null")) {
		return Index{}, errors.New("invalid index entries")
	}
	var rawEntries []json.RawMessage
	if err := strictDecode(wire.Entries, &rawEntries); err != nil || len(rawEntries) > MaxEntries {
		return Index{}, errors.New("invalid index entries")
	}
	index := Index{Format: wire.Format, Publisher: wire.Publisher, GeneratedAt: wire.GeneratedAt, Entries: make([]Entry, 0, len(rawEntries))}
	for _, raw := range rawEntries {
		var entry Entry
		if err := decodeExactObject(raw, entryKeys, &entry); err != nil {
			return Index{}, errors.New("invalid index entry")
		}
		index.Entries = append(index.Entries, entry)
	}
	if err := validateIndex(index); err != nil {
		return Index{}, errors.New("invalid index payload")
	}
	if err := canonicalJSON(index, data); err != nil {
		return Index{}, err
	}
	return index, nil
}

// VerifyIndex authenticates and strictly validates a signed marketplace index.
func VerifyIndex(data []byte, store TrustStore) (Index, error) {
	_, payload, err := verifyEnvelope(data, store, MaxIndexPayloadBytes)
	if err != nil {
		return Index{}, err
	}
	index, err := parseIndexPayload(payload)
	if err != nil {
		return Index{}, err
	}
	keys, err := trustedKeys(store)
	if err != nil {
		return Index{}, errors.New("invalid trust store")
	}
	for _, entry := range index.Entries {
		if _, ok := keys[entry.KeyID]; !ok {
			return Index{}, errors.New("index references unknown signing key")
		}
	}
	return index, nil
}

type archiveWire struct {
	ID            string `json:"id"`
	Version       string `json:"version"`
	OS            string `json:"os"`
	Arch          string `json:"arch"`
	Archive       string `json:"archive"`
	ArchiveSHA256 string `json:"archive_sha256"`
	ArchiveSize   int64  `json:"archive_size"`
}

func parseArchivePayload(data []byte) (ArchiveMetadata, error) {
	var wire archiveWire
	if err := decodeExactObject(data, archiveMetadataKeys, &wire); err != nil {
		return ArchiveMetadata{}, errors.New("invalid archive metadata")
	}
	metadata := ArchiveMetadata{
		ID: wire.ID, Version: wire.Version, OS: wire.OS, Arch: wire.Arch,
		Archive: wire.Archive, ArchiveSHA256: wire.ArchiveSHA256, ArchiveSize: wire.ArchiveSize,
	}
	if err := validateArchiveMetadata(metadata); err != nil {
		return ArchiveMetadata{}, errors.New("invalid archive metadata")
	}
	if err := canonicalJSON(metadata, data); err != nil {
		return ArchiveMetadata{}, err
	}
	return metadata, nil
}

func archiveFileMatches(path string, metadata ArchiveMetadata, entry Entry) error {
	if !archiveNameMatches(metadata.Archive, entry.ID, entry.Version, entry.OS, entry.Arch) {
		return errors.New("archive filename does not match plugin identity")
	}
	if filepath.Base(path) != metadata.Archive {
		return errors.New("archive filename mismatch")
	}
	return nil
}

// VerifyArchive authenticates signed archive metadata and hashes the archive
// through an open file descriptor. The URL in entry is deliberately not used
// to establish archive identity.
func VerifyArchive(archivePath string, signatureData []byte, entry Entry, store TrustStore) error {
	if archivePath == "" || len(signatureData) == 0 || len(signatureData) > MaxArchiveSignatureBytes {
		return errors.New("invalid archive signature")
	}
	if err := ValidateEntry(entry); err != nil {
		return errors.New("invalid archive entry")
	}
	_, payload, err := verifyEnvelope(signatureData, store, MaxArchiveSignatureBytes)
	if err != nil {
		return err
	}
	var envelope SignedEnvelope
	if err := decodeExactObject(signatureData, envelopeKeys, &envelope); err != nil || envelope.KeyID != entry.KeyID {
		return errors.New("archive signature key mismatch")
	}
	metadata, err := parseArchivePayload(payload)
	if err != nil {
		return err
	}
	if metadata.ID != entry.ID || metadata.Version != entry.Version || metadata.OS != entry.OS || metadata.Arch != entry.Arch {
		return errors.New("archive identity mismatch")
	}
	if err := archiveFileMatches(archivePath, metadata, entry); err != nil {
		return err
	}
	if subtle.ConstantTimeCompare(mustDecodeDigest(metadata.ArchiveSHA256), mustDecodeDigest(entry.SHA256)) != 1 {
		return errors.New("archive digest metadata mismatch")
	}
	info, err := os.Lstat(archivePath)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("invalid archive file")
	}
	file, err := os.Open(archivePath)
	if err != nil {
		return errors.New("unable to open archive")
	}
	defer file.Close()
	openedInfo, err := file.Stat()
	if err != nil || !openedInfo.Mode().IsRegular() || openedInfo.Size() != metadata.ArchiveSize {
		return errors.New("archive size mismatch")
	}
	hasher := sha256.New()
	count, err := io.Copy(hasher, io.LimitReader(file, MaxArchiveBytes+1))
	if err != nil || count != metadata.ArchiveSize {
		return errors.New("archive size mismatch")
	}
	digest := hasher.Sum(nil)
	expected := mustDecodeDigest(metadata.ArchiveSHA256)
	if len(expected) != len(digest) || subtle.ConstantTimeCompare(digest, expected) != 1 {
		return errors.New("archive digest mismatch")
	}
	return nil
}

func mustDecodeDigest(text string) []byte {
	decoded, err := hex.DecodeString(text)
	if err != nil {
		return nil
	}
	return decoded
}
