package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

func (h *helper) path(p string) (string, error) {
	if p == "" {
		return "", invalid()
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(h.root, p)
	}
	rel, err := filepath.Rel(h.root, filepath.Clean(p))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", invalid()
	}
	current := h.root
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", invalid()
		}
	}
	return rel, nil
}
func digest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

type fileInfo struct {
	Path   string `json:"path,omitempty"`
	Exists bool   `json:"exists"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

func statFile(root *os.Root, p string) (fileInfo, error) {
	v := fileInfo{}
	pre, err := root.Lstat(p)
	if errors.Is(err, os.ErrNotExist) {
		return v, nil
	}
	if err != nil {
		return v, err
	}
	if !pre.Mode().IsRegular() {
		return v, invalid()
	}
	f, err := root.OpenFile(p, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if errors.Is(err, os.ErrNotExist) {
		return v, nil
	}
	if err != nil {
		return v, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return v, err
	}
	if !info.Mode().IsRegular() || info.Size() > 64<<20 {
		return v, invalid()
	}
	sum := sha256.New()
	n, err := io.Copy(sum, io.LimitReader(f, (64<<20)+1))
	if err != nil {
		return v, err
	}
	if n > 64<<20 {
		return v, invalid()
	}
	return fileInfo{Exists: true, Size: n, SHA256: hex.EncodeToString(sum.Sum(nil))}, nil
}
func (h *helper) fileOp(ctx context.Context, r request) (any, error) {
	a, err := args[struct {
		Path      string `json:"path"`
		Content   string `json:"content"`
		Pattern   string `json:"pattern"`
		Size      int    `json:"size"`
		Recursive bool   `json:"recursive"`
	}](r)
	if err != nil {
		return nil, invalid()
	}
	p, err := h.path(a.Path)
	if err != nil || p == "." {
		return nil, invalid()
	}
	root, err := os.OpenRoot(h.root)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	switch r.Op {
	case "file.stat":
		return statFile(root, p)
	case "file.remove":
		info, err := root.Lstat(p)
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() {
			return nil, invalid()
		}
		return map[string]bool{"removed": true}, root.Remove(p)
	case "file.write":
		if a.Size < 0 || a.Size > 32<<20 {
			return nil, invalid()
		}
		data := []byte(a.Content)
		if a.Pattern != "" {
			if a.Size == 0 {
				return nil, invalid()
			}
			data = make([]byte, a.Size)
			for at := 0; at < len(data); {
				at += copy(data[at:], a.Pattern)
			}
		}
		if len(data) > 32<<20 {
			return nil, invalid()
		}
		if err = root.MkdirAll(filepath.Dir(p), 0700); err != nil {
			return nil, err
		}
		f, err := root.OpenFile(p, os.O_WRONLY|os.O_CREATE|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0600)
		if err != nil {
			return nil, err
		}
		info, err := f.Stat()
		if err != nil || !info.Mode().IsRegular() {
			_ = f.Close()
			return nil, invalid()
		}
		if err = f.Truncate(0); err != nil {
			_ = f.Close()
			return nil, err
		}
		_, err = f.Write(data)
		err = errors.Join(err, f.Sync(), f.Close())
		if err != nil {
			return nil, err
		}
		return statFile(root, p)
	case "file.list":
		out := []fileInfo{}
		err = fs.WalkDir(root.FS(), p, func(name string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if name == p {
				return nil
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return invalid()
			}
			if entry.IsDir() {
				if !a.Recursive {
					return fs.SkipDir
				}
				return nil
			}
			if len(out) >= 256 {
				return invalid()
			}
			v, err := statFile(root, name)
			if err != nil {
				return err
			}
			v.Path, _ = filepath.Rel(p, name)
			out = append(out, v)
			return nil
		})
		return map[string]any{"files": out}, err
	}
	return nil, invalid()
}
