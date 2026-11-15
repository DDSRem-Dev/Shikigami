package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DDSRem-Dev/Shikigami/internal/auth"
	"github.com/DDSRem-Dev/Shikigami/internal/install"
	"github.com/DDSRem-Dev/Shikigami/pkg/config"
	"github.com/creack/pty"
)

func TestQuickstartHelpAndInvalidOptionsHaveNoSideEffects(t *testing.T) {
	for _, args := range [][]string{{"quickstart", "--help"}, {"help", "quickstart"}} {
		var out bytes.Buffer
		code, err := execute(t.Context(), args, &out, io.Discard)
		if code != 0 || err != nil || !strings.Contains(out.String(), "Interactive guided installation") {
			t.Fatalf("%v: %d %v %s", args, code, err, out.String())
		}
	}
	for _, args := range [][]string{
		{"--role=root"}, {"--components=unknown"}, {"--yes"}, {"--non-interactive"},
		{"--role=client", "--user=root", "--bundle=/abs", "--version=v1", "--checksums-sha256=pin"},
		{"--role=daemon", "--bundle=/abs", "--version=v1", "--checksums-sha256=pin"},
	} {
		if _, _, err := parseQuickstart(args, io.Discard); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}

func TestQuickstartClientRepeatAcceptsInstallerDefaults(t *testing.T) {
	account, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	if account.Uid == "0" {
		t.Skip("client requires ordinary user")
	}
	plan := install.Plan{Role: "client", User: account.Username, Port: 9527, Claude: "claude"}
	prefix := ""
	if err = defaults(&plan, &prefix); err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	receipt := install.Receipt{Format: 1, Plan: plan, Active: install.Selection{Release: "releases/fixed"}}
	data, _ := json.Marshal(receipt)
	if err = os.WriteFile(filepath.Join(directory, "installation.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err = quickstartExistingPlan(directory, plan); err != nil {
		t.Fatal("repeat rejected default installation", err)
	}
	plan.Port = 0
	if err = quickstartExistingPlan(directory, plan); err == nil {
		t.Fatal("ignored installation default drift")
	}
}

func TestQuickstartTokenCheckUsesActualSignatureAndRevocation(t *testing.T) {
	account, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	if account.Uid == "0" {
		t.Skip("token helper requires ordinary daemon UID")
	}
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(root, "state")
	if err = os.Mkdir(state, 0700); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultDaemon(account.Username, state)
	configPath := filepath.Join(root, "daemon.yaml")
	if err = config.SaveDaemon(configPath, cfg); err != nil {
		t.Fatal(err)
	}
	manager, err := auth.Open(filepath.Join(state, "auth"), account.Username)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	if err = os.WriteFile(filepath.Join(state, "state.json"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	token, info, err := manager.Create(account.Username, []string{"*"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(state, "token")
	if err = os.WriteFile(path, []byte(token), 0600); err != nil {
		t.Fatal(err)
	}
	check := func(want bool) {
		t.Helper()
		var out bytes.Buffer
		code, err := quickstartTokenCheck([]string{"--config=" + configPath, "--token-file=" + path}, &out)
		var result struct {
			Valid bool `json:"valid"`
		}
		if code != 0 || err != nil || json.Unmarshal(out.Bytes(), &result) != nil || result.Valid != want {
			t.Fatalf("token check %d %v %s", code, err, out.String())
		}
		if strings.Contains(out.String(), token) {
			t.Fatal("credential leaked")
		}
	}
	check(true)
	if err = manager.Revoke(info.ID); err != nil {
		t.Fatal(err)
	}
	check(false)
	if err = os.WriteFile(path, []byte("shiki_invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	check(false)
}

func TestQuickstartBashPATHDoesNotShadowExistingLoginProfile(t *testing.T) {
	t.Setenv("SHELL", "/bin/bash")
	home := t.TempDir()
	path := filepath.Join(home, ".profile")
	if err := os.WriteFile(path, []byte("export EXISTING=yes\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := quickstartShellPath(home); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".bash_profile")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("created shadowing login profile")
	}
	data, _ := os.ReadFile(path)
	if !bytes.Contains(data, []byte("export EXISTING=yes")) || !bytes.Contains(data, []byte(quickstartPathBlock)) {
		t.Fatal("existing startup was not preserved")
	}
}

func TestQuickstartNodeResumesExistingLoginAndHandlesRace(t *testing.T) {
	live := quickstartNodeStatus{State: "needs_login", Reason: "NODE_AUTH_REQUIRED", LoginID: "existing", Expires: time.Now().Add(time.Minute).Unix()}
	online := quickstartNodeStatus{State: "online", Addresses: []string{"100.64.1.2"}}
	for _, race := range []bool{false, true} {
		calls, begins := 0, 0
		inspect := func() (quickstartNodeStatus, error) {
			calls++
			if race && calls == 1 {
				return quickstartNodeStatus{State: "needs_login", Reason: "NODE_AUTH_REQUIRED"}, nil
			}
			if calls <= 2 {
				return live, nil
			}
			return online, nil
		}
		begin := func() error { begins++; return errors.New("another frontend won BeginLogin") }
		addresses, err := quickstartWaitNode(t.Context(), inspect, begin, io.Discard, time.Millisecond)
		if err != nil || len(addresses) != 1 || begins != map[bool]int{false: 0, true: 1}[race] {
			t.Fatalf("race=%v addresses=%v begins=%d err=%v", race, addresses, begins, err)
		}
	}
}

func TestQuickstartNodeStopsOnExpiredAttemptAndCancellation(t *testing.T) {
	live := quickstartNodeStatus{State: "authenticating", LoginID: "attempt", Expires: time.Now().Add(time.Minute).Unix()}
	calls := 0
	_, err := quickstartWaitNode(t.Context(), func() (quickstartNodeStatus, error) {
		calls++
		if calls == 1 {
			return live, nil
		}
		live.Expires = time.Now().Add(-time.Second).Unix()
		return live, nil
	}, func() error { t.Fatal("restarted existing attempt"); return nil }, io.Discard, time.Millisecond)
	if err == nil || calls != 2 {
		t.Fatalf("terminal login kept polling: %d %v", calls, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = quickstartWaitNode(ctx, func() (quickstartNodeStatus, error) { t.Fatal("queried after cancellation"); return live, nil }, func() error { return nil }, io.Discard, time.Millisecond)
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestQuickstartSystemPlanRefusalNeverApplies(t *testing.T) {
	for _, accepted := range []bool{false, true} {
		master, slave, err := pty.Open()
		if err != nil {
			t.Fatal(err)
		}
		answer := "n\n"
		if accepted {
			answer = "yes\n"
		}
		if _, err = master.Write([]byte(answer)); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
		applied := false
		err = quickstartConfirmSystemPlan(ctx, slave, &install.ExtensionPlan{Operation: "setup", Component: "all"}, func() error { applied = true; return nil })
		cancel()
		slave.Close()
		master.Close()
		if applied != accepted || (err == nil) != accepted {
			t.Fatalf("accepted=%v applied=%v err=%v", accepted, applied, err)
		}
	}
}

func TestQuickstartPathPreservesContentAndRejectsLinksAndEdits(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "profile")
	original := "export CUSTOM=retained"
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := quickstartAppendPath(path); err != nil {
			t.Fatal(err)
		}
	}
	data, _ := os.ReadFile(path)
	if string(data) != original+"\n"+quickstartPathBlock {
		t.Fatalf("content changed: %q", data)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0644 {
		t.Fatal("existing mode changed")
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if err := quickstartAppendPath(link); err == nil {
		t.Fatal("accepted symlink")
	}
	hard := filepath.Join(root, "hard")
	if err := os.Link(path, hard); err != nil {
		t.Fatal(err)
	}
	if err := quickstartAppendPath(hard); err == nil {
		t.Fatal("accepted hardlink")
	}
	if err := os.Remove(hard); err != nil {
		t.Fatal(err)
	}
	edited := []byte("# >>> Shikigami managed PATH >>>\nexport PATH=custom\n")
	if err := os.WriteFile(path, edited, 0600); err != nil {
		t.Fatal(err)
	}
	if err := quickstartAppendPath(path); err == nil {
		t.Fatal("overwrote edited block")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(after, edited) {
		t.Fatal("edited content modified")
	}
}

type quickstartRoundTrip func(*http.Request) (*http.Response, error)

func (f quickstartRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestQuickstartClaudeDownloadBoundsAndHTTPS(t *testing.T) {
	for _, test := range []struct {
		name      string
		status    int
		body      string
		size      int64
		location  string
		wantError bool
	}{
		{"valid", 200, "#!/bin/bash\nexit 0\n", -1, "", false},
		{"empty", 200, "", -1, "", true},
		{"status", 503, "unavailable", -1, "", true},
		{"declared-size", 200, "script", 2 << 20, "", true},
		{"stream-size", 200, strings.Repeat("a", (1<<20)+1), -1, "", true},
		{"http-redirect", 302, "", -1, "http://example.com/install", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			transport := quickstartRoundTrip(func(r *http.Request) (*http.Response, error) {
				if r.URL.String() != "https://claude.ai/install.sh" {
					t.Fatalf("unexpected network target %s", r.URL)
				}
				h := http.Header{}
				if test.location != "" {
					h.Set("Location", test.location)
				}
				return &http.Response{StatusCode: test.status, Header: h, Body: io.NopCloser(strings.NewReader(test.body)), ContentLength: test.size, Request: r}, nil
			})
			data, err := quickstartClaudeScript(t.Context(), transport)
			if (err != nil) != test.wantError {
				t.Fatalf("error %v", err)
			}
			if !test.wantError && string(data) != test.body {
				t.Fatal("script changed")
			}
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := quickstartClaudeScript(ctx, quickstartRoundTrip(func(r *http.Request) (*http.Response, error) { return nil, r.Context().Err() }))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
}

func TestQuickstartPrivateStateRejectsPublicAndHardlinkedTokens(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := quickstartPrivateRead(path, uint32(os.Geteuid()), 1024); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := quickstartPrivateRead(path, uint32(os.Geteuid()), 1024); err == nil {
		t.Fatal("accepted public token")
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(path, path+".link"); err != nil {
		t.Fatal(err)
	}
	if _, err := quickstartPrivateRead(path, uint32(os.Geteuid()), 1024); err == nil {
		t.Fatal("accepted hardlinked token")
	}
}

func TestQuickstartInteractiveChildReadsForegroundPTY(t *testing.T) {
	if os.Getenv("SHIKI_QUICKSTART_PTY_CHILD") == "1" {
		account, err := user.Current()
		if err != nil {
			t.Fatal(err)
		}
		tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
		if err != nil {
			t.Fatal(err)
		}
		defer tty.Close()
		plan := install.Plan{User: account.Username, Home: account.HomeDir, Path: "/usr/bin:/bin"}
		err = quickstartCommand(t.Context(), plan, true, tty, nil, "/bin/sh", "-c", `printf 'INPUT_READY\n'; IFS= read -r answer; printf 'RECEIVED:%s\n' "$answer"`)
		if err != nil {
			t.Fatal(err)
		}
		return
	}
	if os.Geteuid() == 0 {
		t.Skip("PTY frontend requires an ordinary runtime user")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestQuickstartInteractiveChildReadsForegroundPTY$")
	cmd.Env = append(os.Environ(), "SHIKI_QUICKSTART_PTY_CHILD=1")
	master, err := pty.Start(cmd)
	if err != nil {
		t.Fatal(err)
	}
	defer master.Close()
	t.Logf("PTY child PID %d", cmd.Process.Pid)
	done := make(chan error, 1)
	go func() {
		var output bytes.Buffer
		buffer := make([]byte, 2048)
		sent := false
		for {
			n, err := master.Read(buffer)
			output.Write(buffer[:n])
			if !sent && strings.Contains(output.String(), "INPUT_READY") {
				_, _ = master.Write([]byte("foreground works\n"))
				sent = true
			}
			if strings.Contains(output.String(), "RECEIVED:foreground works") && done != nil {
				done <- nil
				// Continue draining until the child exits; some PTYs have small
				// output buffers and process exit must not depend on their size.
				for {
					if _, drainErr := master.Read(buffer); drainErr != nil {
						return
					}
				}
			}
			if err != nil {
				done <- fmt.Errorf("PTY ended before input was consumed: %w (%s)", err, output.String())
				return
			}
		}
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("interactive child hung, possibly stopped by SIGTTIN")
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
}
