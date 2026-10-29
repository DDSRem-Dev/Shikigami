package client

import (
	"context"
	"errors"
	"github.com/DDSRem-Dev/Shikigami/pkg/config"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
)

func TestStartupLockSerializesAndCancels(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	first, e := startupLock(context.Background(), dir)
	if e != nil {
		t.Fatal(e)
	}
	defer first.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Millisecond)
	defer cancel()
	if second, e := startupLock(ctx, dir); !errors.Is(e, context.DeadlineExceeded) {
		if second != nil {
			second.Close()
		}
		t.Fatalf("competing startup lock: %v", e)
	}
	first.Close()
	ctx2, cancel2 := context.WithTimeout(context.Background(), time.Second)
	defer cancel2()
	second, e := startupLock(ctx2, dir)
	if e != nil {
		t.Fatal(e)
	}
	second.Close()
	for _, code := range []connect.Code{connect.CodeDeadlineExceeded, connect.CodeCanceled} {
		if connectivityPermanent(connect.NewError(code, "transient")) {
			t.Fatalf("readiness failure treated as permanent: %v", code)
		}
	}
	if !connectivityPermanent(connect.NewError(connect.CodePermissionDenied, "denied")) {
		t.Fatal("permission failure retried")
	}
}
func TestStartupFilesRejectSymlinksAndPublicState(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if e := os.WriteFile(target, []byte("unchanged"), 0600); e != nil {
		t.Fatal(e)
	}
	link := filepath.Join(root, "agent.log")
	if e := os.Symlink(target, link); e != nil {
		t.Fatal(e)
	}
	if file, e := privateFile(link, os.O_CREATE|os.O_APPEND|os.O_WRONLY); e == nil {
		file.Close()
		t.Fatal("followed log symlink")
	}
	dir := filepath.Join(root, "public")
	if e := os.Mkdir(dir, 0755); e != nil {
		t.Fatal(e)
	}
	if e := os.Chmod(dir, 0755); e != nil {
		t.Fatal(e)
	}
	if lock, e := startupLock(context.Background(), dir); e == nil {
		lock.Close()
		t.Fatal("public state directory accepted")
	}
	data, e := os.ReadFile(target)
	if e != nil || string(data) != "unchanged" {
		t.Fatal("symlink target mutated")
	}
}

// The listener is already gone while the old agent joins its workers. A new
// frontend must wait for the lifetime lock instead of starting a doomed child.
func TestStartupAndStopWaitForWorkerCleanup(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "shiki-stop-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	cfg := config.DefaultClient()
	cfg.Agent.StateDir = dir
	cfg.Agent.Socket = filepath.Join(dir, "gone.sock")
	lock, err := os.OpenFile(filepath.Join(dir, "instance.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if _, err = lock.WriteString("12345\n"); err != nil {
		t.Fatal(err)
	}
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	frontend := &Client{Config: cfg}
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Millisecond)
	defer cancel()
	if err = frontend.WaitStopped(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("socket absence falsely reported stopped: %v", err)
	}
	ctx2, cancel2 := context.WithTimeout(t.Context(), 60*time.Millisecond)
	defer cancel2()
	if c, err := startAndConnect(ctx2, "unused", cfg, Options{Timeout: time.Second, AgentBinary: filepath.Join(dir, "must-not-run")}); !errors.Is(err, context.DeadlineExceeded) {
		if c != nil {
			c.Close()
		}
		t.Fatalf("new child attempted while old instance held lock: %v", err)
	}
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	if err = frontend.WaitStopped(t.Context()); err != nil {
		t.Fatal(err)
	}
	if c, err := startAndConnect(t.Context(), "unused", cfg, Options{Timeout: time.Second, AgentBinary: filepath.Join(dir, "must-not-run")}); err == nil || !strings.Contains(err.Error(), "cannot start shiki-agent") {
		if c != nil {
			c.Close()
		}
		t.Fatalf("released lock did not permit a startup attempt: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "instance.lock"))
	if err != nil || string(data) != "12345\n" {
		t.Fatal("frontend changed instance lock record")
	}
}

func TestPrivateLogFIFORejectedWithoutReader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.log")
	if err := syscall.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		file, err := privateFile(path, os.O_WRONLY|os.O_APPEND)
		if file != nil {
			file.Close()
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("FIFO accepted as agent log")
		}
	case <-time.After(time.Second):
		peer, err := os.OpenFile(path, os.O_RDWR|syscall.O_NONBLOCK, 0)
		if err == nil {
			peer.Close()
		}
		t.Fatal("opening log FIFO waited for a reader")
	}
}
