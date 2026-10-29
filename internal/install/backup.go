package install

import (
	"archive/tar"
	"context"
	"crypto/rand"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

const maxBackupBytes int64 = 8 << 30

func (n *Native) Backup(ctx context.Context, plan Plan, prefix string) (string, error) {
	if err := validateBackupDestination(plan, prefix); err != nil {
		return "", err
	}
	root, err := os.OpenRoot(prefix)
	if err != nil {
		return "", err
	}
	defer root.Close()
	// This inventory is taken after Stop, with writer locks held. The same
	// bounded list supplies the space calculation and archive; no second walk
	// silently adds unbudgeted files or omits a replacement source.
	inventory, err := n.collectBackup(ctx, plan, true)
	if err != nil {
		return "", err
	}
	defer inventory.Close()
	if err = root.MkdirAll("backups", 0700); err != nil {
		return "", err
	}
	info, err := root.Lstat("backups")
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errors.New("backup destination must be a non-symlink directory")
	}
	destination, err := root.OpenRoot("backups")
	if err != nil {
		return "", err
	}
	defer destination.Close()
	pinned, err := destination.Stat(".")
	if err != nil || !os.SameFile(info, pinned) {
		return "", errors.New("backup destination changed")
	}
	if err = checkSpace(ctx, []spaceNeed{{path: filepath.Join(prefix, "backups"), bytes: inventory.bytes, root: destination, relative: "."}}); err != nil {
		return "", err
	}
	name := "before-" + rand.Text() + ".tar"
	file, err := destination.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return "", err
	}
	committed := false
	defer func() {
		file.Close()
		if !committed {
			destination.Remove(name)
		}
	}()
	archive := tar.NewWriter(file)
	for _, entry := range inventory.entries {
		if err = ctx.Err(); err != nil {
			return "", err
		}
		if err = entry.unchanged(); err != nil {
			return "", err
		}
		if err = archive.WriteHeader(entry.header); err != nil {
			return "", err
		}
		if entry.info.IsDir() {
			continue
		}
		if err = copyBackupEntry(ctx, archive, entry); err != nil {
			return "", err
		}
	}
	// Directory identity/mtime checks reject additions/removals after inventory.
	for _, entry := range inventory.entries {
		if err = ctx.Err(); err != nil {
			return "", err
		}
		if err = entry.unchanged(); err != nil {
			return "", err
		}
	}
	if err = archive.Close(); err != nil {
		return "", err
	}
	if err = file.Sync(); err != nil {
		return "", err
	}
	if err = file.Close(); err != nil {
		return "", err
	}
	// Persist the tar entry in its actual parent, then a newly created backups
	// directory in the installation prefix. Syncing only the prefix is not enough.
	if err = syncDir(destination); err != nil {
		return "", err
	}
	if err = syncDir(root); err != nil {
		return "", err
	}
	committed = true
	return filepath.Join(prefix, "backups", name), nil
}
func copyBackupEntry(ctx context.Context, out io.Writer, entry backupEntry) error {
	in, err := entry.root.OpenFile(entry.relative, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return err
	}
	defer in.Close()
	opened, err := in.Stat()
	if err != nil || !os.SameFile(entry.info, opened) {
		return errors.New("state file changed during backup")
	}
	copied, err := io.CopyN(out, contextBackupReader{ctx, in}, entry.info.Size())
	if err != nil {
		return err
	}
	if copied != entry.info.Size() {
		return errors.New("state file changed during backup")
	}
	after, err := in.Stat()
	if err != nil {
		return err
	}
	if after.Size() != entry.info.Size() || !after.ModTime().Equal(entry.info.ModTime()) {
		return errors.New("state changed during backup")
	}
	return nil
}

type contextBackupReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextBackupReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}
func within(parent, child string) bool {
	relative, err := filepath.Rel(parent, child)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}

// An archive must never be created inside a tree it is backing up. Resolve
// existing aliases too; otherwise an explicitly configured layout can cause
// the backup to consume its own growing archive or retained release payloads.
func validateBackupDestination(plan Plan, prefix string) error {
	destination, err := filepath.EvalSymlinks(prefix)
	if err != nil {
		return err
	}
	for _, source := range []string{plan.StateDir, plan.TailscaleDir} {
		if source == "" {
			continue
		}
		resolved, err := filepath.EvalSymlinks(source)
		if errors.Is(err, os.ErrNotExist) {
			resolved = filepath.Clean(source)
		} else if err != nil {
			return err
		}
		if within(resolved, destination) {
			return errors.New("installation prefix must not be inside backed-up state directories")
		}
	}
	return nil
}
