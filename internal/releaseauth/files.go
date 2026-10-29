package releaseauth

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

// These helpers use only leaf names beneath pinned parent directory handles.
// NOFOLLOW/NONBLOCK plus fstat reject symlinks and devices even during a swap.
func openRegular(path, label string, limit int64, private bool) (*os.File, os.FileInfo, error) {
	if path == "" {
		return nil, nil, fmt.Errorf("%s path is required", label)
	}
	parent, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return nil, nil, fileError(label, err)
	}
	defer parent.Close()
	name := filepath.Base(path)
	before, err := parent.Lstat(name)
	if err != nil {
		return nil, nil, fileError(label, err)
	}
	if err := regularInfo(before, label, limit, private); err != nil {
		return nil, nil, err
	}
	file, err := parent.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, nil, fileError(label, err)
	}
	info, err := file.Stat()
	if err == nil {
		err = regularInfo(info, label, limit, private)
	}
	if err == nil && !os.SameFile(before, info) {
		err = fmt.Errorf("%s changed during open", label)
	}
	if err != nil {
		file.Close()
		return nil, nil, fileError(label, err)
	}
	return file, info, nil
}

func regularInfo(info os.FileInfo, label string, limit int64, private bool) error {
	if !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > limit {
		return fmt.Errorf("%s must be a nonempty bounded regular file", label)
	}
	if private {
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || uint64(stat.Uid) != uint64(os.Geteuid()) {
			return errors.New("private key owner must be the current user")
		}
		if info.Mode().Perm() != 0600 || info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
			return errors.New("private key mode must be 0600")
		}
	}
	return nil
}

func unchanged(file *os.File, original os.FileInfo) error {
	current, err := file.Stat()
	if err != nil {
		return errors.New("cannot recheck input file")
	}
	if !os.SameFile(original, current) || original.Size() != current.Size() || original.Mode() != current.Mode() || !original.ModTime().Equal(current.ModTime()) {
		return errors.New("input changed while reading")
	}
	return nil
}

func readInput(ctx context.Context, path, label string, limit int64, private bool) ([]byte, os.FileInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	file, info, err := openRegular(path, label, limit, private)
	if err != nil {
		return nil, nil, err
	}
	defer file.Close()
	var data bytes.Buffer
	_, err = transfer(ctx, &data, file, limit)
	if err == nil {
		err = unchanged(file, info)
	}
	if err != nil {
		clear(data.Bytes())
		return nil, nil, fileError(label, err)
	}
	return data.Bytes(), info, nil
}

// transfer checks cancellation between bounded regular-file reads and writes.
// It never writes a byte beyond the declared bound or allocates by archive size.
func transfer(ctx context.Context, dst io.Writer, src io.Reader, limit int64) (int64, error) {
	var total int64
	buffer := make([]byte, 128<<10)
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		chunk := buffer
		if remaining := limit - total + 1; int64(len(chunk)) > remaining {
			chunk = chunk[:remaining]
		}
		n, readErr := src.Read(chunk)
		if total+int64(n) > limit {
			return total, errors.New("input exceeds size limit")
		}
		if n > 0 {
			if err := ctx.Err(); err != nil {
				return total, err
			}
			written, err := dst.Write(chunk[:n])
			total += int64(written)
			if err != nil {
				return total, err
			}
			if written != n {
				return total, io.ErrShortWrite
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return total, ctx.Err()
			}
			return total, readErr
		}
		if n == 0 {
			return total, io.ErrNoProgress
		}
	}
}

func distinct(infos ...os.FileInfo) error {
	for i, current := range infos {
		for _, previous := range infos[:i] {
			if os.SameFile(current, previous) {
				return errors.New("release inputs must be distinct files")
			}
		}
	}
	return nil
}

// Strip user-controlled filenames and parser text from errors. Preserve errno
// and context identity so callers can report failure/cancellation accurately.
func fileError(label string, err error) error {
	var pathError *os.PathError
	if errors.As(err, &pathError) {
		err = pathError.Err
	}
	return fmt.Errorf("%s: %w", label, err)
}

func syncRoot(root *os.Root) error {
	file, err := root.Open(".")
	if err != nil {
		return fileError("output directory sync", err)
	}
	defer file.Close()
	return fileErrorOrNil("output directory sync", file.Sync())
}

func fileErrorOrNil(label string, err error) error {
	if err == nil {
		return nil
	}
	return fileError(label, err)
}

func (o *privateOutput) writeNew(ctx context.Context, name string, data []byte) error {
	file, err := o.create(name)
	if err != nil {
		return fileError("new output file", err)
	}
	defer file.Close()
	if err = file.Chmod(0600); err == nil {
		_, err = transfer(ctx, file, bytes.NewReader(data), int64(len(data)))
	}
	if err == nil {
		err = file.Sync()
	}
	if err == nil {
		err = file.Close()
	}
	return fileErrorOrNil("write output file", err)
}
