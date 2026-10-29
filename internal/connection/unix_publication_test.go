//go:build darwin || linux

package connection

import (
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func publicationDirectory(t *testing.T) string {
	t.Helper()
	d, err := os.MkdirTemp("/tmp", "su-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(d) })
	return d
}
func exchangePublishedSocket(t *testing.T, l net.Listener, path string) {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		c, err := l.Accept()
		if err == nil {
			defer c.Close()
			_, err = io.CopyN(c, c, 1)
		}
		done <- err
	}()
	c, err := net.DialTimeout("unix", path, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(time.Second))
	if _, err = c.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	var data [1]byte
	if _, err = io.ReadFull(c, data[:]); err != nil || data[0] != 'x' {
		t.Fatal("published socket not usable", err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
func TestUnixSocketPublishedOnlyWithFinalPermissions(t *testing.T) {
	directory := publicationDirectory(t)
	for iteration := 0; iteration < 100; iteration++ {
		path := filepath.Join(directory, "agent.sock")
		stop := make(chan struct{})
		result := make(chan error, 1)
		ready := make(chan struct{})
		go func() {
			close(ready)
			for {
				select {
				case <-stop:
					result <- nil
					return
				default:
				}
				info, err := os.Lstat(path)
				if errors.Is(err, os.ErrNotExist) {
					continue
				}
				if err != nil {
					result <- err
					return
				}
				if info.Mode()&os.ModeSocket == 0 || info.Mode().Perm() != 0600 {
					result <- errors.New("public socket visible before mode 0600")
					return
				}
				if err := checkPrivatePath(path, uint32(os.Getuid()), false); err != nil {
					result <- err
					return
				}
			}
		}()
		<-ready
		l, err := ListenUnix(path, uint32(os.Getuid()))
		if err != nil {
			close(stop)
			<-result
			t.Fatal(err)
		}
		close(stop)
		if err := <-result; err != nil {
			l.Close()
			t.Fatal(err)
		}
		if l.Addr().String() != path {
			t.Fatal("private address leaked", l.Addr())
		}
		exchangePublishedSocket(t, l, path)
		if err := l.Close(); err != nil {
			t.Fatal(err)
		}
		entries, err := os.ReadDir(directory)
		if err != nil || len(entries) != 0 {
			t.Fatal("publication leaked temporary or public path", entries, err)
		}
	}
}
func TestUnixPublicationDoesNotReplaceExistingTargetsOrRemoveNewOwner(t *testing.T) {
	directory := publicationDirectory(t)
	path := filepath.Join(directory, "socket")
	for _, kind := range []string{"file", "symlink", "socket"} {
		t.Run(kind, func(t *testing.T) {
			switch kind {
			case "file":
				if err := os.WriteFile(path, []byte("keep"), 0600); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Symlink("missing", path); err != nil {
					t.Fatal(err)
				}
			case "socket":
				l, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
				if err != nil {
					t.Fatal(err)
				}
				l.SetUnlinkOnClose(false)
				l.Close()
			}
			before, err := os.Lstat(path)
			if err != nil {
				t.Fatal(err)
			}
			if l, err := ListenUnix(path, uint32(os.Getuid())); err == nil {
				l.Close()
				t.Fatal("existing target replaced")
			}
			after, err := os.Lstat(path)
			if err != nil || !os.SameFile(before, after) {
				t.Fatal("old target removed", err)
			}
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
		})
	}
	first, err := ListenUnix(path, uint32(os.Getuid()))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	second, err := ListenUnix(path, uint32(os.Getuid()))
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	exchangePublishedSocket(t, second, path)
}
func TestUnixPublicationLongestPathAndConcurrentBind(t *testing.T) {
	base := publicationDirectory(t)
	directory := filepath.Join(base, strings.Repeat("d", 101-len(base)-1))
	if len(directory) != 101 {
		t.Fatal("test path length")
	}
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "s")
	if len(path) != 103 {
		t.Fatal("test socket length")
	}
	var wait sync.WaitGroup
	results := make(chan net.Listener, 16)
	for range 16 {
		wait.Go(func() {
			l, err := ListenUnix(path, uint32(os.Getuid()))
			if err == nil {
				results <- l
			}
		})
	}
	wait.Wait()
	close(results)
	var winner net.Listener
	for l := range results {
		if winner != nil {
			l.Close()
			t.Fatal("multiple successful publishers")
		}
		winner = l
	}
	if winner == nil {
		t.Fatal("no successful publisher")
	}
	defer winner.Close()
	exchangePublishedSocket(t, winner, path)
	if l, err := ListenUnix(path+"x", uint32(os.Getuid())); err == nil {
		l.Close()
		t.Fatal("oversized socket path accepted")
	}
	if err := winner.Close(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 0 {
		t.Fatal("contending publishers leaked names", entries, err)
	}
}
