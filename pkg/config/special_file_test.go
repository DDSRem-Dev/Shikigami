package config

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestConfigurationFIFORejectedWithoutWriter(t *testing.T) {
	path := filepath.Join(privateTestDir(t), "config.yaml")
	if err := syscall.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := LoadClient(path); done <- err }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("FIFO accepted as configuration")
		}
	case <-time.After(time.Second):
		// Release an old blocking implementation before failing, so the regression
		// does not leave a goroutine or an indefinitely blocked test process.
		peer, err := os.OpenFile(path, os.O_RDWR|syscall.O_NONBLOCK, 0)
		if err == nil {
			peer.Close()
		}
		t.Fatal("opening configuration FIFO waited for a writer")
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeNamedPipe == 0 {
		t.Fatal("invalid configuration was replaced")
	}
}
