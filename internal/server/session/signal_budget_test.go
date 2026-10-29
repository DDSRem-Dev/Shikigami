package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DDSRem-Dev/Shikigami/internal/server/tmux"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
)

// Deterministic observation fixture complements the real tmux signal tests.
// It supplies an explicitly known signal; it never converts a missing real
// tmux exit reason into a known outcome.
func TestKnownSignalBudgetPersistsAndStopSuppresses(t *testing.T) {
	base, _ := managerFixture(t, 1)
	value, err := base.Create(t.Context(), &pb.CreateSessionRequest{ProjectId: "proj_fixture", Name: "budget", RequestId: "budget"})
	if err != nil {
		t.Fatal(err)
	}
	if err := base.Close(); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	observation := filepath.Join(dir, "observation")
	launches := filepath.Join(dir, "automatic")
	binary := filepath.Join(dir, "tmux-observer")
	t.Setenv("SHIKI_TEST_EXIT_OBSERVATION", observation)
	t.Setenv("SHIKI_TEST_AUTOMATIC_LAUNCHES", launches)
	data := "shiki-" + value.Id + "|" + value.TmuxWindow + "|" + value.TmuxPane + "|123|1||$1|term\n"
	if err := os.WriteFile(observation, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
action=
explicit=
for arg do
 case "$arg" in
  list-panes) cat "$SHIKI_TEST_EXIT_OBSERVATION"; exit 0;;
  respawn-pane) action=respawn;;
  -k) explicit=yes;;
 esac
done
if [ "$action" != respawn ]; then exit 99; fi
if [ "$explicit" != yes ]; then printf x >> "$SHIKI_TEST_AUTOMATIC_LAUNCHES"; fi
`
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	fake, err := tmux.New(binary)
	if err != nil {
		t.Fatal(err)
	}
	opts := base.opts
	opts.AutoRestart = RestartPolicy{Enabled: true, MaxAttempts: 2, Backoff: time.Millisecond, StableWindow: time.Minute}
	m, err := NewManager(t.Context(), base.store, fake, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = m.Close() }()
	record := awaitRestart(t, m, value.Id, func(r RestartRecord) bool { return r.ReasonCode == "RESTART_LIMIT_REACHED" })
	if record.Attempts != 2 || record.LastSignal != 15 || record.LastExit != nil {
		t.Fatal(record)
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	m, err = NewManager(t.Context(), base.store, fake, opts)
	if err != nil {
		t.Fatal(err)
	}
	m.operations.Lock()
	for range 3 {
		if err := m.restartStep(t.Context(), map[string]stableProcess{}); err != nil {
			m.operations.Unlock()
			t.Fatal(err)
		}
	}
	m.operations.Unlock()
	if _, err := m.StopClaude(t.Context(), &pb.SessionActionRequest{SessionId: value.Id}); err != nil {
		t.Fatal(err)
	}
	m.operations.Lock()
	for range 3 {
		if err := m.restartStep(t.Context(), map[string]stableProcess{}); err != nil {
			m.operations.Unlock()
			t.Fatal(err)
		}
	}
	m.operations.Unlock()
	state, err := m.store.View()
	if err != nil {
		t.Fatal(err)
	}
	if err := state.Get(restartBucket, value.Id, &record); err != nil {
		t.Fatal(err)
	}
	if !record.Suppressed || record.ReasonCode != "USER_STOP" {
		t.Fatal(record)
	}
	count, err := os.ReadFile(launches)
	if err != nil || string(count) != strings.Repeat("x", 2) {
		t.Fatalf("known-signal budget was replenished or stop ignored: %q %v", count, err)
	}
}
