//go:build linux

package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestPrivateKeyringSecretFilePreservesPasswordAndRejectsUnsafeInputs(t *testing.T) {
	account, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	path := filepath.Join(root, "private-input")
	if err = os.WriteFile(path, []byte("  fixture password \t \r\n"), 0600); err != nil {
		t.Fatal(err)
	}
	secret, err := keyringSecretReader(account.Username, false, path, false)(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(secret, []byte("  fixture password \t ")) {
		wipeSecret(secret)
		t.Fatal("password whitespace was silently altered")
	}
	wipeSecret(secret)
	if err = os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err = readKeyringSecretFile(path, account.Username); err == nil {
		t.Fatal("public secret input accepted")
	}
	if err = os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "alias")
	if err = os.Link(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err = readKeyringSecretFile(path, account.Username); err == nil {
		t.Fatal("hardlinked secret input accepted")
	}
	if err = os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err = readKeyringSecretFile(link, account.Username); err == nil {
		t.Fatal("symlink secret input accepted")
	}
	fifo := filepath.Join(root, "fifo")
	if err = unix.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := readKeyringSecretFile(fifo, account.Username); done <- err }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("FIFO secret file accepted")
		}
	case <-time.After(time.Second):
		t.Fatal("secret file reader blocked on FIFO")
	}
	if err = os.WriteFile(path, []byte("sensitive-fixture\nextra"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = keyringSecretReader(account.Username, false, path, false)(t.Context()); err == nil || strings.Contains(err.Error(), "sensitive-fixture") {
		t.Fatal("invalid password was accepted or echoed in its error")
	}
}

func TestSecretStreamCancellationDoesNotRequireEOF(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := readSecretStream(ctx, reader); !errors.Is(err, context.Canceled) {
		t.Fatal("secret input ignored cancellation", err)
	}
}

func TestBootUnlockHelpAndPlanningNeverReadSecretFile(t *testing.T) {
	fifo := filepath.Join(t.TempDir(), "unreadable-secret-fifo")
	if err := unix.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	for _, operation := range []string{"setup", "maintenance"} {
		var out, diagnostics bytes.Buffer
		code, err := execute(t.Context(), []string{operation, "--user=shiki-missing-keyring-help-user", "--boot-unlock", "--keyring-mode=dedicated", "--keyring-secret-file=" + fifo, "--yes", "--help"}, &out, &diagnostics)
		if code != 0 || err != nil || diagnostics.Len() != 0 || !strings.Contains(out.String(), "whole-disk") {
			t.Fatalf("help touched secret/account or omitted threat boundary: %d %v", code, err)
		}
	}
	for _, args := range [][]string{{"setup", "--keyring-password=do-not-accept"}, {"maintenance", "--keyring-secret-stdin"}, {"setup", "--boot-unlock", "--keyring-mode=dedicated", "--reuse-keyring-credential", "--keyring-secret-file=" + fifo}} {
		var out, diagnostics bytes.Buffer
		code, err := execute(t.Context(), args, &out, &diagnostics)
		if code != 2 || err == nil || out.Len() != 0 {
			t.Fatal("unsafe/ambiguous credential flags accepted", args, code, err)
		}
	}
}

func TestUnconfirmedBootPlanDoesNotConsumePrivateSecretInput(t *testing.T) {
	account, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	if account.Uid == "0" {
		account, err = user.Lookup("nobody")
		if err != nil {
			t.Skip("fixture requires an ordinary account")
		}
	}
	root := t.TempDir()
	if err = os.Mkdir(filepath.Join(root, "etc"), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "etc/os-release"), []byte("ID=debian\nVERSION_ID=12\n"), 0600); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(t.TempDir(), "secret-must-not-be-read")
	if err = unix.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	var out, diagnostics bytes.Buffer
	code, err := execute(t.Context(), []string{"setup", "--component=browser", "--user=" + account.Username, "--boot-unlock", "--keyring-mode=dedicated", "--keyring-secret-file=" + fifo, "--stage-only", "--root-prefix=" + root}, &out, &diagnostics)
	if code != 0 || err != nil || diagnostics.Len() != 0 || !strings.Contains(out.String(), `"boot_unlock": true`) {
		t.Fatalf("unconfirmed plan touched secret input: %d %v %s", code, err, diagnostics.String())
	}
	if _, err = os.Lstat(filepath.Join(root, "usr")); !os.IsNotExist(err) {
		t.Fatal("planning activated or staged a runtime", err)
	}
}
