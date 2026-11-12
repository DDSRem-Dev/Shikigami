// Package pluginmarket authenticates signed third-party plugin marketplace
// indexes and archives against an explicitly configured Ed25519 trust store.
package pluginmarket

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"github.com/DDSRem-Dev/Shikigami/internal/strictjson"
)

const (
	// Format is the signed plugin distribution format version.
	Format = 1

	// These limits bound attacker-controlled metadata before it is decoded or
	// authenticated. Archive contents are streamed separately by VerifyArchive.
	MaxEnvelopeBytes         = 1 << 20
	MaxIndexPayloadBytes     = 1 << 20
	MaxArchiveSignatureBytes = 256 << 10
	MaxTrustStoreBytes       = 1 << 20
	MaxArchiveBytes          = int64(8 << 30)
	MaxEntries               = 10000
	MaxURLLength             = 2048
	MaxVersionLength         = 80
	MaxPublisherLength       = 256
	MaxGeneratedAtLength     = 128
	MaxKeyIDLength           = 64
	MaxArchiveNameLength     = 256

	signatureDomain = "Shikigami plugin distribution\x00format=1\x00"
)

var (
	pluginIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}[a-z0-9]$|^[a-z]$`)
	keyIDPattern    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	versionPattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,79}$`)
	digestPattern   = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// SignedEnvelope is the authenticated JSON wrapper used by indexes and
// archive metadata. Payload and Signature are canonical standard base64.
type SignedEnvelope struct {
	Format    int    `json:"format"`
	KeyID     string `json:"key_id"`
	Payload   string `json:"payload"`
	Signature string `json:"signature"`
}

// Index is the signed marketplace index payload.
type Index struct {
	Format      int     `json:"format"`
	Publisher   string  `json:"publisher"`
	GeneratedAt string  `json:"generated_at"`
	Entries     []Entry `json:"entries"`
}

// Entry describes one platform-specific plugin archive.
type Entry struct {
	ID           string `json:"id"`
	Version      string `json:"version"`
	OS           string `json:"os"`
	Arch         string `json:"arch"`
	URL          string `json:"url"`
	SHA256       string `json:"sha256"`
	SignatureURL string `json:"signature_url"`
	KeyID        string `json:"key_id"`
}

// ArchiveMetadata is the signed identity and digest of an archive.
type ArchiveMetadata struct {
	ID            string `json:"id"`
	Version       string `json:"version"`
	OS            string `json:"os"`
	Arch          string `json:"arch"`
	Archive       string `json:"archive"`
	ArchiveSHA256 string `json:"archive_sha256"`
	ArchiveSize   int64  `json:"archive_size"`
}

// TrustRoot is one named SPKI Ed25519 public key. PublicKey contains one PEM
// block and never a private key.
type TrustRoot struct {
	KeyID     string `json:"key_id"`
	PublicKey string `json:"public_key"`
}

// TrustStore is the set of explicitly trusted marketplace signing roots.
type TrustStore struct {
	Format int         `json:"format"`
	Roots  []TrustRoot `json:"roots"`
}

func decodeExactObject(data []byte, keys []string, target any) error {
	var fields map[string]json.RawMessage
	if len(data) == 0 || strictjson.Decode(data, &fields) != nil || fields == nil || len(fields) != len(keys) {
		return errors.New("invalid JSON object")
	}
	allowed := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		allowed[key] = struct{}{}
		value, ok := fields[key]
		if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return errors.New("missing, unknown or null JSON field")
		}
	}
	for key := range fields {
		if _, ok := allowed[key]; !ok {
			return errors.New("unknown JSON field")
		}
	}
	if err := strictjson.Decode(data, target); err != nil {
		return errors.New("invalid JSON fields")
	}
	return nil
}

func decodeCanonicalBase64(text string, max int) ([]byte, error) {
	if text == "" || len(text) > max*2 {
		return nil, errors.New("invalid canonical base64")
	}
	decoded, err := base64.StdEncoding.Strict().DecodeString(text)
	if err != nil || len(decoded) == 0 || len(decoded) > max || base64.StdEncoding.EncodeToString(decoded) != text {
		return nil, errors.New("invalid canonical base64")
	}
	return decoded, nil
}

func canonicalJSON(value any, data []byte) error {
	encoded, err := json.Marshal(value)
	if err != nil || !bytes.Equal(encoded, data) {
		return errors.New("signed payload is not canonical JSON")
	}
	return nil
}

// ValidatePluginID validates the lowercase plugin identifier grammar used by
// manifests and marketplace entries.
func ValidatePluginID(id string) error {
	if !pluginIDPattern.MatchString(id) {
		return errors.New("invalid plugin ID")
	}
	return nil
}

func validateKeyID(id string) error {
	if len(id) == 0 || len(id) > MaxKeyIDLength || !keyIDPattern.MatchString(id) {
		return errors.New("invalid signing key ID")
	}
	return nil
}

func validateBoundedText(value string, max int, requireNonEmpty bool) error {
	if requireNonEmpty && value == "" {
		return errors.New("required text is empty")
	}
	if len(value) > max {
		return errors.New("text is too long")
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return errors.New("text contains unsafe characters")
		}
	}
	return nil
}

func validateHTTPSURL(value string) error {
	if err := validateBoundedText(value, MaxURLLength, true); err != nil {
		return errors.New("invalid HTTPS URL")
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Opaque != "" || parsed.Fragment != "" {
		return errors.New("invalid HTTPS URL")
	}
	if parsed.Hostname() == "" {
		return errors.New("invalid HTTPS URL")
	}
	return nil
}

// ValidateEntry checks the untrusted, signed fields of a marketplace entry.
func ValidateEntry(entry Entry) error {
	if err := ValidatePluginID(entry.ID); err != nil {
		return err
	}
	if !versionPattern.MatchString(entry.Version) || len(entry.Version) > MaxVersionLength {
		return errors.New("invalid plugin version")
	}
	if entry.OS != "linux" && entry.OS != "darwin" {
		return errors.New("invalid plugin OS")
	}
	if entry.Arch != "amd64" && entry.Arch != "arm64" {
		return errors.New("invalid plugin architecture")
	}
	if err := validateHTTPSURL(entry.URL); err != nil {
		return err
	}
	if !digestPattern.MatchString(entry.SHA256) {
		return errors.New("invalid archive SHA-256")
	}
	if err := validateHTTPSURL(entry.SignatureURL); err != nil {
		return err
	}
	if err := validateKeyID(entry.KeyID); err != nil {
		return err
	}
	return nil
}

func validateIndex(index Index) error {
	if index.Format != Format {
		return errors.New("unsupported index format")
	}
	if err := validateBoundedText(index.Publisher, MaxPublisherLength, true); err != nil {
		return errors.New("invalid index publisher")
	}
	if err := validateBoundedText(index.GeneratedAt, MaxGeneratedAtLength, true); err != nil {
		return errors.New("invalid index timestamp")
	}
	if len(index.Entries) > MaxEntries {
		return errors.New("too many index entries")
	}
	seen := make(map[string]struct{}, len(index.Entries))
	for _, entry := range index.Entries {
		if err := ValidateEntry(entry); err != nil {
			return err
		}
		identity := entry.ID + "\x00" + entry.Version + "\x00" + entry.OS + "\x00" + entry.Arch
		if _, ok := seen[identity]; ok {
			return errors.New("duplicate marketplace entry")
		}
		seen[identity] = struct{}{}
	}
	return nil
}

func validateArchiveMetadata(metadata ArchiveMetadata) error {
	if err := ValidatePluginID(metadata.ID); err != nil {
		return err
	}
	if !versionPattern.MatchString(metadata.Version) || len(metadata.Version) > MaxVersionLength {
		return errors.New("invalid archive version")
	}
	if metadata.OS != "linux" && metadata.OS != "darwin" {
		return errors.New("invalid archive OS")
	}
	if metadata.Arch != "amd64" && metadata.Arch != "arm64" {
		return errors.New("invalid archive architecture")
	}
	if !digestPattern.MatchString(metadata.ArchiveSHA256) {
		return errors.New("invalid archive SHA-256")
	}
	if metadata.ArchiveSize < 1 || metadata.ArchiveSize > MaxArchiveBytes {
		return errors.New("invalid archive size")
	}
	if err := validateArchiveName(metadata.Archive); err != nil {
		return err
	}
	return nil
}

func validateArchiveName(name string) error {
	if name == "" || len(name) > MaxArchiveNameLength || filepath.Base(name) != name || strings.ContainsAny(name, `/\\`) || name == "." || name == ".." {
		return errors.New("invalid archive filename")
	}
	for _, r := range name {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return errors.New("invalid archive filename")
		}
	}
	if !strings.HasSuffix(name, ".tar.gz") {
		return errors.New("invalid archive filename")
	}
	return nil
}

func digestBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func signedMessage(payload []byte) []byte {
	message := make([]byte, 0, len(signatureDomain)+len(payload))
	message = append(message, signatureDomain...)
	message = append(message, payload...)
	return message
}

func archiveNameCandidates(id, version, targetOS, arch string) []string {
	// The unprefixed name is the canonical marketplace name. The two legacy
	// product prefixes remain accepted so an index can be migrated without
	// making the URL part of archive identity.
	return []string{
		fmt.Sprintf("%s-%s-%s-%s.tar.gz", id, version, targetOS, arch),
		fmt.Sprintf("shiki-plugin-%s-%s-%s-%s.tar.gz", id, version, targetOS, arch),
		fmt.Sprintf("shikigami-plugin-%s-%s-%s-%s.tar.gz", id, version, targetOS, arch),
	}
}

func archiveNameMatches(name, id, version, targetOS, arch string) bool {
	for _, candidate := range archiveNameCandidates(id, version, targetOS, arch) {
		if name == candidate {
			return true
		}
	}
	return false
}
