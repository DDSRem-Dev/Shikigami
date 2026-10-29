package filetree

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"

	"connectrpc.com/connect/v2"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	"golang.org/x/sys/unix"
)

func (t *Tree) ReadFile(ctx context.Context, r *pb.ReadFileRequest, send func(*pb.FileData) error) error {
	if r == nil || r.ExpectedRevision == "" {
		return failure(connect.CodeInvalidArgument, "expected revision required")
	}
	if err := t.check(ctx, r.ProjectId); err != nil {
		return err
	}
	p, err := clean(r.Path, false)
	if err != nil {
		return err
	}
	d, err := t.directory(path.Dir(p), false)
	if err != nil {
		return err
	}
	defer d.Close()
	f, err := openRegular(d, path.Base(p))
	if err != nil {
		return err
	}
	defer f.Close()
	v, err := t.inspect(ctx, f, p)
	if err != nil {
		return err
	}
	if v.Size > t.opts.MaxFileBytes {
		return failure(connect.CodeResourceExhausted, "file exceeds transfer limit")
	}
	if v.Revision != r.ExpectedRevision {
		return failure(connect.CodeAborted, "file revision changed")
	}
	if err = send(&pb.FileData{Payload: &pb.FileData_Info{Info: v}}); err != nil {
		return err
	}
	if _, err = f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	h := sha256.New()
	var total int64
	for {
		if err = ctx.Err(); err != nil {
			return err
		}
		buf := make([]byte, ChunkSize)
		n, e := f.Read(buf)
		if n > 0 {
			total += int64(n)
			if total > v.Size {
				return failure(connect.CodeAborted, "file grew during transfer")
			}
			h.Write(buf[:n])
			if err = send(&pb.FileData{Payload: &pb.FileData_Chunk{Chunk: buf[:n]}}); err != nil {
				return err
			}
		}
		if errors.Is(e, io.EOF) {
			break
		}
		if e != nil {
			return e
		}
	}
	if total != v.Size || hex.EncodeToString(h.Sum(nil)) != v.ContentHash {
		return failure(connect.CodeAborted, "file changed during transfer")
	}
	latest, err := t.StatFile(ctx, &pb.StatFileRequest{ProjectId: r.ProjectId, Path: p})
	if err != nil {
		return err
	}
	if latest.Revision != v.Revision {
		return failure(connect.CodeAborted, "file changed during transfer")
	}
	return nil
}

func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
func makeTemp(d *os.File) (*os.File, string, error) {
	name := internalPrefix + newID()
	fd, err := unix.Openat(int(d.Fd()), name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return nil, "", fsError(err)
	}
	return os.NewFile(uintptr(fd), name), name, nil
}
func removeAt(d *os.File, name string) error { return unix.Unlinkat(int(d.Fd()), name, 0) }
func current(t *Tree, ctx context.Context, d *os.File, name, p string) (*pb.FileInfo, error) {
	f, err := openRegular(d, name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return t.inspect(ctx, f, p)
}

func (t *Tree) WriteFile(ctx context.Context, recv func() (*pb.WriteFileRequest, error)) (*pb.FileInfo, error) {
	first, err := recv()
	if err != nil {
		return nil, err
	}
	if first == nil || first.GetHeader() == nil {
		return nil, failure(connect.CodeInvalidArgument, "first frame must be a write header")
	}
	h := first.GetHeader()
	if err = t.check(ctx, h.ProjectId); err != nil {
		return nil, err
	}
	p, err := clean(h.Path, false)
	if err != nil {
		return nil, err
	}
	hash, err := hex.DecodeString(h.ContentHash)
	if err != nil || len(hash) != sha256.Size || hex.EncodeToString(hash) != h.ContentHash || h.Size < 0 || h.Mode&^uint32(0777) != 0 {
		return nil, failure(connect.CodeInvalidArgument, "invalid hash, size, or ordinary permission bits")
	}
	if h.Size > t.opts.MaxFileBytes {
		return nil, failure(connect.CodeResourceExhausted, "file exceeds transfer limit")
	}
	d, err := t.directory(path.Dir(p), true)
	if err != nil {
		return nil, err
	}
	defer d.Close()
	f, tmp, err := makeTemp(d)
	if err != nil {
		return nil, err
	}
	keep := false
	defer func() {
		f.Close()
		if !keep {
			_ = removeAt(d, tmp)
		}
	}()
	digest := sha256.New()
	var size int64
	for {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		frame, e := recv()
		if errors.Is(e, io.EOF) {
			break
		}
		if e != nil {
			return nil, e
		}
		if frame == nil {
			return nil, failure(connect.CodeInvalidArgument, "empty write frame")
		}
		chunk, ok := frame.Payload.(*pb.WriteFileRequest_Chunk)
		if !ok {
			return nil, failure(connect.CodeInvalidArgument, "only the first frame may contain a header")
		}
		if len(chunk.Chunk) > ChunkSize {
			return nil, failure(connect.CodeResourceExhausted, "chunk exceeds 256 KiB")
		}
		size += int64(len(chunk.Chunk))
		if size > h.Size || size > t.opts.MaxFileBytes {
			return nil, failure(connect.CodeResourceExhausted, "received content exceeds declared size")
		}
		if _, err = f.Write(chunk.Chunk); err != nil {
			return nil, err
		}
		digest.Write(chunk.Chunk)
	}
	if size != h.Size || hex.EncodeToString(digest.Sum(nil)) != h.ContentHash {
		return nil, failure(connect.CodeDataLoss, "content size or SHA-256 mismatch")
	}
	if err = f.Chmod(os.FileMode(h.Mode)); err != nil {
		return nil, err
	}
	if err = f.Sync(); err != nil {
		return nil, err
	}
	if err = f.Close(); err != nil {
		return nil, err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if err = t.check(ctx, h.ProjectId); err != nil {
		return nil, err
	}
	if err = t.ensureRecovered(); err != nil {
		return nil, err
	}
	name := path.Base(p)
	old, err := current(t, ctx, d, name, p)
	if h.BaseRevision == "" {
		if err == nil {
			return nil, failure(connect.CodeAborted, "file already exists")
		}
		if connect.CodeOf(err) != connect.CodeNotFound {
			return nil, err
		}
		if err = unix.Linkat(int(d.Fd()), tmp, int(d.Fd()), name, 0); err != nil {
			return nil, fsError(err)
		}
		if err = removeAt(d, tmp); err != nil {
			return nil, err
		}
		if err = d.Sync(); err != nil {
			return nil, err
		}
	} else {
		if err != nil {
			if connect.CodeOf(err) == connect.CodeNotFound {
				return nil, failure(connect.CodeAborted, "base file disappeared")
			}
			return nil, err
		}
		if old.Revision != h.BaseRevision {
			return nil, failure(connect.CodeAborted, "base revision changed")
		}
		if err = t.reserve(p, t.opts.MaxFileBytes); err != nil {
			return nil, err
		}
		j := pending{ID: tmp, Path: p, Temp: tmp}
		if err = t.putPending(j); err != nil {
			return nil, err
		}
		keep = true
		if err = exchange(int(d.Fd()), tmp, name); err != nil {
			if clearErr := t.clearPending(tmp); clearErr == nil {
				keep = false
			}
			return nil, fmt.Errorf("atomic exchange unavailable or failed: %w", err)
		}
		if err = d.Sync(); err != nil {
			return nil, err
		}
		preservedRevision, e := t.preserve(d, j)
		if e != nil {
			return nil, e
		}
		keep = false
		if preservedRevision != h.BaseRevision {
			return nil, failure(connect.CodeAborted, "external write raced with commit; displaced content preserved in history")
		}
	}
	result, err := current(t, ctx, d, name, p)
	if err != nil {
		return nil, err
	}
	if result.ContentHash != h.ContentHash || result.Mode != h.Mode {
		return nil, failure(connect.CodeAborted, "external write detected after commit")
	}
	return result, nil
}

func (t *Tree) DeleteFile(ctx context.Context, r *pb.DeleteFileRequest) error {
	if r == nil || r.ExpectedRevision == "" {
		return failure(connect.CodeInvalidArgument, "expected revision required")
	}
	if err := t.check(ctx, r.ProjectId); err != nil {
		return err
	}
	p, err := clean(r.Path, false)
	if err != nil {
		return err
	}
	d, err := t.directory(path.Dir(p), false)
	if err != nil {
		return err
	}
	defer d.Close()
	t.mu.Lock()
	defer t.mu.Unlock()
	if err = t.check(ctx, r.ProjectId); err != nil {
		return err
	}
	if err = t.ensureRecovered(); err != nil {
		return err
	}
	name := path.Base(p)
	before, err := current(t, ctx, d, name, p)
	if err != nil {
		return err
	}
	if before.Revision != r.ExpectedRevision {
		return failure(connect.CodeAborted, "delete revision changed")
	}
	if err = t.reserve(p, t.opts.MaxFileBytes); err != nil {
		return err
	}
	tmp := internalPrefix + newID()
	j := pending{ID: tmp, Path: p, Temp: tmp}
	if err = t.putPending(j); err != nil {
		return err
	}
	if err = unix.Renameat(int(d.Fd()), name, int(d.Fd()), tmp); err != nil {
		_ = t.clearPending(tmp)
		return fsError(err)
	}
	if err = d.Sync(); err != nil {
		return err
	}
	moved, err := current(t, context.WithoutCancel(ctx), d, tmp, p)
	if err != nil {
		return err
	}
	raced := moved.Revision != r.ExpectedRevision
	if raced { // Restore only if the original name is still absent.
		if e := unix.Linkat(int(d.Fd()), tmp, int(d.Fd()), name, 0); e != nil && !errors.Is(e, unix.EEXIST) {
			return e
		}
	}
	preservedRevision, err := t.preserve(d, j)
	if err != nil {
		return err
	}
	if raced || preservedRevision != r.ExpectedRevision {
		return failure(connect.CodeAborted, "external write raced with delete; displaced content preserved")
	}
	var st unix.Stat_t
	err = unix.Fstatat(int(d.Fd()), name, &st, unix.AT_SYMLINK_NOFOLLOW)
	if err == nil {
		return failure(connect.CodeAborted, "file recreated during delete")
	}
	if !errors.Is(err, unix.ENOENT) {
		return fsError(err)
	}
	return nil
}
