package session

import (
	"context"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/DDSRem-Dev/Shikigami/internal/server/storage"
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
