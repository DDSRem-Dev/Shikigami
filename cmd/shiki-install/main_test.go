package main

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/DDSRem-Dev/Shikigami/internal/install"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestInvalidArgumentsDoNotCreateInstallation(t *testing.T) {
	for _, arguments := range [][]string{{"install", "--unknown"}, {"install", "--role=invalid"}, {"install", "--stage-only", "--version=v1"}, {"status"}, {"rollback"}, {"uninstall"}, {"probe"}} {
		prefix := filepath.Join(t.TempDir(), "must-not-exist")
		args := append(append([]string{}, arguments...), "--prefix", prefix)
		var output bytes.Buffer
		code, err := execute(context.Background(), args, &output, &output)
		if err == nil || code == 0 {
			t.Fatal("invalid installation arguments succeeded")
		}
		if _, err = os.Stat(prefix); !os.IsNotExist(err) {
			t.Fatal("invalid arguments created installation state")
		}
	}
}

func TestProbeRejectsInterruptedOrMismatchedCurrentBeforeRunningHelper(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("client installer requires its ordinary user")
	}
	for _, pending := range []bool{true, false} {
		t.Run(map[bool]string{true: "interrupted", false: "mismatched-current"}[pending], func(t *testing.T) {
			prefix := filepath.Join(t.TempDir(), "install")
			artifacts, err := install.OpenArtifacts(prefix)
			if err != nil {
				t.Fatal(err)
			}
			artifacts.Close()
			a := install.Selection{Release: "releases/v1-" + runtime.GOOS + "-" + runtime.GOARCH, Version: "v1", Checksums: strings.Repeat("0", 64)}
			b := install.Selection{Release: "releases/v2-" + runtime.GOOS + "-" + runtime.GOARCH, Version: "v2", Checksums: strings.Repeat("1", 64)}
			receipt := install.Receipt{Format: 1, Active: a}
			if pending {
				receipt.Pending = &b
				receipt.PendingAction = "install"
			}
			data, _ := json.Marshal(receipt)
			if err = os.WriteFile(filepath.Join(prefix, "installation.json"), data, 0600); err != nil {
				t.Fatal(err)
			}
			if err = os.Symlink(b.Release, filepath.Join(prefix, "current")); err != nil {
				t.Fatal(err)
			}
			var output bytes.Buffer
			code, err := execute(t.Context(), []string{"probe", "--prefix", prefix, "--non-interactive"}, &output, &output)
			want := "active link differs"
			if pending {
				want = "interrupted installation"
			}
			if code != 1 || err == nil || !strings.Contains(err.Error(), want) || output.Len() != 0 {
				t.Fatal("probe passed an uncommitted release to its helper", code, err, output.String())
			}
		})
	}
}
func TestKeyReadRejectsFIFOAndPublicFileWithoutBlocking(t *testing.T) {
	account, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "key")
	if err = unix.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := readKey(path, account.Username); done <- err }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("FIFO key accepted")
		}
	case <-time.After(time.Second):
		t.Fatal("FIFO key read blocked")
	}
	os.Remove(path)
	if err = os.WriteFile(path, []byte("secret-test-key"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err = readKey(path, account.Username); err == nil {
		t.Fatal("public key file accepted")
	}
	if err = os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	key, err := readKey(path, account.Username)
	if err != nil || string(key) != "secret-test-key" {
		t.Fatal("valid private key read failed")
	}
	clear(key)
}
