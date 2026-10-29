package project

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"connectrpc.com/connect/v2"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	"golang.org/x/sys/unix"
)

const maxDirectoryEntries = 10000

type directoryCursor struct {
	Revision string `json:"revision"`
	Offset   int    `json:"offset"`
}

// ListDirectories is a bounded picker, not the sync plugin's file inventory.
// Every component, including the stored project root, is opened without links.
func (m *Manager) ListDirectories(ctx context.Context, request *pb.ListProjectDirectoriesRequest) (*pb.ListProjectDirectoriesResponse, error) {
	if request == nil || len(request.Path) > 4096 || !utf8.ValidString(request.Path) || strings.ContainsAny(request.Path, "\\\x00") || (request.Path != "" && (path.IsAbs(request.Path) || path.Clean(request.Path) != request.Path || request.Path == "." || request.Path == ".." || strings.HasPrefix(request.Path, "../"))) {
		return nil, rpc(connect.CodeInvalidArgument, "canonical project-relative directory required")
	}
	pageSize := request.PageSize
	if pageSize == 0 {
		pageSize = 50
	}
	if pageSize > 100 || len(request.PageToken) > 512 {
		return nil, rpc(connect.CodeInvalidArgument, "directory page size or cursor exceeds limit")
	}
	var cursor directoryCursor
	if request.PageToken != "" {
		data, err := base64.RawURLEncoding.DecodeString(request.PageToken)
		if err != nil || json.Unmarshal(data, &cursor) != nil || len(cursor.Revision) != 64 || cursor.Offset <= 0 || cursor.Offset > maxDirectoryEntries {
			return nil, rpc(connect.CodeInvalidArgument, "invalid directory cursor")
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	project, err := m.Get(ctx, &pb.GetProjectRequest{Id: request.ProjectId})
	if err != nil {
		return nil, err
	}
	root, err := openAbsoluteDirectory(project.Path)
	if err != nil {
		return nil, directoryError(err)
	}
	defer root.Close()
	rootInfo, err := root.Stat()
	if err != nil {
		return nil, directoryError(err)
	}
	dir, err := openDirectoryAt(root, request.Path)
	if err != nil {
		return nil, directoryError(err)
	}
	defer dir.Close()
	before, err := dir.Stat()
	if err != nil {
		return nil, directoryError(err)
	}
	entries, err := dir.ReadDir(maxDirectoryEntries + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, directoryError(err)
	}
	if len(entries) > maxDirectoryEntries {
		return nil, rpc(connect.CodeResourceExhausted, "directory scan exceeds 10000 entries; choose a narrower path")
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	hash := sha256.New()
	fmt.Fprintf(hash, "%s\x00%s\x00%d\x00", project.Id, request.Path, before.ModTime().UnixNano())
	var rootStat unix.Stat_t
	if err := unix.Fstat(int(root.Fd()), &rootStat); err != nil {
		return nil, directoryError(err)
	}
	fmt.Fprintf(hash, "%d:%d\x00", rootStat.Dev, rootStat.Ino)
	// The inode binds a cursor to this directory, even across a replacement
	// carrying identical names and timestamps. The descriptor remains pinned.
	var stat unix.Stat_t
	if err := unix.Fstat(int(dir.Fd()), &stat); err != nil {
		return nil, directoryError(err)
	}
	fmt.Fprintf(hash, "%d:%d\x00", stat.Dev, stat.Ino)
	directories := make([]string, 0)
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !utf8.ValidString(entry.Name()) {
			return nil, rpc(connect.CodeFailedPrecondition, "DIRECTORY_NAME_NOT_UTF8")
		}
		var info unix.Stat_t
		err := unix.Fstatat(int(dir.Fd()), entry.Name(), &info, unix.AT_SYMLINK_NOFOLLOW)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil, rpc(connect.CodeAborted, "directory changed during scan")
			}
			return nil, directoryError(err)
		}
		fmt.Fprintf(hash, "%s\x00%d\x00", entry.Name(), info.Mode&unix.S_IFMT)
		if info.Mode&unix.S_IFMT == unix.S_IFDIR {
			choice := path.Join(request.Path, entry.Name())
			if strings.Contains(entry.Name(), "\\") || len(choice) > 4096 {
				return nil, rpc(connect.CodeFailedPrecondition, "DIRECTORY_NAME_UNREPRESENTABLE")
			}
			directories = append(directories, choice)
		}
	}
	after, err := dir.Stat()
	if err != nil {
		return nil, directoryError(err)
	}
	// Reopen from the absolute no-follow root to detect replacement of either
	// the association or requested directory while its original fd was pinned.
	currentRoot, err := openAbsoluteDirectory(project.Path)
	if err != nil {
		return nil, rpc(connect.CodeAborted, "project root changed during scan")
	}
	defer currentRoot.Close()
	currentRootInfo, err := currentRoot.Stat()
	if err != nil || !os.SameFile(rootInfo, currentRootInfo) {
		return nil, rpc(connect.CodeAborted, "project root changed during scan")
	}
	currentDir, err := openDirectoryAt(currentRoot, request.Path)
	if err != nil {
		return nil, rpc(connect.CodeAborted, "directory changed during scan")
	}
	defer currentDir.Close()
	currentInfo, err := currentDir.Stat()
	if err != nil || !os.SameFile(before, currentInfo) || !before.ModTime().Equal(after.ModTime()) || !before.ModTime().Equal(currentInfo.ModTime()) {
		return nil, rpc(connect.CodeAborted, "directory changed during scan")
	}
	revision := fmt.Sprintf("%x", hash.Sum(nil))
	if request.PageToken != "" && cursor.Revision != revision {
		return nil, rpc(connect.CodeAborted, "directory changed; restart pagination")
	}
	if cursor.Offset > len(directories) {
		return nil, rpc(connect.CodeInvalidArgument, "directory cursor exceeds listing")
	}
	end := min(cursor.Offset+int(pageSize), len(directories))
	result := &pb.ListProjectDirectoriesResponse{ProjectId: project.Id, Path: request.Path, Directories: directories[cursor.Offset:end]}
	if end < len(directories) {
		data, err := json.Marshal(directoryCursor{Revision: revision, Offset: end})
		if err != nil {
			return nil, err
		}
		result.NextPageToken = base64.RawURLEncoding.EncodeToString(data)
	}
	return result, ctx.Err()
}

func openAbsoluteDirectory(name string) (*os.File, error) {
	if !filepath.IsAbs(name) || filepath.Clean(name) != name {
		return nil, unix.EINVAL
	}
	root, err := os.Open("/")
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return openDirectoryAt(root, strings.TrimPrefix(name, "/"))
}

func openDirectoryAt(parent *os.File, relative string) (*os.File, error) {
	fd, err := unix.Openat(int(parent.Fd()), ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	for _, component := range strings.Split(relative, "/") {
		if component == "" {
			continue
		}
		next, err := unix.Openat(fd, component, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		_ = unix.Close(fd)
		if err != nil {
			return nil, err
		}
		fd = next
	}
	return os.NewFile(uintptr(fd), relative), nil
}

func directoryError(err error) error {
	if errors.Is(err, unix.ELOOP) || errors.Is(err, unix.ENOTDIR) || errors.Is(err, unix.EINVAL) {
		return rpc(connect.CodeInvalidArgument, "directory path cannot traverse links or non-directories")
	}
	return filesystemError(err)
}
