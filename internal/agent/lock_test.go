package agent

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestInstanceLockAndStaleSocket(t *testing.T) {
	// Darwin's Unix socket paths are limited to 104 bytes.
	dir, err := os.MkdirTemp("/tmp", "shiki-lock-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	socket := filepath.Join(dir, "agent.sock")
	first, err := AcquireInstance(dir, socket)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AcquireInstance(dir, socket); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("second writer acquired lock: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: socket, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	listener.SetUnlinkOnClose(false)
	if err := os.Chmod(socket, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := AcquireInstance(dir, socket); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatal("removed live socket", err)
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := AcquireInstance(dir, socket)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if _, err := os.Lstat(socket); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("stale socket retained", err)
	}
}

func TestInstanceRefusesUnsafeSocketAndSymlinkLock(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "shiki-lock-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	socket := filepath.Join(dir, "agent.sock")
	if err := os.WriteFile(socket, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := AcquireInstance(dir, socket); err == nil {
		t.Fatal("accepted regular file as stale socket")
	}
	got, err := os.ReadFile(socket)
	if err != nil || string(got) != "preserve" {
		t.Fatal("destroyed socket path contents")
	}
	if err := os.Remove(filepath.Join(dir, "instance.lock")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(socket, filepath.Join(dir, "instance.lock")); err != nil {
		t.Fatal(err)
	}
	if _, err := AcquireInstance(dir, socket); err == nil {
		t.Fatal("followed symlink lock")
	}
}

func TestRefusedSocketDoesNotProveRecordedProcessIsDead(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "shiki-lock-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	socket := filepath.Join(dir, "agent.sock")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: socket, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	listener.SetUnlinkOnClose(false)
	if err := os.Chmod(socket, 0600); err != nil {
		t.Fatal(err)
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "instance.lock"), []byte(strconv.Itoa(os.Getpid())), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := AcquireInstance(dir, socket); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("live recorded process ignored after refused dial: %v", err)
	}
	if _, err := os.Lstat(socket); err != nil {
		t.Fatal("removed socket despite live recorded process", err)
	}
}
