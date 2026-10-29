package install

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestShellBootstrapVerifiesPrivateCopyAndRejectsTampering(t *testing.T) {
	bundle := t.TempDir()
	payload := []byte("#!/bin/bash\nprintf 'verified-helper\\n'\n")
	if err := os.WriteFile(filepath.Join(bundle, "shiki-install"), payload, 0700); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(payload)
	checksums := []byte(hex.EncodeToString(sum[:]) + "  shiki-install\n")
	if err := os.WriteFile(filepath.Join(bundle, ChecksumsName), checksums, 0600); err != nil {
		t.Fatal(err)
	}
	pin := digest(checksums)
	wrapper, err := filepath.Abs("../../scripts/install-client.sh")
	if err != nil {
		t.Fatal(err)
	}
	invoke := func() ([]byte, error) {
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "/bin/bash", wrapper, "--bundle", bundle, "--checksums-sha256", pin)
		cmd.WaitDelay = time.Second
		return cmd.CombinedOutput()
	}
	out, err := invoke()
	if err != nil || !strings.Contains(string(out), "verified-helper") {
		t.Fatalf("verified bootstrap failed: %s: %v", out, err)
	}
	if err = os.WriteFile(filepath.Join(bundle, "shiki-install"), []byte("changed"), 0700); err != nil {
		t.Fatal(err)
	}
	out, err = invoke()
	if err == nil || strings.Contains(string(out), "verified-helper") {
		t.Fatal("tampered helper was executed")
	}
	if err = os.Remove(filepath.Join(bundle, ChecksumsName)); err != nil {
		t.Fatal(err)
	}
	if err = unix.Mkfifo(filepath.Join(bundle, ChecksumsName), 0600); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if _, err = invoke(); err == nil || time.Since(started) > time.Second {
		t.Fatal("FIFO bootstrap did not fail promptly")
	}
}
func TestChildOutputCaptureIsBounded(t *testing.T) {
	output := &cappedOutput{limit: 1024}
	data := []byte(strings.Repeat("x", 1<<20))
	if n, err := output.Write(data); err != nil || n != len(data) {
		t.Fatal("bounded drain stopped consuming child output")
	}
	captured, truncated := output.Bytes()
	if !truncated || len(captured) != 1024 {
		t.Fatal("child output was not bounded")
	}
}
