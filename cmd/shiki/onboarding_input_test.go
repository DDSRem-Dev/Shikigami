//go:build darwin || linux

package main

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"
)

func TestOnboardingAddressPromptRecoversPortOnlyInput(t *testing.T) {
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	process := smartPTYProcess{t: t, master: master, slave: slave}
	defer process.close()
	fd := int(master.Fd())
	if err := unix.SetNonblock(fd, true); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	a := &app{input: slave, output: slave, errorOutput: slave}
	var address string
	done := process.start(cancel, func() error { var err error; address, err = a.promptTailnetAddress(ctx, ""); return err })
	smartReadPTY(t, ctx, fd, "VPS Tailscale IP")
	if _, err := master.WriteString("9527\n"); err != nil {
		t.Fatal(err)
	}
	smartReadPTY(t, ctx, fd, "not a port number")
	if _, err := master.WriteString("100.71.151.36\n"); err != nil {
		t.Fatal(err)
	}
	if err := smartWaitPTY(ctx, fd, process.finished); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil || address != "100.71.151.36:9527" {
		t.Fatal(address, err)
	}
}

func TestOnboardingCredentialEnterAndControlCRestoreTerminal(t *testing.T) {
	for _, cancelInput := range []bool{false, true} {
		name := "enter"
		if cancelInput {
			name = "control-c"
		}
		t.Run(name, func(t *testing.T) {
			master, slave, err := pty.Open()
			if err != nil {
				t.Fatal(err)
			}
			process := smartPTYProcess{t: t, master: master, slave: slave}
			defer process.close()
			fd := int(master.Fd())
			if err := unix.SetNonblock(fd, true); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			a := &app{input: slave, output: slave, errorOutput: slave}
			var value []byte
			done := process.start(cancel, func() error { var err error; value, err = a.credential(ctx, &serverFlags{}, true); return err })
			instructions := smartReadPTY(t, ctx, fd, "Connection token (hidden):")
			if !strings.Contains(instructions, "No characters or asterisks") || !strings.Contains(instructions, "Ctrl+C") {
				t.Fatal("missing credential instructions", instructions)
			}
			// Wait for raw mode before injecting secret bytes, so the test also proves
			// they are never echoed by a still-canonical PTY.
			waitSecretRaw(t, ctx, slave)
			input := "shiki_private-fixture-token\r"
			if cancelInput {
				input = "shiki_partial\x03"
			}
			if _, err := master.WriteString(input); err != nil {
				t.Fatal(err)
			}
			if err := smartWaitPTY(ctx, fd, process.finished); err != nil {
				t.Fatal(err)
			}
			err = <-done
			if cancelInput {
				if !errors.Is(err, context.Canceled) || value != nil {
					t.Fatal("Ctrl+C did not cancel secret input", err)
				}
			} else if err != nil || string(value) != "shiki_private-fixture-token" {
				t.Fatal("Enter did not submit token", err)
			}
			clear(value)
			state, err := onboardingTermios(int(slave.Fd()))
			if err != nil || state.Lflag&unix.ECHO == 0 || state.Lflag&unix.ICANON == 0 {
				t.Fatal("terminal not restored", err)
			}
		})
	}
}

func waitSecretRaw(t *testing.T, ctx context.Context, input *os.File) {
	t.Helper()
	for {
		state, err := onboardingTermios(int(input.Fd()))
		if err != nil {
			t.Fatal(err)
		}
		if state.Lflag&unix.ECHO == 0 && state.Lflag&unix.ICANON == 0 {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(time.Millisecond):
		}
	}
}
