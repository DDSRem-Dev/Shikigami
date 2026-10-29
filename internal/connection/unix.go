//go:build darwin || linux

package connection

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

type peerUIDKey struct{}

func PeerUID(ctx context.Context) (uint32, bool) {
	uid, ok := ctx.Value(peerUIDKey{}).(uint32)
	return uid, ok
}

type uidConn struct {
	net.Conn
	uid uint32
}
type uidListener struct {
	net.Listener
	uid uint32
}

func (l *uidListener) Accept() (net.Conn, error) {
	for {
		conn, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		uid, err := socketPeerUID(conn)
		if err != nil || uid != l.uid {
			_ = conn.Close()
			continue
		}
		return &uidConn{Conn: conn, uid: uid}, nil
	}
}

// ListenUnix never replaces or removes an existing destination. Stale recovery belongs to the
// single-instance lock owner, which must establish that the old process died.
func ListenUnix(path string, uid uint32) (net.Listener, error) {
	if err := validateSocketPath(path); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	if err := checkPrivatePath(filepath.Dir(path), uid, true); err != nil {
		return nil, err
	}
	// Never expose the public name before mode/ownership are final. A hard link
	// publishes the bound inode without overwriting any existing destination.
	// Darwin and Linux both allow connecting through the linked socket inode.
	directory := filepath.Dir(path)
	length := min(12, 103-len(directory)-1)
	if length < 1 {
		return nil, errors.New("Unix socket directory leaves no room for a private bind name")
	}
	var listener *net.UnixListener
	var temporary string
	var err error
	for range 64 {
		temporary = filepath.Join(directory, rand.Text()[:length])
		if temporary == path {
			continue
		}
		listener, err = net.ListenUnix("unix", &net.UnixAddr{Name: temporary, Net: "unix"})
		if err == nil {
			break
		}
		if !errors.Is(err, syscall.EADDRINUSE) {
			return nil, err
		}
	}
	if listener == nil {
		return nil, errors.New("cannot allocate private Unix socket bind name")
	}
	listener.SetUnlinkOnClose(false)
	inode, err := os.Lstat(temporary)
	if err != nil {
		_ = listener.Close()
		return nil, err
	}
	defer removeSocketInode(temporary, inode)
	if err = os.Chmod(temporary, 0600); err != nil {
		_ = listener.Close()
		return nil, err
	}
	if err = checkPrivatePath(temporary, uid, false); err != nil {
		_ = listener.Close()
		return nil, err
	}
	if err = os.Link(temporary, path); err != nil {
		_ = listener.Close()
		return nil, err
	}
	published := &publishedUnixListener{UnixListener: listener, path: path, inode: inode}
	if err := removeSocketInode(temporary, inode); err != nil {
		return nil, errors.Join(err, published.Close())
	}
	return &uidListener{Listener: published, uid: uid}, nil
}

// The net listener remains bound to its private name, so its default unlink-on-
// close would target the wrong path. Own cleanup explicitly and preserve a
// replacement installed after this listener was detached from the public name.
// Identity-check then unlink relies on the owner lifecycle lock; this is not an
// atomic CAS against arbitrary same-UID renames between those two syscalls.
type publishedUnixListener struct {
	*net.UnixListener
	path      string
	inode     os.FileInfo
	closeOnce sync.Once
	closeErr  error
}

func (l *publishedUnixListener) Addr() net.Addr { return &net.UnixAddr{Name: l.path, Net: "unix"} }
func (l *publishedUnixListener) Close() error {
	l.closeOnce.Do(func() {
		l.closeErr = l.UnixListener.Close()
		l.closeErr = errors.Join(l.closeErr, removeSocketInode(l.path, l.inode))
	})
	return l.closeErr
}
func removeSocketInode(path string, expected os.FileInfo) error {
	current, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !os.SameFile(current, expected) {
		return nil
	}
	return os.Remove(path)
}

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
