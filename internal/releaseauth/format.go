// Package releaseauth authenticates local or HTTPS release archives using an
// explicitly supplied Ed25519 trust key. It never unpacks, executes or installs.
package releaseauth

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"regexp"

	"github.com/DDSRem-Dev/Shikigami/internal/strictjson"
)

const (
	Format           = 1
	MaxMetadataBytes = 64 << 10
	MaxArchiveBytes  = int64(8 << 30)
	// The format number is authenticated even though the envelope also carries
	// it for dispatch. These bytes must never be reused for another signature.
	signatureDomain = "Shikigami release archive signature\x00format=1\x00"
)

var (
	versionPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,79}$`)
	digestPattern  = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

type envelope struct {
	Format    int    `json:"format"`
	Payload   string `json:"payload"`
	Signature string `json:"signature"`
}

type payload struct {
	Version         string `json:"version"`
	OS              string `json:"os"`
	Arch            string `json:"arch"`
	Archive         string `json:"archive"`
	ArchiveSHA256   string `json:"archive_sha256"`
	ArchiveSize     int64  `json:"archive_size"`
	ChecksumsSHA256 string `json:"checksums_sha256"`
}

func archiveName(version, targetOS, arch string) (string, error) {
	if !versionPattern.MatchString(version) || (targetOS != "linux" && targetOS != "darwin") || (arch != "amd64" && arch != "arm64") {
		return "", errors.New("explicit valid release version, OS and architecture are required")
	}
	return fmt.Sprintf("shikigami-%s-%s-%s.tar.gz", version, targetOS, arch), nil
}

func (p payload) validate() error {
	name, err := archiveName(p.Version, p.OS, p.Arch)
	if err != nil || p.Archive != name || p.ArchiveSize < 1 || p.ArchiveSize > MaxArchiveBytes || !digestPattern.MatchString(p.ArchiveSHA256) || !digestPattern.MatchString(p.ChecksumsSHA256) {
		return errors.New("invalid signed release metadata")
	}
	return nil
}

// encoding/json accepts case-insensitive field aliases. Check the exact key
// set as well as strictjson's duplicate/unknown/trailing-content checks.
func decodeObject(data []byte, keys []string, target any) error {
	var fields map[string]json.RawMessage
	if err := strictjson.Decode(data, &fields); err != nil || len(fields) != len(keys) {
		return errors.New("invalid JSON object")
	}
	for _, key := range keys {
		value, ok := fields[key]
		if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return errors.New("missing, unknown or null JSON field")
		}
	}
	if err := strictjson.Decode(data, target); err != nil {
		return errors.New("invalid JSON fields")
	}
	return nil
}

func decodeBase64(text string) ([]byte, error) {
	data, err := base64.StdEncoding.Strict().DecodeString(text)
	if err != nil || len(data) == 0 || base64.StdEncoding.EncodeToString(data) != text {
		return nil, errors.New("invalid canonical base64")
	}
	return data, nil
}

func signedMessage(data []byte) []byte {
	return append([]byte(signatureDomain), data...)
}

func authenticate(data []byte, public ed25519.PublicKey) (payload, error) {
	var result payload
	var outer envelope
	if err := decodeObject(data, []string{"format", "payload", "signature"}, &outer); err != nil || outer.Format != Format {
		return result, errors.New("invalid signature envelope")
	}
	message, err := decodeBase64(outer.Payload)
	if err != nil || len(message) > MaxMetadataBytes {
		return result, errors.New("invalid signed payload encoding")
	}
	signature, err := decodeBase64(outer.Signature)
	if err != nil || len(signature) != ed25519.SignatureSize || !ed25519.Verify(public, signedMessage(message), signature) {
		return result, errors.New("signature authentication failed")
	}
	if err := decodeObject(message, []string{"version", "os", "arch", "archive", "archive_sha256", "archive_size", "checksums_sha256"}, &result); err != nil {
		return payload{}, errors.New("invalid signed payload")
	}
	if err := result.validate(); err != nil {
		return payload{}, err
	}
	return result, nil
}

func pemBlock(data []byte, kind string) ([]byte, error) {
	trimmed := bytes.TrimSpace(data)
	// pem.Decode scans past malformed leading blocks. Require exactly one
	// BEGIN/END pair before decoding so that recovery cannot hide prefix junk.
	if bytes.Count(trimmed, []byte("-----BEGIN ")) != 1 || bytes.Count(trimmed, []byte("-----END ")) != 1 || !bytes.HasPrefix(trimmed, []byte("-----BEGIN "+kind+"-----")) || !bytes.HasSuffix(trimmed, []byte("-----END "+kind+"-----")) {
		return nil, errors.New("invalid key PEM")
	}
	block, rest := pem.Decode(trimmed)
	if block == nil || block.Type != kind || len(block.Headers) != 0 || len(bytes.TrimSpace(rest)) != 0 {
		return nil, errors.New("invalid key PEM")
	}
	return block.Bytes, nil
}

func parsePrivate(data []byte) (ed25519.PrivateKey, error) {
	der, err := pemBlock(data, "PRIVATE KEY")
	if err != nil {
		return nil, errors.New("private key must be one unencrypted PKCS8 Ed25519 PEM block")
	}
	defer clear(der)
	key, err := x509.ParsePKCS8PrivateKey(der)
	private, ok := key.(ed25519.PrivateKey)
	if err != nil || !ok || len(private) != ed25519.PrivateKeySize {
		return nil, errors.New("private key must be PKCS8 Ed25519")
	}
	derived := ed25519.NewKeyFromSeed(private.Seed())
	defer clear(derived)
	canonical, marshalErr := x509.MarshalPKCS8PrivateKey(private)
	defer clear(canonical)
	if marshalErr != nil || !bytes.Equal(private, derived) || !bytes.Equal(canonical, der) {
		clear(private)
		return nil, errors.New("private key must contain canonical consistent Ed25519 material")
	}
	return private, nil
}

func parsePublic(data []byte) (ed25519.PublicKey, string, error) {
	der, err := pemBlock(data, "PUBLIC KEY")
	if err != nil {
		return nil, "", errors.New("public key must be one SPKI Ed25519 PEM block")
	}
	key, err := x509.ParsePKIXPublicKey(der)
	public, ok := key.(ed25519.PublicKey)
	if err != nil || !ok || len(public) != ed25519.PublicKeySize {
		return nil, "", errors.New("public key must be SPKI Ed25519")
	}
	canonical, err := x509.MarshalPKIXPublicKey(public)
	if err != nil || !bytes.Equal(canonical, der) {
		return nil, "", errors.New("public key DER is not canonical SPKI")
	}
	sum := sha256.Sum256(canonical)
	return public, hex.EncodeToString(sum[:]), nil
}
