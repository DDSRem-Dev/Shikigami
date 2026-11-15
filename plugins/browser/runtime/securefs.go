package runtime

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
)

// PrivateDir rejects link leaves and shared directories. The caller supplies
// only host-owned roots and generated identifiers, never a remote path.
func PrivateDir(path string) error {
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0077 != 0 || !owned(info) {
		return errors.New("browser directory is not private and user-owned")
	}
	return nil
}

func ReadJSON(path string, value any, maximum int64) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || !owned(info) || !singleLink(info) || info.Size() > maximum {
		return errors.New("invalid private browser state file")
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return err
	}
	if !os.SameFile(info, opened) {
		return errors.New("browser state changed during open")
	}
	decoder := json.NewDecoder(io.LimitReader(file, maximum+1))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("trailing browser state data")
	}
	return nil
}

// AtomicJSON rejects encoded data beyond the matching reader's limit before
// creating a temporary file or replacing previously readable state.
func AtomicJSON(path string, value any, maximum int64) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if maximum <= 0 || int64(len(data)) > maximum {
		return errors.New("encoded browser JSON exceeds its reader limit")
	}
	return atomicFile(path, data)
}

// PublishedError means the new file is visible but its directory could not be
// synchronized. Callers must retain the published state, not restore old data.
type PublishedError struct{ Err error }

func (failure *PublishedError) Error() string {
	return "browser state published but directory sync failed: " + failure.Err.Error()
}
func (failure *PublishedError) Unwrap() error { return failure.Err }

func atomicFile(path string, data []byte) error {
	return atomicFileWithSync(path, data, syncDirectory)
}

func atomicFileWithSync(path string, data []byte, syncParent func(string) error) error {
	if err := PrivateDir(filepath.Dir(path)); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".state-*")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	if err := file.Chmod(0600); err != nil {
		file.Close()
		return err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	if err := syncParent(filepath.Dir(path)); err != nil {
		return &PublishedError{Err: err}
	}
	return nil
}

// OpenStage uses a root handle as well as identity checks: neither a symlink
// swap nor a hard link can turn profile staging into an arbitrary-file API.
func OpenStage(directory, name string) (*os.File, error) {
	if err := PrivateDir(directory); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	before, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() || !owned(before) || !singleLink(before) {
		return nil, errors.New("browser staging entry is not a private ordinary file")
	}
	file, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	after, err := file.Stat()
	if err != nil || !os.SameFile(before, after) || !singleLink(after) {
		file.Close()
		return nil, errors.New("browser staging entry changed during open")
	}
	return file, nil
}
