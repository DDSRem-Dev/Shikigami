package pluginmarket

import (
	"bytes"
	"crypto/ed25519"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/DDSRem-Dev/Shikigami/internal/strictjson"
)

var trustStoreKeys = []string{"format", "roots"}
var trustRootKeys = []string{"key_id", "public_key"}

// NewTrustStore returns an empty trust store using the current format.
func NewTrustStore() TrustStore {
	return TrustStore{Format: Format, Roots: make([]TrustRoot, 0)}
}

// AddRoot validates and adds one SPKI Ed25519 root. A root ID can occur only
// once in a store, even if the public keys differ.
func (s *TrustStore) AddRoot(root TrustRoot) error {
	if s == nil {
		return errors.New("nil trust store")
	}
	if s.Format == 0 {
		s.Format = Format
	}
	if s.Format != Format || len(s.Roots) >= MaxEntries {
		return errors.New("invalid trust store")
	}
	_, der, err := parsePublicKey(root.PublicKey)
	if err != nil || validateKeyID(root.KeyID) != nil {
		return errors.New("invalid trust root")
	}
	for _, existing := range s.Roots {
		if existing.KeyID == root.KeyID {
			return errors.New("duplicate trust root")
		}
	}
	root.PublicKey = canonicalPEM(der)
	s.Roots = append(s.Roots, root)
	return nil
}

// AddRoot adds a root to store. It is also available as a method on TrustStore
// for callers that prefer method-style construction.
func AddRoot(store *TrustStore, root TrustRoot) error {
	if store == nil {
		return errors.New("nil trust store")
	}
	return store.AddRoot(root)
}

func pemBlock(data []byte) ([]byte, error) {
	trimmed := bytes.TrimSpace(data)
	if bytes.Count(trimmed, []byte("-----BEGIN ")) != 1 ||
		bytes.Count(trimmed, []byte("-----END ")) != 1 ||
		!bytes.HasPrefix(trimmed, []byte("-----BEGIN PUBLIC KEY-----")) ||
		!bytes.HasSuffix(trimmed, []byte("-----END PUBLIC KEY-----")) {
		return nil, errors.New("invalid public key PEM")
	}
	block, rest := pem.Decode(trimmed)
	if block == nil || block.Type != "PUBLIC KEY" || len(block.Headers) != 0 || len(bytes.TrimSpace(rest)) != 0 {
		return nil, errors.New("invalid public key PEM")
	}
	return block.Bytes, nil
}

func parsePublicKey(text string) (ed25519.PublicKey, []byte, error) {
	der, err := pemBlock([]byte(text))
	if err != nil {
		return nil, nil, errors.New("invalid trust root")
	}
	key, err := x509.ParsePKIXPublicKey(der)
	public, ok := key.(ed25519.PublicKey)
	if err != nil || !ok || len(public) != ed25519.PublicKeySize {
		return nil, nil, errors.New("invalid trust root")
	}
	canonical, err := x509.MarshalPKIXPublicKey(public)
	if err != nil || !bytes.Equal(canonical, der) {
		return nil, nil, errors.New("invalid trust root")
	}
	return append(ed25519.PublicKey(nil), public...), canonical, nil
}

func canonicalPEM(der []byte) string {
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
}

func decodeTrustStore(data []byte) (TrustStore, error) {
	var wire struct {
		Format int             `json:"format"`
		Roots  json.RawMessage `json:"roots"`
	}
	if err := decodeExactObject(data, trustStoreKeys, &wire); err != nil || wire.Format != Format {
		return TrustStore{}, errors.New("invalid trust store")
	}
	if len(wire.Roots) == 0 || bytes.Equal(bytes.TrimSpace(wire.Roots), []byte("null")) {
		return TrustStore{}, errors.New("invalid trust store roots")
	}
	var rawRoots []json.RawMessage
	if err := strictDecode(wire.Roots, &rawRoots); err != nil || len(rawRoots) > MaxEntries {
		return TrustStore{}, errors.New("invalid trust store roots")
	}
	store := TrustStore{Format: Format, Roots: make([]TrustRoot, 0, len(rawRoots))}
	seen := make(map[string]struct{}, len(rawRoots))
	for _, raw := range rawRoots {
		var root TrustRoot
		if err := decodeExactObject(raw, trustRootKeys, &root); err != nil || validateKeyID(root.KeyID) != nil {
			return TrustStore{}, errors.New("invalid trust root")
		}
		public, der, err := parsePublicKey(root.PublicKey)
		if err != nil {
			return TrustStore{}, errors.New("invalid trust root")
		}
		if _, ok := seen[root.KeyID]; ok {
			return TrustStore{}, errors.New("duplicate trust root")
		}
		seen[root.KeyID] = struct{}{}
		_ = public
		root.PublicKey = canonicalPEM(der)
		store.Roots = append(store.Roots, root)
	}
	return store, nil
}

func strictDecode(data []byte, out any) error {
	return strictjson.Decode(data, out)
}

func normalizedTrustStore(store TrustStore) (TrustStore, error) {
	if store.Format == 0 {
		store.Format = Format
	}
	if store.Format != Format || len(store.Roots) > MaxEntries {
		return TrustStore{}, errors.New("invalid trust store")
	}
	normalized := TrustStore{Format: Format, Roots: make([]TrustRoot, 0, len(store.Roots))}
	seen := make(map[string]struct{}, len(store.Roots))
	for _, root := range store.Roots {
		if validateKeyID(root.KeyID) != nil {
			return TrustStore{}, errors.New("invalid trust root")
		}
		_, der, err := parsePublicKey(root.PublicKey)
		if err != nil {
			return TrustStore{}, errors.New("invalid trust root")
		}
		if _, ok := seen[root.KeyID]; ok {
			return TrustStore{}, errors.New("duplicate trust root")
		}
		seen[root.KeyID] = struct{}{}
		normalized.Roots = append(normalized.Roots, TrustRoot{KeyID: root.KeyID, PublicKey: canonicalPEM(der)})
	}
	return normalized, nil
}

func trustedKeys(store TrustStore) (map[string]ed25519.PublicKey, error) {
	normalized, err := normalizedTrustStore(store)
	if err != nil {
		return nil, err
	}
	keys := make(map[string]ed25519.PublicKey, len(normalized.Roots))
	for _, root := range normalized.Roots {
		public, _, err := parsePublicKey(root.PublicKey)
		if err != nil {
			return nil, errors.New("invalid trust root")
		}
		keys[root.KeyID] = public
	}
	return keys, nil
}

func readBoundedFile(path string, max int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, errors.New("file exceeds metadata limit")
	}
	return data, nil
}

func ensurePrivateParent(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return errors.New("invalid trust store directory")
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return err
	}
	return nil
}

// LoadTrustStore loads and validates a private trust-store file.
func LoadTrustStore(path string) (TrustStore, error) {
	if path == "" {
		return TrustStore{}, errors.New("trust store path is required")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return TrustStore{}, errors.New("invalid trust store file")
	}
	parent, err := os.Stat(filepath.Dir(path))
	if err != nil || !parent.IsDir() || parent.Mode().Perm()&0077 != 0 {
		return TrustStore{}, errors.New("invalid trust store directory")
	}
	data, err := readBoundedFile(path, MaxTrustStoreBytes)
	if err != nil {
		return TrustStore{}, errors.New("unable to read trust store")
	}
	return decodeTrustStore(data)
}

// SaveTrustStore atomically writes a private trust-store file and keeps its
// containing directory private.
func SaveTrustStore(path string, store TrustStore) error {
	if path == "" {
		return errors.New("trust store path is required")
	}
	normalized, err := normalizedTrustStore(store)
	if err != nil {
		return err
	}
	data, err := json.Marshal(normalized)
	if err != nil || len(data) > MaxTrustStoreBytes {
		return errors.New("unable to encode trust store")
	}
	if err := ensurePrivateParent(path); err != nil {
		return errors.New("unable to prepare trust store directory")
	}
	dir := filepath.Dir(path)
	temp, err := os.OpenFile(filepath.Join(dir, ".trust-store.tmp-"+fmt.Sprintf("%d", os.Getpid())), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		// A random temporary name avoids collisions between concurrent saves.
		temp, err = os.CreateTemp(dir, ".trust-store-*")
	}
	if err != nil {
		return errors.New("unable to create trust store temporary file")
	}
	tempName := temp.Name()
	removeTemp := true
	defer func() {
		_ = temp.Close()
		if removeTemp {
			_ = os.Remove(tempName)
		}
	}()
	if err := temp.Chmod(0600); err != nil {
		return errors.New("unable to protect trust store temporary file")
	}
	if _, err := temp.Write(data); err != nil {
		return errors.New("unable to write trust store")
	}
	if err := temp.Sync(); err != nil {
		return errors.New("unable to sync trust store")
	}
	if err := temp.Close(); err != nil {
		return errors.New("unable to close trust store")
	}
	if err := os.Rename(tempName, path); err != nil {
		return errors.New("unable to install trust store")
	}
	removeTemp = false
	return nil
}
