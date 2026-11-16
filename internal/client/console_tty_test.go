//go:build darwin || linux

package client

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"
)

// Unlike an already-open PTY slave, /dev/tty is not pollable on Darwin. Exercise
// the exact descriptor used by installer and setup prompts in a real session.
func TestConsoleControllingTerminal(t *testing.T) {
	testConsoleControllingTerminal(t, false)
}

// Opt in only in the disposable Linux regression container / CI runner.
func TestConsoleSudoPipe(t *testing.T) {
	if runtime.GOOS != "linux" || os.Getenv("SHIKI_TEST_SUDO_PTY") != "1" {
		t.Skip("requires explicit Linux sudo PTY regression environment")
	}
	testConsoleControllingTerminal(t, true)
}

func testConsoleControllingTerminal(t *testing.T, sudo bool) {
	for _, mode := range []string{"answer", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestConsoleControllingTerminalHelper$")
			command.Env = append(os.Environ(), "SHIKI_TEST_CONSOLE_TTY="+mode)
			if sudo {
				// Mirror curl | sudo bash and bootstrap's background supervisor.
				// sudo's use_pty monitor initially owns the foreground group.
				script := `SHIKI_TEST_CONSOLE_SUDO=1 SHIKI_TEST_CONSOLE_TTY="$2" "$1" -test.run=^TestConsoleControllingTerminalHelper$ <&0 &
child=$!
wait "$child"`
				command = exec.CommandContext(ctx, "bash", "-c", `printf '%s\n' "$1" | sudo -n bash -s -- "$2" "$3"`, "shiki-test", script, os.Args[0], mode)
			}
			master, err := pty.Start(command)
			if err != nil {
				t.Fatal(err)
			}
			defer master.Close()
			defer func() { _ = command.Process.Kill(); _ = command.Wait() }()
			reader, err := openTerminalFD(master)
			if err != nil {
				t.Fatal(err)
			}
			defer reader.Close()
			waitConsoleMarker(t, ctx, reader, "CONFIRM")
			if mode == "answer" {
				if err = reader.Write(ctx, []byte("yes\n")); err != nil {
					t.Fatal(err)
				}
				waitConsoleMarker(t, ctx, reader, "SECRET-READY")
				if err = reader.Write(ctx, []byte("private-test-input\n")); err != nil {
					t.Fatal(err)
				}
			}
			waitConsoleMarker(t, ctx, reader, "RESTORED")
			// Drain final test output before waiting: closing a Darwin controlling
			// terminal may wait for its output queue to be consumed.
			data := make([]byte, 4096)
			for {
				if _, err = reader.Read(ctx, data); err != nil {
					break
				}
			}
			if err = command.Wait(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestConsoleControllingTerminalHelper(t *testing.T) {
	mode := os.Getenv("SHIKI_TEST_CONSOLE_TTY")
	if mode == "" {
		return
	}
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer tty.Close()
	if os.Getenv("SHIKI_TEST_CONSOLE_SUDO") == "1" {
		foreground, err := unix.IoctlGetInt(int(tty.Fd()), unix.TIOCGPGRP)
		if err != nil || foreground == unix.Getpgrp() {
			t.Fatalf("sudo regression must start outside terminal foreground group (enable sudo use_pty): foreground=%d own=%d error=%v", foreground, unix.Getpgrp(), err)
		}
	}
	before, err := consoleTerminalState(int(tty.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	flags, err := unix.FcntlInt(tty.Fd(), unix.F_GETFL, 0)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	fmt.Fprintln(tty, "CONFIRM")
	if mode == "cancel" {
		short, stop := context.WithTimeout(ctx, 100*time.Millisecond)
		defer stop()
		if _, err = ReadConsoleLine(short, tty, 32); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("confirmation cancellation: %v", err)
		}
		secretCtx, stopSecret := context.WithTimeout(ctx, 100*time.Millisecond)
		defer stopSecret()
		if _, err = ReadSecret(secretCtx, tty, 64); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("secret cancellation: %v", err)
		}
	} else {
		answer, err := ReadConsoleLine(ctx, tty, 32)
		if err != nil || answer != "yes" {
			t.Fatalf("confirmation: %q %v", answer, err)
		}
		fd := int(tty.Fd())
		ready := make(chan struct{})
		go func() {
			defer close(ready)
			for {
				state, e := consoleTerminalState(fd)
				if e == nil && state.Lflag&unix.ECHO == 0 {
					fmt.Fprintln(tty, "SECRET-READY")
					return
				}
				select {
				case <-ctx.Done():
					return
				case <-time.After(time.Millisecond):
				}
			}
		}()
		secret, err := ReadSecret(ctx, tty, 64)
		<-ready
		if err != nil || string(secret) != "private-test-input" {
			t.Fatalf("secret read failed: %v", err)
		}
		clear(secret)
	}
	after, err := consoleTerminalState(int(tty.Fd()))
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("terminal mode not restored: before=%#v after=%#v error=%v", before, after, err)
	}
	restored, err := unix.FcntlInt(tty.Fd(), unix.F_GETFL, 0)
	if err != nil || (flags^restored)&(unix.O_NONBLOCK|unix.O_APPEND|unix.O_ASYNC) != 0 {
		t.Fatalf("descriptor flags not restored: %x -> %x (%v)", flags, restored, err)
	}
	fmt.Fprintln(tty, "RESTORED")
}

func waitConsoleMarker(t *testing.T, ctx context.Context, reader *terminalFD, marker string) {
	t.Helper()
	var output []byte
	data := make([]byte, 4096)
	for !bytes.Contains(output, []byte(marker)) {
		n, err := reader.Read(ctx, data)
		if n > 0 {
			output = append(output, data[:n]...)
		}
		if bytes.Contains(output, []byte("private-test-input")) {
			t.Fatal("hidden input was echoed")
		}
		if err != nil {
			t.Fatalf("waiting for %s in %q: %v", marker, output, err)
		}
	}
}
