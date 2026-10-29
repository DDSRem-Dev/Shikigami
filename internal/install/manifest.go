// Package install verifies explicitly trusted local release bundles and manages
// versioned local installations. It never discovers or invents a release source.
package install

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/DDSRem-Dev/Shikigami/internal/strictjson"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"syscall"

	host "github.com/DDSRem-Dev/Shikigami/internal/plugin"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	"github.com/DDSRem-Dev/Shikigami/pkg/protocol/version"
)

const ManifestName = "release.json"
const ChecksumsName = "SHA256SUMS"
const MaxArtifactBytes = 1 << 30

var releaseVersion = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,79}$`)
var binaryNames = []string{"shiki", "shiki-agent", "shiki-daemon", "shiki-install", "shiki-plugin-sync", "shiki-plugin-port"}
var auxiliaryNames = []string{"sync.manifest.json", "port.manifest.json", "install.sh", "install-client.sh", "uninstall.sh", "LICENSE"}

type File struct {
	Name       string `json:"name"`
	SHA256     string `json:"sha256"`
	Size       int64  `json:"size"`
	Executable bool   `json:"executable"`
}
type Manifest struct {
	Format        int    `json:"format"`
	Version       string `json:"version"`
	OS            string `json:"os"`
	Arch          string `json:"arch"`
	ProtocolMajor uint32 `json:"protocol_major"`
	ConfigSchema  int    `json:"config_schema"`
	StateSchema   int    `json:"state_schema"`
	Files         []File `json:"files"`
}
type Bundle struct {
	Directory        string
	Manifest         Manifest
	TrustedChecksums string
}

func names() []string { return append(append([]string{}, binaryNames...), auxiliaryNames...) }
func executable(name string) bool {
	for _, n := range binaryNames {
		if n == name {
			return true
		}
	}
	return strings.HasSuffix(name, ".sh")
}
func digest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func readRegular(directory, name string, limit int64) ([]byte, error) {
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	info, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return nil, fmt.Errorf("unsafe or oversized release file: %s", name)
	}
	file, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !os.SameFile(info, opened) {
		return nil, errors.New("release file changed during open")
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if int64(len(data)) > limit {
		return nil, errors.New("release file exceeds limit")
	}
	return data, err
}
func decode(data []byte, out any) error { return strictjson.Decode(data, out) }
func parseChecksums(data []byte) (map[string]string, error) {
	out := map[string]string{}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 2 || len(fields[0]) != 64 || filepath.Base(fields[1]) != fields[1] || strings.ContainsAny(fields[1], "\\\x00") {
			return nil, errors.New("invalid checksums manifest")
		}
		if _, err := hex.DecodeString(fields[0]); err != nil {
			return nil, errors.New("invalid digest")
		}
		if _, ok := out[fields[1]]; ok {
			return nil, errors.New("duplicate checksum entry")
		}
		out[fields[1]] = fields[0]
	}
	return out, scanner.Err()
}

// Verify requires a digest obtained independently by the operator. Computing a
// digest from the untrusted bundle itself is deliberately not a trust option.
func Verify(directory, expectedVersion, trustedChecksums string) (*Bundle, error) {
	if !filepath.IsAbs(directory) || !releaseVersion.MatchString(expectedVersion) || len(trustedChecksums) != 64 {
		return nil, errors.New("absolute bundle, fixed version and trusted --checksums-sha256 are required")
	}
	if _, err := hex.DecodeString(trustedChecksums); err != nil {
		return nil, errors.New("invalid trusted checksum")
	}
	sums, err := readRegular(directory, ChecksumsName, 64<<10)
	if err != nil {
		return nil, err
	}
	if digest(sums) != trustedChecksums {
		return nil, errors.New("release checksum manifest does not match trusted digest")
	}
	checksums, err := parseChecksums(sums)
	if err != nil {
		return nil, err
	}
	data, err := readRegular(directory, ManifestName, 64<<10)
	if err != nil {
		return nil, err
	}
	if digest(data) != checksums[ManifestName] {
		return nil, errors.New("release manifest checksum mismatch")
	}
	var m Manifest
	if err = decode(data, &m); err != nil {
		return nil, err
	}
	if m.Format != 1 || m.Version != expectedVersion || m.OS != runtime.GOOS || m.Arch != runtime.GOARCH || m.ProtocolMajor != 1 || m.ConfigSchema != 1 || m.StateSchema != 1 {
		return nil, errors.New("release version, platform or schema is incompatible")
	}
	expected := map[string]bool{}
	for _, name := range names() {
		expected[name] = true
	}
	if len(m.Files) != len(expected) || len(checksums) != len(expected)+1 {
		return nil, errors.New("incomplete or unexpected release files")
	}
	for _, entry := range m.Files {
		if !expected[entry.Name] || entry.Executable != executable(entry.Name) || entry.Size < 1 || entry.Size > MaxArtifactBytes || checksums[entry.Name] != entry.SHA256 {
			return nil, errors.New("invalid release file declaration")
		}
		delete(expected, entry.Name)
		if err = verifyFile(directory, entry); err != nil {
			return nil, err
		}
	}
	for _, id := range []string{"sync", "port"} {
		data, err := readRegular(directory, id+".manifest.json", 64<<10)
		if err != nil {
			return nil, err
		}
		var p host.Manifest
		if err = decode(data, &p); err != nil {
			return nil, err
		}
		if p.Name != id || p.Version != m.Version || p.Side != "both" || p.Executable != "shiki-plugin-"+id || p.SHA256 != checksums[p.Executable] {
			return nil, errors.New("plugin manifest mismatch")
		}
		for _, side := range []string{"client", "server"} {
			contract, _ := sdk.BuiltinProtocol(id, side)
			if _, err = version.Negotiate(contract, p.Protocol, m.Version); err != nil {
				return nil, err
			}
		}
	}
	return &Bundle{Directory: directory, Manifest: m, TrustedChecksums: trustedChecksums}, nil
}
func verifyFile(directory string, entry File) error {
	root, err := os.OpenRoot(directory)
	if err != nil {
		return err
	}
	defer root.Close()
	info, err := root.Lstat(entry.Name)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() != entry.Size {
		return fmt.Errorf("unsafe release payload: %s", entry.Name)
	}
	file, err := root.OpenFile(entry.Name, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return errors.New("release payload changed during open")
	}
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(file, entry.Size+1))
	if err != nil {
		return err
	}
	if n != entry.Size || hex.EncodeToString(h.Sum(nil)) != entry.SHA256 {
		return fmt.Errorf("release payload checksum mismatch: %s", entry.Name)
	}
	return nil
}

// CreateManifest records locally built payloads. The returned digest identifies
// this local build; it is not a claim of publisher signatures or provenance.
func CreateManifest(directory, build, goos, goarch string) (string, error) {
	if !releaseVersion.MatchString(build) || (goos != "linux" && goos != "darwin") || (goarch != "arm64" && goarch != "amd64") {
		return "", errors.New("unsupported release version or platform")
	}
	m := Manifest{Format: 1, Version: build, OS: goos, Arch: goarch, ProtocolMajor: 1, ConfigSchema: 1, StateSchema: 1}
	sums := map[string]string{}
	for _, name := range names() {
		file, err := os.OpenFile(filepath.Join(directory, name), os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
		if err != nil {
			return "", err
		}
		info, err := file.Stat()
		if err != nil {
			file.Close()
			return "", err
		}
		if !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > MaxArtifactBytes {
			file.Close()
			return "", errors.New("invalid build payload")
		}
		h := sha256.New()
		n, err := io.Copy(h, io.LimitReader(file, MaxArtifactBytes+1))
		file.Close()
		if err != nil || !info.Mode().IsRegular() || n < 1 || n > MaxArtifactBytes {
			return "", errors.New("invalid build payload")
		}
		hash := hex.EncodeToString(h.Sum(nil))
		sums[name] = hash
		m.Files = append(m.Files, File{Name: name, SHA256: hash, Size: n, Executable: executable(name)})
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return "", err
	}
	data = append(data, '\n')
	if err = os.WriteFile(filepath.Join(directory, ManifestName), data, 0600); err != nil {
		return "", err
	}
	sums[ManifestName] = digest(data)
	ordered := make([]string, 0, len(sums))
	for name := range sums {
		ordered = append(ordered, name)
	}
	sort.Strings(ordered)
	var b strings.Builder
	for _, name := range ordered {
		fmt.Fprintf(&b, "%s  %s\n", sums[name], name)
	}
	data = []byte(b.String())
	if err = os.WriteFile(filepath.Join(directory, ChecksumsName), data, 0600); err != nil {
		return "", err
	}
	return digest(data), nil
}
