// Package filetree synchronizes ordinary files through pinned directory handles.
// Revisions describe content and permission bits; filesystem timestamps are never
// treated as a distributed clock. External writers do not participate in our
// locks, so replaced versions are retained and commits are checked again.
package filetree

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"connectrpc.com/connect/v2"
	sdk "github.com/DDSRem-Dev/Shikigami/pkg/plugin"
	storage "github.com/DDSRem-Dev/Shikigami/pkg/plugin/state"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	"golang.org/x/sys/unix"
)

const ChunkSize = 256 << 10
const internalPrefix = ".shiki-sync-transfer-"

type Options struct {
	ProjectID       string
	StateDir        string
	MaxFileBytes    int64
	MaxEntries      int
	MaxPageSize     uint32
	HistoryVersions int
	HistoryBytes    int64
	PollInterval    time.Duration
	OnSkipped       func(context.Context, string)
}

type Tree struct {
	root        *os.File
	state       *storage.Store
	history     *os.Root
	opts        Options
	mu          sync.Mutex
	closed      chan struct{}
	once        sync.Once
	skippedHint atomic.Bool
}

var _ sdk.FileService = (*Tree)(nil)

func Open(root string, opts Options) (*Tree, error) {
	if opts.ProjectID == "" || opts.StateDir == "" {
		return nil, fmt.Errorf("project ID and private state directory are required")
	}
	if opts.MaxFileBytes == 0 {
		opts.MaxFileBytes = 10 << 20
	}
	if opts.MaxEntries == 0 {
		opts.MaxEntries = 10000
	}
	if opts.MaxPageSize == 0 {
		opts.MaxPageSize = 128
	}
	if opts.HistoryVersions == 0 {
		opts.HistoryVersions = 5
	}
	if opts.HistoryBytes == 0 {
		opts.HistoryBytes = 64 << 20
	}
	if opts.PollInterval == 0 {
		opts.PollInterval = time.Second
	}
	if opts.MaxFileBytes < 1 || opts.MaxEntries < 1 || opts.MaxPageSize > 1024 || opts.HistoryVersions < 1 || opts.HistoryBytes < opts.MaxFileBytes || opts.PollInterval < time.Millisecond {
		return nil, fmt.Errorf("invalid filesystem limits")
	}
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	canonical, err = filepath.Abs(canonical)
	if err != nil {
		return nil, err
	}
	if err = storage.EnsurePrivateDir(opts.StateDir); err != nil {
		return nil, err
	}
	statePath, err := filepath.EvalSymlinks(opts.StateDir)
	if err != nil {
		return nil, err
	}
	statePath, err = filepath.Abs(statePath)
	if err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(canonical, statePath)
	if err != nil {
		return nil, err
	}
	if rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
		return nil, fmt.Errorf("state directory must be outside the project")
	}
	fd, err := unix.Open(canonical, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	t := &Tree{root: os.NewFile(uintptr(fd), canonical), opts: opts, closed: make(chan struct{})}
	t.state, err = storage.Open(statePath, storage.Options{DisableRecovery: true})
	if err != nil {
		t.root.Close()
		return nil, err
	}
	if err = storage.EnsurePrivateDir(filepath.Join(statePath, "history")); err == nil {
		t.history, err = os.OpenRoot(filepath.Join(statePath, "history"))
	}
	if err != nil {
		t.state.Close()
		t.root.Close()
		return nil, err
	}
	if err = t.state.Update(func(s *storage.Snapshot) error {
		type identity struct {
			Root    string `json:"root"`
			Project string `json:"project"`
		}
		var prior identity
		if b := s.Buckets["filetree_identity"]["identity"]; b != nil {
			if e := json.Unmarshal(b, &prior); e != nil {
				return e
			}
			if prior.Root != canonical || prior.Project != opts.ProjectID {
				return fmt.Errorf("filesystem state belongs to another project or root")
			}
			return nil
		}
		return s.Put("filetree_identity", "identity", identity{canonical, opts.ProjectID})
	}); err != nil {
		t.Close()
		return nil, err
	}
	if err = t.recover(); err != nil {
		t.Close()
		return nil, err
	}
	return t, nil
}

func (t *Tree) Close() error {
	var err error
	t.once.Do(func() {
		close(t.closed)
		t.mu.Lock()
		defer t.mu.Unlock()
		err = errors.Join(t.history.Close(), t.state.Close(), t.root.Close())
	})
	return err
}
func (t *Tree) check(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case <-t.closed:
		return failure(connect.CodeUnavailable, "filesystem closed")
	default:
	}
	if id != t.opts.ProjectID {
		return failure(connect.CodePermissionDenied, "project does not match filesystem")
	}
	pinned, err := t.root.Stat()
	current, pathErr := os.Lstat(t.root.Name())
	if err != nil || pathErr != nil || !current.IsDir() || !os.SameFile(pinned, current) {
		return failure(connect.CodeFailedPrecondition, "PROJECT_ROOT_CHANGED")
	}
	return nil
}
func failure(code connect.Code, msg string) error { return connect.NewError(code, msg) }
func fsError(err error) error {
	if err == nil {
		return nil
	}
	var ce *connect.Error
	if errors.As(err, &ce) {
		return err
	}
	switch {
	case errors.Is(err, os.ErrNotExist):
		return connect.NewError(connect.CodeNotFound, err.Error())
	case errors.Is(err, os.ErrPermission):
		return connect.NewError(connect.CodePermissionDenied, err.Error())
	case errors.Is(err, unix.ELOOP), errors.Is(err, unix.ENOTDIR):
		return connect.NewError(connect.CodeFailedPrecondition, err.Error())
	case errors.Is(err, os.ErrExist):
		return connect.NewError(connect.CodeAborted, err.Error())
	default:
		return err
	}
}
func clean(p string, rootOK bool) (string, error) {
	if p == "" || p == "." {
		if rootOK {
			return ".", nil
		}
		return "", failure(connect.CodeInvalidArgument, "file path required")
	}
	if strings.ContainsAny(p, "\\\x00") || strings.HasPrefix(p, "/") || path.Clean(p) != p {
		return "", failure(connect.CodeInvalidArgument, "invalid relative path")
	}
	for _, v := range strings.Split(p, "/") {
		if v == ".." || strings.HasPrefix(v, internalPrefix) {
			return "", failure(connect.CodeInvalidArgument, "reserved or escaping path")
		}
	}
	return p, nil
}

// Each component is opened independently with O_NOFOLLOW. Unlike a separate
// Lstat followed by Open, swapping a component for a symlink cannot redirect it.
func (t *Tree) directory(p string, create bool) (*os.File, error) {
	raw, err := t.root.SyscallConn()
	if err != nil {
		return nil, fsError(err)
	}
	var fd int
	var openErr error
	// Control keeps the descriptor pinned against concurrent Tree.Close.
	if err = raw.Control(func(rootFD uintptr) {
		fd, openErr = unix.Openat(int(rootFD), ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	}); err != nil {
		return nil, fsError(err)
	}
	if openErr != nil {
		return nil, fsError(openErr)
	}
	if p != "." {
		for _, part := range strings.Split(p, "/") {
			next, e := unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
			if errors.Is(e, unix.ENOENT) && create {
				e = unix.Mkdirat(fd, part, 0700)
				if e == nil {
					e = unix.Fsync(fd)
				}
				if e == nil || errors.Is(e, unix.EEXIST) {
					next, e = unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
				}
			}
			unix.Close(fd)
			if e != nil {
				return nil, fsError(e)
			}
			fd = next
		}
	}
	return os.NewFile(uintptr(fd), p), nil
}
func openRegular(dir *os.File, name string) (*os.File, error) {
	fd, err := unix.Openat(int(dir.Fd()), name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, fsError(err)
	}
	f := os.NewFile(uintptr(fd), name)
	s, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if !s.Mode().IsRegular() {
		f.Close()
		return nil, failure(connect.CodeFailedPrecondition, "only regular files can be transferred")
	}
	return f, nil
}
func info(p string, s os.FileInfo) *pb.FileInfo {
	return &pb.FileInfo{Name: path.Base(p), Path: p, Size: s.Size(), Mode: uint32(s.Mode().Perm()), ModTime: s.ModTime().UnixNano(), IsDir: s.IsDir()}
}
func sameStat(a, b os.FileInfo) bool {
	return os.SameFile(a, b) && a.Size() == b.Size() && a.ModTime() == b.ModTime() && a.Mode() == b.Mode()
}
func revision(hash string, mode uint32) string {
	v := sha256.Sum256([]byte(fmt.Sprintf("v1:%s:%03o", hash, mode)))
	return hex.EncodeToString(v[:])
}
func (t *Tree) inspect(ctx context.Context, f *os.File, p string) (*pb.FileInfo, error) {
	before, err := f.Stat()
	if err != nil {
		return nil, err
	}
	out := info(p, before)
	if !before.Mode().IsRegular() {
		return nil, failure(connect.CodeFailedPrecondition, "not a regular file")
	}
	if before.Size() > t.opts.MaxFileBytes {
		return out, nil
	}
	if _, err = f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	h := sha256.New()
	buf := make([]byte, ChunkSize)
	var size int64
	for {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		n, e := f.Read(buf)
		if n > 0 {
			size += int64(n)
			if size > t.opts.MaxFileBytes {
				return nil, failure(connect.CodeResourceExhausted, "file exceeds transfer limit")
			}
			h.Write(buf[:n])
		}
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, e
		}
	}
	after, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !sameStat(before, after) || size != before.Size() {
		return nil, failure(connect.CodeAborted, "file changed while reading")
	}
	out.ContentHash = hex.EncodeToString(h.Sum(nil))
	out.Revision = revision(out.ContentHash, out.Mode)
	return out, nil
}

func (t *Tree) StatFile(ctx context.Context, r *pb.StatFileRequest) (*pb.FileInfo, error) {
	if r == nil {
		return nil, failure(connect.CodeInvalidArgument, "request required")
	}
	if err := t.check(ctx, r.ProjectId); err != nil {
		return nil, err
	}
	p, err := clean(r.Path, true)
	if err != nil {
		return nil, err
	}
	if p == "." {
		s, e := t.root.Stat()
		if e != nil {
			return nil, e
		}
		return info(".", s), nil
	}
	dir, err := t.directory(path.Dir(p), false)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	var st unix.Stat_t
	if err = unix.Fstatat(int(dir.Fd()), path.Base(p), &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return nil, fsError(err)
	}
	if st.Mode&unix.S_IFMT == unix.S_IFDIR {
		d, e := t.directory(p, false)
		if e != nil {
			return nil, e
		}
		defer d.Close()
		s, e := d.Stat()
		if e != nil {
			return nil, e
		}
		return info(p, s), nil
	}
	f, err := openRegular(dir, path.Base(p))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return t.inspect(ctx, f, p)
}

func (t *Tree) ListFiles(ctx context.Context, r *pb.ListFilesRequest) (*pb.ListFilesResponse, error) {
	if r == nil {
		return nil, failure(connect.CodeInvalidArgument, "request required")
	}
	if err := t.check(ctx, r.ProjectId); err != nil {
		return nil, err
	}
	p, err := clean(r.Path, true)
	if err != nil {
		return nil, err
	}
	n := r.PageSize
	if n == 0 {
		n = 100
	}
	if n > t.opts.MaxPageSize {
		n = t.opts.MaxPageSize
	}
	last := ""
	if r.PageToken != "" {
		b, e := base64.RawURLEncoding.DecodeString(r.PageToken)
		if e != nil {
			return nil, failure(connect.CodeInvalidArgument, "invalid page token")
		}
		parts := strings.SplitN(string(b), "\x00", 2)
		if len(parts) != 2 || parts[0] != p {
			return nil, failure(connect.CodeInvalidArgument, "page token directory mismatch")
		}
		last = parts[1]
	}
	d, err := t.directory(p, false)
	if err != nil {
		return nil, err
	}
	defer d.Close()
	entries, err := d.ReadDir(t.opts.MaxEntries + 1)
	if err != nil && err != io.EOF {
		return nil, err
	}
	if len(entries) > t.opts.MaxEntries {
		return nil, failure(connect.CodeResourceExhausted, "directory entry limit exceeded")
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	out := &pb.ListFilesResponse{}
	for _, e := range entries {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		if e.Name() <= last || strings.HasPrefix(e.Name(), internalPrefix) {
			continue
		}
		if e.Type()&os.ModeSymlink != 0 || (!e.IsDir() && !e.Type().IsRegular()) {
			// Report once per tree lifetime through the host log, without
			// exposing filenames or following unsupported filesystem objects.
			if t.opts.OnSkipped != nil && t.skippedHint.CompareAndSwap(false, true) {
				t.opts.OnSkipped(ctx, "SYMLINK_OR_SPECIAL_FILE_SKIPPED")
			}
			continue
		}
		if len(out.Files) == int(n) {
			out.NextPageToken = base64.RawURLEncoding.EncodeToString([]byte(p + "\x00" + out.Files[len(out.Files)-1].Name))
			break
		}
		v, e := t.StatFile(ctx, &pb.StatFileRequest{ProjectId: r.ProjectId, Path: path.Join(p, e.Name())})
		if connect.CodeOf(e) == connect.CodeNotFound {
			continue
		}
		if e != nil {
			return nil, e
		}
		out.Files = append(out.Files, v)
	}
	return out, nil
}
