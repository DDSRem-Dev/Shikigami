//go:build darwin || linux

package coretransport

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

func NewUnixClient(path string, uid uint32, authority string) (*Endpoint, error) {
	if err := validateSocketPath(path); err != nil {
		return nil, err
	}
	if authority == "" || strings.IndexFunc(authority, func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '.')
	}) >= 0 {
		return nil, errors.New("invalid IPC authority")
	}
	if err := checkPrivatePath(filepath.Dir(path), uid, true); err != nil {
		return nil, err
	}
	if err := checkPrivatePath(path, uid, false); err != nil {
		return nil, err
	}
	p := new(http.Protocols)
	p.SetUnencryptedHTTP2(true)
	transport := &http.Transport{Protocols: p, Proxy: nil, IdleConnTimeout: 90 * time.Second,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			if network != "tcp" || address != authority+":80" {
				return nil, errors.New("unexpected IPC destination")
			}
			if err := checkPrivatePath(filepath.Dir(path), uid, true); err != nil {
				return nil, err
			}
			if err := checkPrivatePath(path, uid, false); err != nil {
				return nil, err
			}
			c, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "unix", path)
			if err != nil {
				return nil, err
			}
			peer, err := socketPeerUID(c)
			if err != nil || peer != uid {
				_ = c.Close()
				return nil, errors.New("IPC peer UID mismatch")
			}
			return c, nil
		},
	}
	return newEndpoint("http://"+authority, transport), nil
}

func validateSocketPath(path string) error {
	// 104 is Darwin's smaller sockaddr_un limit, including the terminating NUL.
	if !filepath.IsAbs(path) || strings.ContainsRune(path, 0) || len(path) >= 104 {
		return errors.New("Unix socket path must be absolute and shorter than 104 bytes")
	}
	return nil
}
func checkPrivatePath(path string, uid uint32, directory bool) error {
	i, err := os.Lstat(path)
	if err != nil {
		return err
	}
	s, ok := i.Sys().(*syscall.Stat_t)
	if !ok || s.Uid != uid {
		return fmt.Errorf("IPC path has unexpected owner: %s", path)
	}
	if directory {
		if !i.IsDir() || i.Mode().Perm() != 0700 {
			return fmt.Errorf("IPC directory must be a real directory with mode 0700: %s", path)
		}
	} else if i.Mode()&os.ModeSocket == 0 || i.Mode().Perm() != 0600 {
		return fmt.Errorf("IPC socket must have mode 0600: %s", path)
	}
	return nil
}
