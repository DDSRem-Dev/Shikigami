package server

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	epb "github.com/DDSRem-Dev/Shikigami/pkg/protocol/extensions"
	browserruntime "github.com/DDSRem-Dev/Shikigami/plugins/browser/runtime"
)

const stagingEntryLimit = 512

func boundedEntries(directory string) ([]os.DirEntry, error) {
	file, err := os.Open(directory)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	entries, err := file.ReadDir(stagingEntryLimit + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if len(entries) > stagingEntryLimit {
		return nil, errors.New("browser staging entry limit exceeded")
	}
	return entries, nil
}
func uploadTemporary(name string) bool {
	suffix, ok := strings.CutPrefix(name, ".upload-")
	if !ok || len(suffix) < 1 || len(suffix) > 20 {
		return false
	}
	for _, letter := range suffix {
		if letter < '0' || letter > '9' {
			return false
		}
	}
	return true
}
func cleanInterruptedUploads(state string, profiles map[string]*epb.BrowserProfile) error {
	for id := range profiles {
		directory := filepath.Join(state, "profiles", id, "uploads")
		if _, err := os.Lstat(directory); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return err
		}
		if err := browserruntime.PrivateDir(directory); err != nil {
			return err
		}
		if err := cleanUploadDirectory(directory); err != nil {
			return err
		}
	}
	return nil
}

func cleanUploadDirectory(directory string) error {
	root, err := os.OpenRoot(directory)
	if err != nil {
		return err
	}
	defer root.Close()
	file, err := root.Open(".")
	if err != nil {
		return err
	}
	defer file.Close()
	count, removed := 0, false
	for {
		entries, readErr := file.ReadDir(128)
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return readErr
		}
		for _, entry := range entries {
			if uploadTemporary(entry.Name()) {
				// Unlink the recognized private name without following it or
				// requiring one link: publication may have linked the final
				// name immediately before the previous process exited.
				if err := root.Remove(entry.Name()); err != nil {
					return err
				}
				removed = true
			} else {
				count++
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
	}
	if removed {
		if err := file.Sync(); err != nil {
			return err
		}
	}
	if count > stagingEntryLimit {
		return errors.New("browser staging entry limit exceeded")
	}
	return nil
}
