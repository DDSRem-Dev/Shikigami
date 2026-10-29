package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestHelpPrecedesAccountCredentialAndInstallationAccess(t *testing.T) {
	root := t.TempDir()
	key := filepath.Join(root, "unreadable-key")
	if err := unix.Mkfifo(key, 0600); err != nil {
		t.Fatal(err)
	}
	prefix := filepath.Join(root, "must-not-exist")
	for _, operation := range []string{"install", "rollback", "uninstall", "status", "probe"} {
		var out, diagnostics bytes.Buffer
		code, err := execute(t.Context(), []string{operation, "--role=daemon", "--user=shiki-nonexistent-help-account", "--prefix", prefix, "--tailscale-key-file", key, "--help"}, &out, &diagnostics)
		if code != 0 || err != nil || !strings.Contains(out.String(), "Usage: shiki-install "+operation) || !strings.Contains(out.String(), "-prefix") || diagnostics.Len() != 0 {
			t.Fatalf("%s help failed: code=%d error=%v stdout=%q stderr=%q", operation, code, err, out.String(), diagnostics.String())
		}
		if operation != "install" && (strings.Contains(out.String(), "-stage-only") || strings.Contains(out.String(), "-bundle") || strings.Contains(out.String(), "-config")) {
			t.Fatal("maintenance help advertises install-only flags", out.String())
		}
		if _, err := os.Lstat(prefix); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("help touched installation state", err)
		}
	}
}

func TestHelpRoutesAndVersionRejectUnknownArguments(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"-h"}, {"help"}, {"help", "version"}, {"help", "install"}, {"version", "--help"}} {
		var out, diagnostics bytes.Buffer
		code, err := execute(t.Context(), args, &out, &diagnostics)
		if code != 0 || err != nil || !strings.Contains(out.String(), "Usage:") || diagnostics.Len() != 0 || strings.Contains(out.String(), "_user-") {
			t.Fatalf("%v: code=%d error=%v stdout=%q stderr=%q", args, code, err, out.String(), diagnostics.String())
		}
	}
	for _, args := range [][]string{{"version", "--unknown"}, {"--version", "extra"}, {"help", "_user-init"}, {"missing", "--help"}, {"help", "install", "extra"}, {"--help", "extra"}} {
		code, err := execute(t.Context(), args, io.Discard, io.Discard)
		if code != 2 || err == nil {
			t.Fatalf("invalid arguments accepted: %v code=%d error=%v", args, code, err)
		}
	}
}

func TestMaintenanceRejectsStageAndIgnoredInstallFlagsBeforeAccess(t *testing.T) {
	for _, operation := range []string{"rollback", "uninstall", "status", "probe"} {
		for _, option := range []string{"--stage-only", "--stage-only=false", "--config=/must-not-read", "--bundle=/must-not-read", "--version=unused", "--state-dir=/must-not-change"} {
			prefix := filepath.Join(t.TempDir(), "must-not-exist")
			code, err := execute(t.Context(), []string{operation, "--user=shiki-nonexistent-help-account", "--prefix", prefix, option}, io.Discard, io.Discard)
			if code != 2 || err == nil || !strings.Contains(err.Error(), "does not apply to "+operation) {
				t.Fatalf("ignored maintenance option reached account/installation access: %s %s: %d %v", operation, option, code, err)
			}
			if _, err := os.Lstat(prefix); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("invalid maintenance option created installation", err)
			}
		}
	}
}
