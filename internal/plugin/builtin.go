package plugin

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/DDSRem-Dev/Shikigami/internal/strictjson"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"

	storage "github.com/DDSRem-Dev/Shikigami/pkg/plugin/state"
)

var embeddedExtraction sync.Mutex

// ResolveBuiltinManifest prefers explicitly installed artifacts. Only an absent
// manifest may use the matching build's embedded assets; incompatible/tampered
// installed content never silently falls back to another implementation.
func ResolveBuiltinManifest(directory, id, side, build string) (Manifest, error) {
	manifest, err := ReadInstalledManifest(directory, id, side, build)
	if err == nil || !errors.Is(err, os.ErrNotExist) {
		return manifest, err
	}
	assets := builtinAssets()
	if assets == nil {
		return manifest, err
	}
	return extractBuiltin(assets, directory, id, side, build)
}
func extractBuiltin(assets fs.FS, directory, id, side, build string) (Manifest, error) {
	var manifest Manifest
	if (id != "sync" && id != "port") || (side != "client" && side != "server") || !filepath.IsAbs(directory) || build == "" || build == "." || build == ".." || filepath.Base(build) != build || strings.ContainsAny(build, "\\\x00") {
		return manifest, errors.New("invalid embedded plugin request")
	}
	file, err := assets.Open(id + ".manifest.json")
	if err != nil {
		return manifest, err
	}
	data, err := io.ReadAll(io.LimitReader(file, (64<<10)+1))
	file.Close()
	if err != nil {
		return manifest, err
	}
	if len(data) > 64<<10 {
		return manifest, errors.New("embedded manifest exceeds limit")
	}
	if err = strictjson.Decode(data, &manifest); err != nil {
		return manifest, err
	}
	if manifest.Name != id || manifest.Version != build || manifest.Side != "both" || manifest.Executable != "shiki-plugin-"+id || len(manifest.SHA256) != 64 {
		return Manifest{}, errors.New("embedded plugin build does not match host")
	}
	if _, err = hex.DecodeString(manifest.SHA256); err != nil {
		return Manifest{}, err
	}
	embeddedExtraction.Lock()
	defer embeddedExtraction.Unlock()
	cache := filepath.Join(directory, ".embedded", build, runtime.GOOS+"-"+runtime.GOARCH, id+"-"+manifest.SHA256)
	// Validate/create ancestors from outside inward, before placing content.
	for _, current := range []string{directory, filepath.Join(directory, ".embedded"), filepath.Join(directory, ".embedded", build), filepath.Join(directory, ".embedded", build, runtime.GOOS+"-"+runtime.GOARCH), cache} {
		if err = storage.EnsurePrivateDir(current); err != nil {
			return Manifest{}, err
		}
	}
	target := filepath.Join(cache, manifest.Executable)
	source, err := assets.Open(manifest.Executable)
	if err != nil {
		return Manifest{}, err
	}
	defer source.Close()
	if err = installEmbeddedFile(target, source, manifest.SHA256, 0700); err != nil {
		return Manifest{}, err
	}
	sum := sha256.Sum256(data)
	if err = installEmbeddedFile(filepath.Join(cache, id+".manifest.json"), strings.NewReader(string(data)), hex.EncodeToString(sum[:]), 0600); err != nil {
		return Manifest{}, err
	}
	return ReadInstalledManifest(cache, id, side, build)
}
func installEmbeddedFile(target string, source io.Reader, expected string, mode os.FileMode) error {
	verify := func() error {
		f, err := os.OpenFile(target, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
		if err != nil {
			return err
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil {
			return err
		}
		owner, ok := info.Sys().(*syscall.Stat_t)
		if !ok || owner.Uid != uint32(os.Geteuid()) || !info.Mode().IsRegular() || info.Mode().Perm() != mode || info.Size() > 1<<30 {
			return errors.New("unsafe embedded plugin cache")
		}
		h := sha256.New()
		if _, err = io.Copy(h, io.LimitReader(f, (1<<30)+1)); err != nil {
			return err
		}
		if hex.EncodeToString(h.Sum(nil)) != expected {
			return errors.New("embedded cache checksum mismatch; evidence preserved")
		}
		return nil
	}
	if err := verify(); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(target), ".extract-")
	if err != nil {
		return err
	}
	defer func() { temporary.Close(); os.Remove(temporary.Name()) }()
	if err = temporary.Chmod(mode); err != nil {
		return err
	}
	h := sha256.New()
	size, err := io.Copy(io.MultiWriter(temporary, h), io.LimitReader(source, (1<<30)+1))
	if err != nil {
		return err
	}
	if size > 1<<30 || hex.EncodeToString(h.Sum(nil)) != expected {
		return errors.New("embedded payload checksum mismatch")
	}
	if err = temporary.Sync(); err != nil {
		return err
	}
	if err = temporary.Close(); err != nil {
		return err
	}
	if err = os.Link(temporary.Name(), target); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	if err = verify(); err != nil {
		return err
	}
	directory, err := os.Open(filepath.Dir(target))
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
