package tmux

import (
	"context"
	"io/fs"
	"os"
	"testing"
	"time"
)

type fakeShellFile struct {
	mode fs.FileMode
}

func (f fakeShellFile) Name() string       { return "shell" }
func (f fakeShellFile) Size() int64        { return 1 }
func (f fakeShellFile) Mode() fs.FileMode  { return f.mode }
func (f fakeShellFile) ModTime() time.Time { return time.Time{} }
func (f fakeShellFile) IsDir() bool        { return f.mode.IsDir() }
func (f fakeShellFile) Sys() any           { return nil }

func fakeStatMap(entries map[string]fs.FileMode) func(string) (os.FileInfo, error) {
	return func(path string) (os.FileInfo, error) {
		mode, ok := entries[path]
		if !ok {
			return nil, os.ErrNotExist
		}
		return fakeShellFile{mode: mode}, nil
	}
}

func TestResolveInteractiveShellSelectionAndSecurityBoundaries(t *testing.T) {
	shells := []byte("# valid login shells\n/bin/sh\n/bin/dash\n/bin/bash\n/usr/bin/zsh\n/usr/bin/fish\n")

	t.Run("preserves configured rich login shell from passwd", func(t *testing.T) {
		passwd := []byte("root:x:0:0:root:/root:/bin/bash\nshiki:x:1000:1000::/home/shiki:/usr/bin/zsh\n")
		stat := fakeStatMap(map[string]fs.FileMode{
			"/bin/sh":      0755,
			"/bin/bash":    0755,
			"/usr/bin/zsh": 0755,
		})
		if got := resolveInteractiveShell(passwd, shells, "/bin/sh", 1000, stat); got != "/usr/bin/zsh" {
			t.Fatalf("got %q, want /usr/bin/zsh", got)
		}
	})

	t.Run("upgrades minimal sh or dash account default to bash when available", func(t *testing.T) {
		for _, minimal := range []string{"/bin/sh", "/bin/dash"} {
			passwd := []byte("shiki:x:1000:1000::/home/shiki:" + minimal + "\n")
			stat := fakeStatMap(map[string]fs.FileMode{
				"/bin/sh":   0755,
				"/bin/dash": 0755,
				"/bin/bash": 0755,
			})
			if got := resolveInteractiveShell(passwd, shells, minimal, 1000, stat); got != "/bin/bash" {
				t.Fatalf("for %s: got %q, want /bin/bash", minimal, got)
			}
		}
	})

	t.Run("falls back to sh when bash is absent", func(t *testing.T) {
		passwd := []byte("shiki:x:1000:1000::/home/shiki:/bin/sh\n")
		stat := fakeStatMap(map[string]fs.FileMode{
			"/bin/sh": 0755,
		})
		if got := resolveInteractiveShell(passwd, shells, "", 1000, stat); got != "/bin/sh" {
			t.Fatalf("got %q, want /bin/sh", got)
		}
	})

	t.Run("rejects nologin false untrusted env paths and non-executable files", func(t *testing.T) {
		passwd := []byte("shiki:x:1000:1000::/home/shiki:/usr/sbin/nologin\n")
		stat := fakeStatMap(map[string]fs.FileMode{
			"/usr/sbin/nologin": 0755,
			"/tmp/evil-shell":   0755,
			"/bin/bash":         0644, // not executable
			"/usr/bin/bash":     fs.ModeDir | 0755,
			"/bin/sh":           0755,
		})
		if got := resolveInteractiveShell(passwd, shells, "/tmp/evil-shell", 1000, stat); got != "/bin/sh" {
			t.Fatalf("got %q, want /bin/sh", got)
		}
	})

	t.Run("uses validated SHELL env when account is absent from passwd", func(t *testing.T) {
		stat := fakeStatMap(map[string]fs.FileMode{
			"/usr/bin/fish": 0755,
			"/bin/bash":     0755,
			"/bin/sh":       0755,
		})
		if got := resolveInteractiveShell(nil, shells, "/usr/bin/fish", 501, stat); got != "/usr/bin/fish" {
			t.Fatalf("got %q, want /usr/bin/fish", got)
		}
	})
}

func TestDefaultShellSupportsArrowKeyCursorEditing(t *testing.T) {
	if _, err := os.Stat("/bin/bash"); err != nil {
		if _, zshErr := os.Stat("/bin/zsh"); zshErr != nil {
			t.Skip("neither /bin/bash nor /bin/zsh installed for line-editing check")
		}
	}
	t.Setenv("HOME", t.TempDir())
	c := testClient(t)
	if isMinimalBourneShell(c.Shell()) {
		t.Fatalf("InteractiveShell resolved minimal shell %q despite rich shell availability", c.Shell())
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()

	// Verify both empty-argv Create (--no-claude) and Stop (stop-claude fallback).
	pane, err := c.Create(ctx, "sess_arrow", t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	verifyArrowKeyLineEdit(t, ctx, c, pane.SessionID, "CREATE")

	if err := c.Respawn(ctx, pane.SessionID, []string{"/bin/sleep", "30"}); err != nil {
		t.Fatal(err)
	}
	if err := c.Stop(ctx, pane.SessionID); err != nil {
		t.Fatal(err)
	}
	verifyArrowKeyLineEdit(t, ctx, c, pane.SessionID, "STOP")
}

func verifyArrowKeyLineEdit(t *testing.T, ctx context.Context, c *Client, sessionID, tag string) {
	t.Helper()
	a, err := c.Attach(ctx, sessionID, 24, 80)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	// Use split arguments so the readiness marker only appears after the shell
	// has actually executed printf, never from kernel TTY input echo.
	if _, err := a.Write([]byte("printf 'SHIKI_READY_%s_%s\\n' '" + tag + "' 'OK'\r")); err != nil {
		t.Fatal(err)
	}
	readUntil(t, a, []byte("SHIKI_READY_"+tag+"_OK"))

	// Type "SHIKI_LINE=ac", press Left arrow (\x1b[D), insert "b", and print.
	// Minimal dash/sh leaves the escape bytes inside SHIKI_LINE instead of "abc".
	input := "SHIKI_LINE=ac\x1b[Db; printf 'EDITED_%s:%s\\n' '" + tag + "' \"$SHIKI_LINE\"\r"
	if _, err := a.Write([]byte(input)); err != nil {
		t.Fatal(err)
	}
	readUntil(t, a, []byte("EDITED_"+tag+":abc"))
}
