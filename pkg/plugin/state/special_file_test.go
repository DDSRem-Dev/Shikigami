package state

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestStateFIFORejectedWithoutWriter(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "state.json")
	if err := syscall.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		store, err := Open(dir, Options{DisableRecovery: true})
		if store != nil {
			store.Close()
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("FIFO accepted as state")
		}
	case <-time.After(time.Second):
		peer, err := os.OpenFile(path, os.O_RDWR|syscall.O_NONBLOCK, 0)
		if err == nil {
			peer.Close()
		}
		t.Fatal("opening state FIFO waited for a writer")
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeNamedPipe == 0 {
		t.Fatal("invalid state was replaced")
	}
}
