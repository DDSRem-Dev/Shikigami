package session

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/DDSRem-Dev/Shikigami/internal/server/storage"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
)

func TestLaunchEnvironmentUsesObservedSnapshotWithoutGlobalTmuxMutation(t *testing.T) {
	store, err := storage.Open(filepath.Join(t.TempDir(), "state"), storage.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	calls := 0
	current := &LaunchEnvironment{Revision: "region-one", Values: map[string]string{"TZ": "Asia/Tokyo", "LANG": "ja_JP.UTF-8", "LANGUAGE": "ja:en", "LC_TIME": "ja_JP.UTF-8"}}
	manager := &Manager{store: store, opts: ManagerOptions{Environment: func(_ context.Context) (*LaunchEnvironment, error) {
		calls++
		return current, nil
	}}}
	argv := []string{"/bin/sh", "-c", `printf '%s|%s|%s|%s|%s' "$TZ" "$LANG" "$LANGUAGE" "$LC_TIME" "${LC_ALL-unset}"`}
	launch, err := manager.launchCommand(t.Context(), "sess_environment", argv, true)
	if err != nil {
		t.Fatal(err)
	}
	run := func(command []string) string {
		t.Helper()
		process := exec.Command(command[0], command[1:]...)
		process.Env = []string{"PATH=/usr/bin:/bin", "TZ=old", "LANG=old", "LANGUAGE=old", "LC_TIME=old", "LC_ALL=old"}
		output, err := process.Output()
		if err != nil {
			t.Fatal(err)
		}
		return string(output)
	}
	if output := run(launch); output != "Asia/Tokyo|ja_JP.UTF-8|ja:en|ja_JP.UTF-8|unset" {
		t.Fatal("stale inherited locale won over actual region", output)
	}
	current = &LaunchEnvironment{Revision: "region-two", Values: map[string]string{"TZ": "America/New_York", "LANG": "en_US.UTF-8", "LANGUAGE": "en"}}
	automatic, err := manager.launchCommand(t.Context(), "sess_environment", argv, false)
	if err != nil || !reflect.DeepEqual(automatic, launch) || calls != 1 {
		t.Fatal("automatic recovery picked up changed host configuration", automatic, calls, err)
	}
	explicit, err := manager.launchCommand(t.Context(), "sess_environment", argv, true)
	if err != nil || calls != 2 {
		t.Fatal(err, calls)
	}
	if output := run(explicit); output != "America/New_York|en_US.UTF-8|en||unset" {
		t.Fatal("explicit restart failed to refresh region", output)
	}
}

func TestLaunchEnvironmentRejectsUnrelatedProcessSettings(t *testing.T) {
	manager := &Manager{opts: ManagerOptions{Environment: func(context.Context) (*LaunchEnvironment, error) {
		return &LaunchEnvironment{Values: map[string]string{"PATH": "/caller-controlled"}}, nil
	}}}
	if _, err := manager.launchCommand(t.Context(), "sess_environment", []string{"/bin/true"}, true); err == nil {
		t.Fatal("region callback expanded into arbitrary process environment")
	}
}

func TestEmptyShellAndStopClaudeUseInteractiveShellWithRegionEnvironment(t *testing.T) {
	if _, err := os.Stat("/bin/bash"); err != nil {
		if _, zshErr := os.Stat("/bin/zsh"); zshErr != nil {
			t.Skip("neither /bin/bash nor /bin/zsh installed for interactive shell check")
		}
	}
	t.Setenv("HOME", t.TempDir())
	m, _ := managerFixture(t, 4)
	regionCalls := 0
	m.operations.Lock()
	m.opts.Environment = func(context.Context) (*LaunchEnvironment, error) {
		regionCalls++
		return &LaunchEnvironment{
			Revision: "rev-jp",
			Values:   map[string]string{"TZ": "Asia/Tokyo"},
		}, nil
	}
	m.operations.Unlock()

	noClaude := false
	emptyShell, err := m.Create(t.Context(), &pb.CreateSessionRequest{
		ProjectId:       "proj_fixture",
		Name:            "empty-shell",
		RequestId:       "empty-shell-region",
		AutoStartClaude: &noClaude,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertSessionShellArrowEditAndTZ(t, m, emptyShell.Id, "EMPTY", "Asia/Tokyo")

	withClaude, err := m.Create(t.Context(), &pb.CreateSessionRequest{
		ProjectId: "proj_fixture",
		Name:      "claude-then-stop",
		RequestId: "claude-then-stop-region",
	})
	if err != nil {
		t.Fatal(err)
	}
	callsBeforeStop := regionCalls
	stopped, err := m.StopClaude(t.Context(), &pb.SessionActionRequest{SessionId: withClaude.Id})
	if err != nil || stopped.ClaudeStatus != "stopped" {
		t.Fatalf("StopClaude failed: %+v %v", stopped, err)
	}
	if regionCalls != callsBeforeStop {
		t.Fatalf("StopClaude unexpectedly queried live region worker: calls %d -> %d", callsBeforeStop, regionCalls)
	}
	assertSessionShellArrowEditAndTZ(t, m, withClaude.Id, "STOPPED", "Asia/Tokyo")
}

func assertSessionShellArrowEditAndTZ(t *testing.T, m *Manager, sessionID, tag, wantTZ string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	att, err := m.tmux.Attach(ctx, sessionID, 24, 80)
	if err != nil {
		t.Fatal(err)
	}
	defer att.Close()

	waitOutput := func(needle string) {
		t.Helper()
		done := make(chan []byte, 1)
		go func() {
			var acc []byte
			buf := make([]byte, 4096)
			for {
				n, readErr := att.Read(buf)
				acc = append(acc, buf[:n]...)
				if bytes.Contains(acc, []byte(needle)) || readErr != nil {
					done <- acc
					return
				}
			}
		}()
		select {
		case got := <-done:
			if !bytes.Contains(got, []byte(needle)) {
				t.Fatalf("missing %q in terminal output %q", needle, got)
			}
		case <-time.After(5 * time.Second):
			_ = att.Close()
			got := <-done
			t.Fatalf("timed out waiting for %q in %q", needle, got)
		}
	}

	if _, err := att.Write([]byte("printf 'READY_%s_%s\\n' '" + tag + "' 'OK'\r")); err != nil {
		t.Fatal(err)
	}
	waitOutput("READY_" + tag + "_OK")

	// Use Left arrow (\x1b[D) to insert 'b' between 'a' and 'c' while printing $TZ.
	if _, err := att.Write([]byte("SHIKI_EDIT=ac\x1b[Db; printf 'RESULT_%s:%s:%s\\n' '" + tag + "' \"$TZ\" \"$SHIKI_EDIT\"\r")); err != nil {
		t.Fatal(err)
	}
	waitOutput("RESULT_" + tag + ":" + wantTZ + ":abc")
}
