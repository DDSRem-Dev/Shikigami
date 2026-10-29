package install

import (
	"archive/tar"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

const maxBackupEntries = 100000

type backupEntry struct {
	root     *os.Root
	relative string
	info     os.FileInfo
	header   *tar.Header
}
type backupInventory struct {
	entries []backupEntry
	roots   []*os.Root
	locks   []*os.File
	bytes   uint64
}

func (b *backupInventory) Close() {
	for _, f := range b.locks {
		f.Close()
	}
	for _, r := range b.roots {
		r.Close()
	}
}

type byteCounter uint64

func (c *byteCounter) Write(p []byte) (int, error) { *c += byteCounter(len(p)); return len(p), nil }

func (n *Native) collectBackup(ctx context.Context, plan Plan, lockWriters bool) (result *backupInventory, err error) {
	type source struct {
		label, path string
		directory   bool
	}
	sources := []source{{"config", plan.Config, false}, {"state", plan.StateDir, true}}
	if plan.ServiceFile != "" {
		sources = append(sources, source{"service", plan.ServiceFile, false})
	}
	if plan.TailscaleDir != "" && !within(plan.StateDir, plan.TailscaleDir) {
		sources = append(sources, source{"tailscale", plan.TailscaleDir, true})
	}
	b := &backupInventory{bytes: 1024}
	defer func() {
		if err != nil {
			b.Close()
		}
	}()
	var content uint64
	entries := 0
	for _, item := range sources {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		if item.path == "" {
			continue
		}
		if !filepath.IsAbs(item.path) {
			return nil, errors.New("backup source must be absolute")
		}
		base, start := item.path, "."
		if !item.directory {
			base, start = filepath.Dir(item.path), filepath.Base(item.path)
		}
		info, statErr := os.Lstat(base)
		if errors.Is(statErr, os.ErrNotExist) {
			continue
		}
		if statErr != nil {
			return nil, statErr
		}
		if !info.IsDir() {
			return nil, errors.New("backup root must be a non-symlink directory")
		}
		root, openErr := os.OpenRoot(base)
		if openErr != nil {
			return nil, openErr
		}
		b.roots = append(b.roots, root)
		pinned, statErr := root.Stat(".")
		if statErr != nil || !os.SameFile(info, pinned) {
			return nil, errors.New("backup root changed")
		}
		err = walkBackup(ctx, root, filepath.ToSlash(start), func(relative string, entry fs.DirEntry, walkErr error) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if errors.Is(walkErr, os.ErrNotExist) && relative == filepath.ToSlash(start) {
				return nil
			}
			if walkErr != nil {
				return walkErr
			}
			absolute := filepath.Join(base, filepath.FromSlash(relative))
			if plan.ProjectsDir != "" && absolute == filepath.Clean(plan.ProjectsDir) {
				if entry.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			entries++
			if entries > maxBackupEntries {
				return errors.New("state backup entry limit exceeded")
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if !info.IsDir() && !info.Mode().IsRegular() {
				return nil
			}
			if info.Size() < 0 {
				return errors.New("invalid backup file size")
			}
			if info.Mode().IsRegular() {
				content, err = addSpace(content, uint64(info.Size()))
				if err != nil || content > uint64(maxBackupBytes) {
					return errors.New("state backup exceeds 8 GiB limit")
				}
				if lockWriters && strings.HasSuffix(entry.Name(), ".lock") {
					file, err := root.OpenFile(relative, os.O_RDWR|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
					if err != nil {
						return err
					}
					opened, err := file.Stat()
					if err != nil || !os.SameFile(info, opened) {
						file.Close()
						return errors.New("state lock changed during backup")
					}
					owned, err := n.ownsStartupLock(file)
					if err != nil {
						file.Close()
						return err
					}
					if !owned {
						if err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
							file.Close()
							return errors.New("host or plugin still owns state; backup refused")
						}
					}
					b.locks = append(b.locks, file)
				}
			}
			header, err := tar.FileInfoHeader(info, "")
			if err != nil {
				return err
			}
			header.Name = filepath.ToSlash(filepath.Join(item.label, relative))
			var count byteCounter
			if err = tar.NewWriter(&count).WriteHeader(header); err != nil {
				return err
			}
			size := uint64(count)
			if info.Mode().IsRegular() {
				padded := (uint64(info.Size()) + 511) / 512 * 512
				size, err = addSpace(size, padded)
				if err != nil {
					return err
				}
			}
			if b.bytes, err = addSpace(b.bytes, size); err != nil {
				return err
			}
			b.entries = append(b.entries, backupEntry{root, relative, info, header})
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return b, nil
}
func (e backupEntry) unchanged() error {
	info, err := e.root.Lstat(e.relative)
	if err != nil {
		return err
	}
	if !os.SameFile(e.info, info) || e.info.Mode() != info.Mode() || e.info.Size() != info.Size() || !e.info.ModTime().Equal(info.ModTime()) {
		return errors.New("state changed during backup")
	}
	return nil
}
