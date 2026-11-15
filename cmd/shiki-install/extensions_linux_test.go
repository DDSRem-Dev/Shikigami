//go:build linux

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetupPrintsPlanBeforeConfirmationWithoutHostActions(t *testing.T) {
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
	if err = os.WriteFile(filepath.Join(root, "etc/os-release"), []byte("ID=ubuntu\nVERSION_ID=24.04\n"), 0600); err != nil {
		t.Fatal(err)
	}
	args := []string{"setup", "--component=all", "--user=" + account.Username, "--stage-only", "--root-prefix=" + root}
	for _, confirmed := range []bool{false, true} {
		var out, diagnostics bytes.Buffer
		invocation := append([]string{}, args...)
		if confirmed {
			invocation = append(invocation, "--yes")
		}
		code, callErr := execute(t.Context(), invocation, &out, &diagnostics)
		if (!confirmed && (code != 0 || callErr != nil)) || (confirmed && (code != 1 || callErr == nil)) {
			t.Fatalf("unexpected confirmation result: %d %v", code, callErr)
		}
		var plan map[string]json.RawMessage
		if err = json.Unmarshal(out.Bytes(), &plan); err != nil {
			t.Fatal("plan was not printed before apply refusal", err, out.String())
		}
		if len(plan["blocked"]) == 0 || !strings.Contains(out.String(), "dl.google.com") {
			t.Fatal("missing source/trust blockers", out.String())
		}
		if _, err = os.Lstat(filepath.Join(root, "usr")); !os.IsNotExist(err) {
			t.Fatal("planning or refused apply modified fixture", err)
		}
	}
}

func TestExtensionHelpDoesNotInspectAccountsOrKeys(t *testing.T) {
	for _, operation := range []string{"setup", "maintenance"} {
		var out, diagnostics bytes.Buffer
		code, err := execute(t.Context(), []string{operation, "--user=shiki-nonexistent-extension-help", "--yes", "--help"}, &out, &diagnostics)
		if code != 0 || err != nil || diagnostics.Len() != 0 || !strings.Contains(out.String(), "No public network") {
			t.Fatalf("help touched dependencies: %d %v %s", code, err, diagnostics.String())
		}
	}
}

func TestExtensionCommandsRejectArbitraryPackageSourceAndRootFlags(t *testing.T) {
	for _, args := range [][]string{{"setup", "--package=arbitrary"}, {"setup", "--source=https://arbitrary.invalid"}, {"maintenance", "--root-prefix=/must-not-write"}, {"maintenance", "--bundle=/must-not-read"}} {
		var out, diagnostics bytes.Buffer
		code, err := execute(t.Context(), args, &out, &diagnostics)
		if code != 2 || err == nil || out.Len() != 0 {
			t.Fatalf("arbitrary installation inputs accepted: %v code=%d err=%v", args, code, err)
		}
	}
}
