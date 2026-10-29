package project

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"connectrpc.com/connect/v2"
	"github.com/DDSRem-Dev/Shikigami/internal/server/storage"
	"golang.org/x/sys/unix"
)

// OpenDirectory canonicalizes an explicitly associated existing directory and
// returns a root-confined handle. Callers own Close. Neither this function nor
// project deletion changes its contents.
func OpenDirectory(path string) (string, *os.Root, error) {
	if !filepath.IsAbs(path) || strings.IndexByte(path, 0) >= 0 {
		return "", nil, errors.New("absolute project directory required")
	}
	canonical, err := filepath.EvalSymlinks(filepath.Clean(path))
	if err != nil {
		return "", nil, err
	}
	root, err := os.OpenRoot(canonical)
	if err != nil {
		return "", nil, err
	}
	if err = checkDirectoryAccess(root); err != nil {
		root.Close()
		return "", nil, err
	}
	return canonical, root, nil
}

func checkDirectoryAccess(root *os.Root) error {
	dir, err := root.Open(".")
	if err != nil {
		return err
	}
	raw, err := dir.SyscallConn()
	if err == nil {
		var accessErr error
		err = raw.Control(func(fd uintptr) {
			accessErr = unix.Faccessat(int(fd), ".", unix.R_OK|unix.W_OK|unix.X_OK, unix.AT_EACCESS)
		})
		if err == nil {
			err = accessErr
		}
	}
	err = errors.Join(err, dir.Close())
	return err
}

// OpenWorkingDirectory is shared by session creation/restoration. It requires
// an existing directory inside the project; os.Root prevents symlink escape
// during traversal instead of relying on a check followed by an ordinary Open.
func OpenWorkingDirectory(projectRoot, requested string) (string, *os.Root, error) {
	if requested == "" {
		requested = projectRoot
	}
	if !filepath.IsAbs(projectRoot) || !filepath.IsAbs(requested) || strings.IndexByte(requested, 0) >= 0 || !storage.Within(projectRoot, requested) {
		return "", nil, rpc(connect.CodeInvalidArgument, "working directory must be inside the project")
	}
	root, err := os.OpenRoot(projectRoot)
	if err != nil {
		return "", nil, filesystemError(err)
	}
	defer root.Close()
	rel, err := filepath.Rel(projectRoot, requested)
	if err != nil {
		return "", nil, rpc(connect.CodeInvalidArgument, "invalid working directory")
	}
	dir, err := root.OpenRoot(rel)
	if err != nil {
		return "", nil, filesystemError(err)
	}
	if err = checkDirectoryAccess(dir); err != nil {
		dir.Close()
		return "", nil, filesystemError(err)
	}
	canonical, err := filepath.EvalSymlinks(requested)
	if err != nil {
		dir.Close()
		return "", nil, filesystemError(err)
	}
	if !storage.Within(projectRoot, canonical) {
		dir.Close()
		return "", nil, rpc(connect.CodeInvalidArgument, "working directory escapes the project")
	}
	return canonical, dir, nil
}

func (m *Manager) resolve(path string) (canonical string, exists bool, err error) {
	path = filepath.Clean(path)
	info, statErr := os.Stat(path)
	if statErr == nil {
		if !info.IsDir() {
			return "", false, rpc(connect.CodeInvalidArgument, "project path must name a directory")
		}
		canonical, root, openErr := OpenDirectory(path)
		if openErr != nil {
			return "", false, filesystemError(openErr)
		}
		if closeErr := root.Close(); closeErr != nil {
			return "", false, filesystemError(closeErr)
		}
		return canonical, true, nil
	}
	if !errors.Is(statErr, os.ErrNotExist) {
		return "", false, filesystemError(statErr)
	}
	// Normalize the configured base's own aliases (for example /var on macOS)
	// without resolving untrusted symlinks below that root.
	if storage.Within(m.baseAlias, path) {
		rel, _ := filepath.Rel(m.baseAlias, path)
		path = filepath.Join(m.base, rel)
	}
	if !storage.Within(m.base, path) {
		return "", false, rpc(connect.CodeInvalidArgument, "new project directories must be inside configured projects_dir")
	}
	root := m.baseRoot
	rel, relErr := filepath.Rel(m.base, path)
	if relErr != nil {
		return "", false, rpc(connect.CodeInvalidArgument, "invalid project path")
	}
	// Skip symlinks for new roots, including dangling links. os.Root remains
	// the race-safe enforcement even if a directory is swapped after this walk.
	current := "."
	for _, component := range strings.Split(rel, string(filepath.Separator)) {
		current = filepath.Join(current, component)
		entry, err := root.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			break
		}
		if err != nil {
			return "", false, filesystemError(err)
		}
		if entry.Mode()&os.ModeSymlink != 0 {
			return "", false, rpc(connect.CodeInvalidArgument, "new project path cannot traverse symbolic links")
		}
		if !entry.IsDir() {
			return "", false, rpc(connect.CodeInvalidArgument, "project parent is not a directory")
		}
	}
	return path, false, nil
}

func (m *Manager) createDirectory(path string) error {
	root := m.baseRoot
	rel, err := filepath.Rel(m.base, path)
	if err != nil || !storage.Within(m.base, path) {
		return rpc(connect.CodeInvalidArgument, "project path escapes configured projects_dir")
	}
	if err = root.MkdirAll(rel, 0700); err != nil {
		return filesystemError(err)
	}
	return nil
}
