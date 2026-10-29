package config

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"gopkg.in/yaml.v3"
)

var ErrRevisionConflict = errors.New("configuration changed; reload before saving")
var ErrFutureSchema = errors.New("configuration schema is newer than this program; no changes made")

const maxConfigBytes = 1 << 20

func LoadDaemon(path string) (*Daemon, error) {
	path, err := configPath(path)
	if err != nil {
		return nil, err
	}
	data, err := readPrivate(path)
	if err != nil {
		return nil, loadError(err)
	}
	cfg, err := parseDaemon(data)
	return cfg, loadError(err)
}
func LoadClient(path string) (*Client, error) {
	path, err := configPath(path)
	if err != nil {
		return nil, err
	}
	data, err := readPrivate(path)
	if err != nil {
		return nil, loadError(err)
	}
	cfg, err := parseClient(data)
	return cfg, loadError(err)
}

// Loading never repairs or restores files. Keep missing-file initialization and
// error classification intact while explaining the operator's recovery steps.
func loadError(err error) error {
	if err == nil || errors.Is(err, os.ErrNotExist) {
		return err
	}
	if errors.Is(err, ErrFutureSchema) {
		return fmt.Errorf("%w; preserve the original file and any .previous backup; use a compatible program version, or explicitly restore a backup only after validating its compatibility; do not lower schema_version; then explicitly restart the affected service", err)
	}
	return fmt.Errorf("%w; configuration was not loaded and the original file was left unchanged; preserve the original file and any .previous backup; correct the reported problem, or explicitly restore a backup only after validating it with a compatible program version; then explicitly restart the affected service", err)
}

func parseDaemon(data []byte) (*Daemon, error) {
	cfg := DefaultDaemon("", "")
	version, err := decode(data, cfg)
	if err != nil {
		return nil, err
	}
	if version < SchemaVersion {
		cfg.MigratedFrom = &version
		cfg.SchemaVersion = SchemaVersion
	}
	if err = cfg.NormalizeAndValidate(); err != nil {
		return nil, err
	}
	cfg.Revision = revision(data)
	return cfg, nil
}
func parseClient(data []byte) (*Client, error) {
	cfg := DefaultClient()
	version, err := decode(data, cfg)
	if err != nil {
		return nil, err
	}
	if version < SchemaVersion {
		cfg.MigratedFrom = &version
		cfg.SchemaVersion = SchemaVersion
	}
	if err = cfg.NormalizeAndValidate(); err != nil {
		return nil, err
	}
	cfg.Revision = revision(data)
	return cfg, nil
}

// Schema 0 is the documented pre-implementation layout with no version field.
// Its migration is additive: defaults plus schema_version=1. Unknown keys are
// rejected in every schema, including version 0; migration never drops fields.
func decode(data []byte, target any) (int, error) {
	if len(data) == 0 || len(data) > maxConfigBytes {
		return 0, errors.New("configuration is empty or exceeds 1 MiB")
	}
	var node yaml.Node
	if err := yaml.Unmarshal(data, &node); err != nil {
		return 0, errors.New("invalid YAML configuration")
	}
	if len(node.Content) != 1 || node.Content[0].Kind != yaml.MappingNode {
		return 0, errors.New("configuration must be a YAML mapping")
	}
	version := 0
	for i := 0; i < len(node.Content[0].Content); i += 2 {
		if node.Content[0].Content[i].Value == "schema_version" {
			if err := node.Content[0].Content[i+1].Decode(&version); err != nil {
				return 0, errors.New("schema_version must be an integer")
			}
		}
	}
	if version > SchemaVersion {
		return 0, ErrFutureSchema
	}
	if version < 0 {
		return 0, errors.New("invalid schema_version")
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(target); err != nil {
		return 0, errors.New("invalid configuration: unknown or duplicate key, incorrect value type, or invalid duration")
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return 0, errors.New("configuration must contain exactly one YAML document")
	}
	return version, nil
}

// SaveDaemon writes a fully validated candidate. For existing files, Revision
// must come from LoadDaemon; concurrent changes and invalid originals are never
// overwritten. The previous valid bytes are retained at path + ".previous".
func SaveDaemon(path string, cfg *Daemon) error {
	if cfg == nil {
		return errors.New("daemon config is required")
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return errors.New("cannot encode daemon config")
	}
	candidate, err := parseDaemon(data)
	if err != nil {
		return err
	}
	data, err = yaml.Marshal(candidate)
	if err != nil {
		return err
	}
	if err = save(path, data, cfg.Revision, func(old []byte) error { _, err := parseDaemon(old); return err }); err != nil {
		return err
	}
	candidate.Revision = revision(data)
	candidate.MigratedFrom = nil
	*cfg = *candidate
	return nil
}

func SaveClient(path string, cfg *Client) error {
	if cfg == nil {
		return errors.New("client config is required")
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return errors.New("cannot encode client config")
	}
	candidate, err := parseClient(data)
	if err != nil {
		return err
	}
	data, err = yaml.Marshal(candidate)
	if err != nil {
		return err
	}
	if err = save(path, data, cfg.Revision, func(old []byte) error { _, err := parseClient(old); return err }); err != nil {
		return err
	}
	candidate.Revision = revision(data)
	candidate.MigratedFrom = nil
	*cfg = *candidate
	return nil
}

func save(path string, data []byte, expected string, validateExisting func([]byte) error) error {
	path, err := configPath(path)
	if err != nil {
		return err
	}
	if len(data) > maxConfigBytes {
		return errors.New("configuration exceeds 1 MiB")
	}
	dir := filepath.Dir(path)
	if err = privateDir(dir); err != nil {
		return err
	}
	lock, err := openPrivate(path+".lock", os.O_CREATE|os.O_RDWR)
	if err != nil {
		return err
	}
	defer lock.Close()
	deadline := time.Now().Add(5 * time.Second)
	for {
		err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			return err
		}
		if !time.Now().Before(deadline) {
			return errors.New("configuration writer is busy")
		}
		time.Sleep(10 * time.Millisecond)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	old, err := readPrivate(path)
	if err == nil {
		if err = validateExisting(old); err != nil {
			return fmt.Errorf("existing configuration is invalid; preserve it for recovery: %w", err)
		}
		if expected == "" || expected != revision(old) {
			return ErrRevisionConflict
		}
		if err = atomicWrite(path+".previous", old); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	} else if expected != "" {
		return ErrRevisionConflict
	}
	return atomicWrite(path, data)
}

func revision(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func configPath(path string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return absolutePath(path, home)
}
func privateDir(path string) error {
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.IsDir() || info.Mode().Perm() != 0700 || !ok || stat.Uid != uint32(os.Geteuid()) {
		return errors.New("configuration directory must be owned by current user with mode 0700")
	}
	return nil
}
func openPrivate(path string, flags int) (*os.File, error) {
	fd, err := syscall.Open(path, flags|syscall.O_NOFOLLOW|syscall.O_CLOEXEC|syscall.O_NONBLOCK, 0600)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || !ok || (stat.Uid != uint32(os.Geteuid()) && os.Geteuid() != 0) {
		f.Close()
		return nil, errors.New("configuration must be an owned regular file with mode 0600")
	}
	return f, nil
}
func readPrivate(path string) ([]byte, error) {
	f, err := openPrivate(path, os.O_RDONLY)
	if err != nil {
		return nil, err
	}
	data, readErr := io.ReadAll(io.LimitReader(f, maxConfigBytes+1))
	closeErr := f.Close()
	if len(data) > maxConfigBytes {
		return nil, errors.New("configuration exceeds 1 MiB")
	}
	return data, errors.Join(readErr, closeErr)
}
func atomicWrite(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".config-*")
	if err != nil {
		return err
	}
	temp := f.Name()
	defer os.Remove(temp)
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(temp, path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	return errors.Join(dir.Sync(), dir.Close())
}
