package session

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/DDSRem-Dev/Shikigami/internal/server/storage"
	"github.com/DDSRem-Dev/Shikigami/internal/server/tmux"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	"golang.org/x/sys/unix"
)

func restartFixture(t *testing.T, command []string, policy RestartPolicy) *Manager {
	t.Helper()
	base, _ := managerFixture(t, 4)
	if err := base.Close(); err != nil {
		t.Fatal(err)
	}
	opts := base.opts
	opts.ClaudeCommand = command
	opts.AutoRestart = policy
	m, err := NewManager(t.Context(), base.store, base.tmux, opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Close() })
	return m
}
func awaitRestart(t *testing.T, m *Manager, id string, match func(RestartRecord) bool) RestartRecord {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		state, err := m.store.View()
		if err != nil {
			t.Fatal(err)
		}
		var value RestartRecord
		if err := state.Get(restartBucket, id, &value); err == nil && match(value) {
			return value
		}
		select {
		case <-ctx.Done():
			probe, stop := context.WithTimeout(context.Background(), time.Second)
			raw, probeErr := exec.CommandContext(probe, "tmux", "-L", fmt.Sprintf("shiki-%d", os.Getuid()), "list-panes", "-t", "shiki-"+id, "-F", "#{pane_id}|#{pane_pid}|#{pane_dead}|#{pane_dead_status}|#{pane_dead_signal}").CombinedOutput()
			stop()
			t.Fatalf("restart record did not converge: %+v; actual tmux pane=%q, query=%v", value, raw, probeErr)
		case <-ticker.C:
		}
	}
}

func TestRealTmuxRestartBudgetOrUnknownPauseSurvivesManagerRestart(t *testing.T) {
	dir := t.TempDir()
	marker, fifo := filepath.Join(dir, "attempts"), filepath.Join(dir, "exit-gate")
	if err := unix.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	m := restartFixture(t, []string{"/bin/sh", "-c", `printf x >> "$1"; read trigger < "$2"; exit 7`, "shiki", marker, fifo}, RestartPolicy{Enabled: true, MaxAttempts: 2, Backoff: 20 * time.Millisecond, StableWindow: time.Minute})
	value, err := m.Create(t.Context(), &pb.CreateSessionRequest{ProjectId: "proj_fixture", Name: "crashing", RequestId: "crashing"})
	if err != nil {
		t.Fatal(err)
	}
	// Each real child waits until tmux has exposed its live pane. This tests
	// retry budgeting without assuming tmux always reports an immediate exit;
	// absent exit reasons instead require a strict, durable unknown pause.
	// The separate deterministic known-exit test always proves the full budget.
	previousPID := 0
	var record RestartRecord
	known, unknown := 0, 0
	for attempt := 1; attempt <= 3; attempt++ {
		deadline := time.Now().Add(8 * time.Second)
		released := false
		for time.Now().Before(deadline) {
			content, readErr := os.ReadFile(marker)
			pane, inspectErr := m.tmux.Inspect(t.Context(), value.Id)
			if readErr == nil && len(content) == attempt && inspectErr == nil && !pane.Dead && pane.PID > 0 && pane.PID != previousPID {
				fd, openErr := unix.Open(fifo, unix.O_WRONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
				if openErr == nil {
					_, writeErr := unix.Write(fd, []byte("exit\n"))
					closeErr := unix.Close(fd)
					if writeErr != nil || closeErr != nil {
						t.Fatal(writeErr, closeErr)
					}
					previousPID = pane.PID
					released = true
					break
				}
				if !errors.Is(openErr, unix.ENXIO) {
					t.Fatal(openErr)
				}
			}
			select {
			case <-t.Context().Done():
				t.Fatal(t.Context().Err())
			case <-time.After(10 * time.Millisecond):
			}
		}
		if !released {
			state, _ := m.store.View()
			var r RestartRecord
			_ = state.Get(restartBucket, value.Id, &r)
			pane, err := m.tmux.Inspect(t.Context(), value.Id)
			t.Fatalf("exit gate %d not reached; record=%+v pane=%+v err=%v", attempt, r, pane, err)
		}
		record = awaitRestart(t, m, value.Id, func(r RestartRecord) bool {
			return r.ReasonCode == "EXIT_REASON_UNKNOWN" || r.ReasonCode == "RESTART_LIMIT_REACHED" || r.Attempts >= attempt
		})
		if record.ReasonCode == "EXIT_REASON_UNKNOWN" {
			pane, err := m.tmux.Inspect(t.Context(), value.Id)
			if err != nil || !pane.Dead || pane.AbnormalExit() || record.LastExit != nil || record.LastSignal != 0 || record.NextAttempt != 0 || record.Attempts != attempt-1 {
				t.Fatalf("unsafe unknown outcome: %+v %+v %v", record, pane, err)
			}
			unknown++
			break
		}
		if record.LastExit == nil || *record.LastExit != 7 || record.LastSignal != 0 || record.Attempts != min(attempt, 2) {
			t.Fatalf("known exit violated budget: %+v", record)
		}
		known++
	}
	if unknown == 0 && (known != 3 || record.Attempts != 2 || record.ReasonCode != "RESTART_LIMIT_REACHED") {
		t.Fatalf("known outcomes did not exhaust budget: %+v", record)
	}
	t.Logf("real exit outcomes: known=%d unknown=%d; unknown is pause evidence, not known budget acceptance", known, unknown)
	bytes, err := os.ReadFile(marker)
	if err != nil || len(bytes) != record.Attempts+1 || len(bytes) > 3 {
		t.Fatalf("wrong launch count: %q %v", bytes, err)
	}
	beforeLaunches := string(bytes)
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	next, err := NewManager(t.Context(), m.store, m.tmux, m.opts)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	// Drive the restarted controller directly as well as its background loop;
	// the persisted exhausted budget must prevent any external action.
	next.operations.Lock()
	for range 3 {
		if err = next.restartStep(t.Context(), map[string]stableProcess{}); err != nil {
			break
		}
	}
	next.operations.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	bytes, err = os.ReadFile(marker)
	if err != nil || string(bytes) != beforeLaunches {
		t.Fatalf("manager restart replenished budget: %q %v", bytes, err)
	}
	state, _ := next.store.View()
	var after RestartRecord
	if err := state.Get(restartBucket, value.Id, &after); err != nil {
		t.Fatal(err)
	}
	if after.Attempts != record.Attempts || after.NextAttempt != 0 || (unknown > 0 && (after.LastExit != nil || after.LastSignal != 0 || after.ReasonCode != "EXIT_REASON_UNKNOWN")) {
		t.Fatalf("restart lost safe outcome: %+v", after)
	}
}

func TestRestartStableWindowQuietProcessAndExplicitStop(t *testing.T) {
	m := restartFixture(t, []string{"/bin/sleep", "30"}, RestartPolicy{Enabled: true, MaxAttempts: 2, Backoff: 20 * time.Millisecond, StableWindow: 300 * time.Millisecond})
	value, err := m.Create(t.Context(), &pb.CreateSessionRequest{ProjectId: "proj_fixture", Name: "quiet", RequestId: "quiet"})
	if err != nil {
		t.Fatal(err)
	}
	initial, err := m.tmux.Inspect(t.Context(), value.Id)
	if err != nil || initial.Dead || initial.PID <= 0 {
		t.Fatalf("quiet process not running: %+v %v", initial, err)
	}
	// Stable-window acceptance starts with an explicit existing retry budget,
	// not an assumed exit cause from a short-lived real child. tmux may retain
	// no exit reason, in which case production must pause rather than restart.
	// Known/unknown exit handling and actual retries have separate tests.
	func() {
		m.operations.Lock()
		defer m.operations.Unlock()
		if err := m.store.Update(func(state *storage.Snapshot) error {
			return state.Put(restartBucket, value.Id, RestartRecord{Attempts: 1, ReasonCode: "RESTART_ATTEMPT"})
		}); err != nil {
			t.Fatal(err)
		}
		stable := map[string]stableProcess{}
		step := func(wantAttempts int, wantReason string) {
			t.Helper()
			if err := m.restartStep(t.Context(), stable); err != nil {
				t.Fatal(err)
			}
			state, err := m.store.View()
			if err != nil {
				t.Fatal(err)
			}
			var record RestartRecord
			if err := state.Get(restartBucket, value.Id, &record); err != nil {
				t.Fatal(err)
			}
			if record.Attempts != wantAttempts || record.ReasonCode != wantReason || record.NextAttempt != 0 || record.LastExit != nil || record.LastSignal != 0 || record.Suppressed {
				t.Fatalf("stable window changed retry state incorrectly: %+v", record)
			}
		}
		step(1, "RESTART_ATTEMPT") // A first live observation cannot reset the budget.
		timer := time.NewTimer(m.opts.AutoRestart.StableWindow)
		defer timer.Stop()
		select {
		case <-t.Context().Done():
			t.Fatal(t.Context().Err())
		case <-timer.C:
		}
		step(0, "STABLE_WINDOW_REACHED")
	}()
	pane, err := m.tmux.Inspect(t.Context(), value.Id)
	if err != nil || pane.Dead || pane.PID != initial.PID || pane.PaneID != initial.PaneID {
		t.Fatalf("quiet process incorrectly killed: %v %v", pane, err)
	}
	if err := m.tmux.RespawnDead(t.Context(), value.Id, pane.PaneID, []string{"/bin/false"}); err == nil {
		t.Fatal("automatic tmux action replaced a live process")
	}
	after, err := m.tmux.Inspect(t.Context(), value.Id)
	if err != nil || after.PID != pane.PID {
		t.Fatal("live process changed", err)
	}
	if _, err := m.StopClaude(t.Context(), &pb.SessionActionRequest{SessionId: value.Id}); err != nil {
		t.Fatal(err)
	}
	awaitRestart(t, m, value.Id, func(r RestartRecord) bool { return r.Suppressed && r.ReasonCode == "USER_STOP" })
	if err := m.tmux.Respawn(t.Context(), value.Id, []string{"/bin/sh", "-c", "exit 12"}); err != nil {
		t.Fatal(err)
	}
	m.operations.Lock()
	err = m.restartStep(t.Context(), map[string]stableProcess{})
	m.operations.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	record := awaitRestart(t, m, value.Id, func(r RestartRecord) bool { return r.Suppressed })
	if record.Attempts != 0 {
		t.Fatal("explicit stop did not suppress retry", record)
	}
}

func TestUnknownExitIsNotACrashAndBackoffIsBounded(t *testing.T) {
	value := &pb.Session{ClaudeStatus: "running"}
	applyPane(value, tmux.Pane{Dead: true})
	if value.ClaudeStatus != "unknown" {
		t.Fatal(value.ClaudeStatus)
	}
	if restartDelay(time.Second, 1) != 2*time.Second || restartDelay(time.Second, 99) != 30*time.Second {
		t.Fatal("invalid retry delay")
	}
}
