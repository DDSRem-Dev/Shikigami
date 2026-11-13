package plugin

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"syscall"

	"github.com/DDSRem-Dev/Shikigami/internal/strictjson"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
)

var pluginIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}[a-z0-9]$|^[a-z]$`)

func builtinID(id string) bool { return id == "sync" || id == "port" }

type artifactIntegrity struct {
	ManifestSHA256 string `json:"manifest_sha256"`
	BinarySHA256   string `json:"binary_sha256"`
}

// readThirdPartyArtifact verifies the installer's local integrity receipt, not
// publisher signatures. Manual installation is explicit user trust; marketplace
// provenance is verified before publication by the installer. The receipt pins
// exact bytes across discovery, process launch and host restarts.
func readThirdPartyArtifact(directory, id, side string) (Manifest, error) {
	var result Manifest
	if !pluginIDPattern.MatchString(id) || builtinID(id) || filepath.Base(directory) != id || (side != "client" && side != "server") {
		return result, errors.New("invalid third-party plugin location or side")
	}
	if err := validateArtifactDirectory(directory); err != nil {
		return result, err
	}
	before, err := os.Lstat(directory)
	if err != nil {
		return result, err
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return result, err
	}
	defer root.Close()
	manifestFile, err := openArtifactFile(root, "manifest.json", 64<<10, false)
	if err != nil {
		return result, err
	}
	data, err := io.ReadAll(io.LimitReader(manifestFile, (64<<10)+1))
	manifestFile.Close()
	if err != nil {
		return result, err
	}
	if len(data) > 64<<10 {
		return result, errors.New("plugin manifest exceeds limit")
	}
	var declaration sdk.PluginManifest
	if err := strictjson.Decode(data, &declaration); err != nil {
		return result, err
	}
	if err := sdk.ValidateManifest(&declaration); err != nil {
		return result, err
	}
	if declaration.ID != id {
		return result, errors.New("plugin ID must match its directory")
	}
	if !slices.Contains(declaration.Sides, side) {
		return result, fmt.Errorf("%w: plugin does not support %s", ErrIncompatible, side)
	}
	if !slices.Contains(declaration.Capabilities, sdk.CoreCapability) {
		return result, fmt.Errorf("%w: plugin requires %s", ErrIncompatible, sdk.CoreCapability)
	}
	receiptFile, err := openArtifactFile(root, ".artifact-integrity.json", 1024, false)
	if err != nil {
		return result, err
	}
	receiptData, err := io.ReadAll(io.LimitReader(receiptFile, 1025))
	receiptFile.Close()
	if err != nil {
		return result, err
	}
	var receipt artifactIntegrity
	if len(receiptData) > 1024 {
		return result, errors.New("plugin integrity receipt exceeds limit")
	}
	if err := strictjson.Decode(receiptData, &receipt); err != nil {
		return result, err
	}
	manifestSum := sha256.Sum256(data)
	manifestDigest := hex.EncodeToString(manifestSum[:])
	binaryDigest, err := hex.DecodeString(receipt.BinarySHA256)
	if err != nil || len(binaryDigest) != sha256.Size || receipt.ManifestSHA256 != manifestDigest {
		return result, errors.New("plugin manifest integrity mismatch")
	}
	binary, err := openArtifactFile(root, declaration.Binary, 256<<20, true)
	if err != nil {
		return result, err
	}
	hash := sha256.New()
	n, err := io.Copy(hash, io.LimitReader(binary, (256<<20)+1))
	binary.Close()
	if err != nil {
		return result, err
	}
	if n == 0 || n > 256<<20 || hex.EncodeToString(hash.Sum(nil)) != receipt.BinarySHA256 {
		return result, errors.New("plugin binary integrity mismatch")
	}
	after, err := os.Lstat(directory)
	if err != nil {
		return result, err
	}
	if !os.SameFile(before, after) {
		return result, errors.New("plugin changed during discovery")
	}
	// Manifest schema 1 uses the public SDK's protocol 1.0. Software versions
	// are independent of the host build, but must match GetInfo exactly. Every
	// declared capability is required; undeclared runtime capabilities are not
	// negotiated, and the manifest's action contract cannot be silently omitted.
	return Manifest{
		Name: id, Version: declaration.Version, Executable: filepath.Join(directory, declaration.Binary),
		SHA256: receipt.BinarySHA256, Side: side, PluginManifest: &declaration, manifestSHA256: manifestDigest,
		Protocol: &pb.ProtocolOffer{Major: 1, Capabilities: declaration.Capabilities, RequiredCapabilities: declaration.Capabilities},
	}, nil
}

func openArtifactFile(root *os.Root, name string, limit int64, executable bool) (*os.File, error) {
	file, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, err
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || info.Mode().Perm()&0022 != 0 || (owner.Uid != 0 && owner.Uid != uint32(os.Getuid())) || info.Size() > limit || (executable && info.Mode().Perm()&0111 == 0) {
		file.Close()
		return nil, errors.New("unsafe plugin artifact")
	}
	return file, nil
}

func validateArtifactDirectory(directory string) error {
	if !filepath.IsAbs(directory) {
		return errors.New("plugin artifact directory must be absolute")
	}
	info, err := os.Lstat(directory)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("plugin artifact directory must not be a symlink")
	}
	return validateArtifactRoot(directory)
}

// Installed releases historically point plugins_dir at a managed release
// symlink. Accept a protected configured root, but never a symlink package.
func validateArtifactRoot(directory string) error {
	if !filepath.IsAbs(directory) {
		return errors.New("plugin artifact directory must be absolute")
	}
	info, err := os.Stat(directory)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("plugin artifact root must be a directory")
	}
	if err := validateArtifactParents(directory); err != nil {
		return err
	}
	resolved, err := filepath.EvalSymlinks(directory)
	if err != nil {
		return err
	}
	if resolved != directory {
		return validateArtifactParents(resolved)
	}
	return nil
}

func validateArtifactParents(parent string) error {
	for {
		info, err := os.Lstat(parent)
		if err != nil {
			return err
		}
		owner, ok := info.Sys().(*syscall.Stat_t)
		if !ok || (owner.Uid != 0 && owner.Uid != uint32(os.Getuid())) {
			return errors.New("plugin directory has an untrusted owner")
		}
		if info.Mode()&os.ModeSymlink == 0 && (!info.IsDir() || info.Mode().Perm()&0022 != 0 && info.Mode()&os.ModeSticky == 0) {
			return errors.New("plugin directory is replaceable by another user")
		}
		next := filepath.Dir(parent)
		if next == parent {
			return nil
		}
		parent = next
	}
}

func sameArtifact(a, b *Manifest) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Executable == b.Executable && a.SHA256 == b.SHA256 && a.manifestSHA256 == b.manifestSHA256 && a.Side == b.Side && a.Version == b.Version
}

func pluginArguments(base []string, manifest *sdk.PluginManifest, side string) []string {
	var flags []string
	if manifest != nil {
		flags = manifest.BinaryFlags
	}
	args := make([]string, 0, len(base)+len(flags)+1)
	args = append(args, base...)
	sideArgument := "--side=" + side
	for _, flag := range flags {
		flag = strings.ReplaceAll(flag, "${side}", side)
		if flag != sideArgument {
			args = append(args, flag)
		}
	}
	return append(args, sideArgument)
}
