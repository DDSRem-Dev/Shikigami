package install

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

// Unlike fs.WalkDir, do not read an entire large directory before enforcing
// the inventory entry limit. A bounded page is read from each pinned directory.
func walkBackup(ctx context.Context, root *os.Root, start string, visit func(string, fs.DirEntry, error) error) error {
	info, err := root.Lstat(start)
	if err != nil {
		return visit(start, nil, err)
	}
	var walk func(string, fs.DirEntry) error
	walk = func(path string, entry fs.DirEntry) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := visit(path, entry, nil); err != nil {
			if errors.Is(err, fs.SkipDir) && entry.IsDir() {
				return nil
			}
			return err
		}
		if !entry.IsDir() {
			return nil
		}
		directory, err := root.OpenFile(path, os.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW, 0)
		if err != nil {
			return err
		}
		defer directory.Close()
		expected, err := entry.Info()
		if err != nil {
			return err
		}
		opened, err := directory.Stat()
		if err != nil || !os.SameFile(expected, opened) {
			return errors.New("backup directory changed during enumeration")
		}
		for {
			if err := ctx.Err(); err != nil {
				return err
			}
			children, readErr := directory.ReadDir(128)
			for _, child := range children {
				if err := walk(filepath.Join(path, child.Name()), child); err != nil {
					return err
				}
			}
			if errors.Is(readErr, io.EOF) {
				return nil
			}
			if readErr != nil {
				return readErr
			}
		}
	}
	return walk(start, fs.FileInfoToDirEntry(info))
}
