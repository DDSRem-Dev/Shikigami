package session

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/DDSRem-Dev/Shikigami/internal/server/storage"
	"github.com/DDSRem-Dev/Shikigami/internal/server/tmux"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	"golang.org/x/sys/unix"
)

func TestUnknownExitIsNotRestartedAndCanBecomeKnown(t *testing.T) {
	m, _ := managerFixture(t, 2)
	value, err := m.Create(t.Context(), &pb.CreateSessionRequest{ProjectId: "proj_fixture", Name: "observation", RequestId: "observation"})
	if err != nil {
		t.Fatal(err)
	}
	bridge := NewBridge(t.Context(), value.Id, func(context.Context, string, uint32, uint32) (Terminal, error) {
		r, w := io.Pipe()
		return &fakeTerminal{r: r, w: w}, nil
	}, TerminalOptions{})
	defer bridge.Close()
	sub, err := bridge.Open(t.Context(), &pb.TerminalOpen{SessionId: value.Id, Size: &pb.ResizeEvent{Rows: 24, Cols: 80}}, true)
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()
	nextEvent(t, sub, pb.SessionEvent_READY)
	nextEvent(t, sub, pb.SessionEvent_RESET)
	dir := t.TempDir()
	observation := filepath.Join(dir, "pane")
	script := filepath.Join(dir, "tmux-observation")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ncase \"$*\" in *list-panes*) cat '"+observation+"';; *) exit 99;; esac\n"), 0700); err != nil {
		t.Fatal(err)
	}
	fake, err := tmux.New(script)
	if err != nil {
		t.Fatal(err)
	}
	m.operations.Lock()
	original, opts := m.tmux, m.opts
	m.tmux = fake
	m.opts.AutoRestart = RestartPolicy{Enabled: true, MaxAttempts: 1, Backoff: time.Minute, StableWindow: time.Minute}
	m.mu.Lock()
	m.bridges[value.Id] = bridge
	m.mu.Unlock()
	defer func() {
		m.tmux = original
		m.opts = opts
		_ = m.store.Update(func(s *storage.Snapshot) error { return s.Put(storage.SessionsBucket, value.Id, value) })
		m.operations.Unlock()
	}()
	write := func(status, signal string) {
		t.Helper()
		data := fmt.Sprintf("shiki-%s|%s|%s|123|1|%s|$1|%s\n", value.Id, value.TmuxWindow, value.TmuxPane, status, signal)
		if err := os.WriteFile(observation, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("", "")
	for range 3 {
		if err := m.restartStep(t.Context(), map[string]stableProcess{}); err != nil {
			t.Fatal(err)
		}
	}
	unknown := nextEvent(t, sub, pb.SessionEvent_CLAUDE_STOPPED)
	if unknown.GetEvent().Data["reason_code"] != "EXIT_REASON_UNKNOWN" || unknown.GetEvent().Data["exit_code"] != "" {
		t.Fatal(unknown)
	}
	state, err := m.store.View()
	if err != nil {
		t.Fatal(err)
	}
	var record RestartRecord
	if err := state.Get(restartBucket, value.Id, &record); err != nil {
		t.Fatal(err)
	}
	if record.Attempts != 0 || record.NextAttempt != 0 || record.LastExit != nil || record.ReasonCode != "EXIT_REASON_UNKNOWN" {
		t.Fatalf("unknown exit was eligible for restart: %+v", record)
	}
	write("7", "")
	if err := m.restartStep(t.Context(), map[string]stableProcess{}); err != nil {
		t.Fatal(err)
	}
	known := nextEvent(t, sub, pb.SessionEvent_CLAUDE_CRASHED)
	if known.GetEvent().Data["exit_code"] != "7" || known.Sequence != unknown.Sequence+1 {
		t.Fatalf("unknown observation was duplicated or never upgraded: unknown=%v known=%v", unknown, known)
	}
	// Reuse the same decode target as the background poller: the persisted
	// unknown record omits cleared values, which must not survive this read.
	state, err = m.store.View()
	if err != nil {
		t.Fatal(err)
	}
	if err := state.Get(restartBucket, value.Id, &record); err != nil {
		t.Fatal(err)
	}
	if record.NextAttempt == 0 || record.LastExit == nil || *record.LastExit != 7 {
		t.Fatalf("known exit was not scheduled: %+v", record)
	}
	write("", "")
	if err := m.restartStep(t.Context(), map[string]stableProcess{}); err != nil {
		t.Fatal(err)
	}
	state, err = m.store.View()
	if err != nil {
		t.Fatal(err)
	}
	if err := state.Get(restartBucket, value.Id, &record); err != nil {
		t.Fatal(err)
	}
	if record.NextAttempt != 0 || record.LastExit != nil || record.LastSignal != 0 || record.ReasonCode != "EXIT_REASON_UNKNOWN" {
		t.Fatalf("unknown exit retained a scheduled restart or stale cause: %+v raw=%s", record, state.Buckets[restartBucket][value.Id])
	}
	quiet, stop := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer stop()
	if extra, err := sub.Next(quiet); err == nil {
		t.Fatalf("known-to-unknown repeated an ended-process event: %v", extra)
	}
}

func TestSignalRestartBudgetAndExplicitStop(t *testing.T) {
	for _, signal := range []struct {
		name   string
		number int
	}{{"TERM", 15}, {"KILL", 9}} {
		t.Run(signal.name, func(t *testing.T) {
			dir := t.TempDir()
			marker, fifo := filepath.Join(dir, "launches"), filepath.Join(dir, "exit-trigger")
			if err := unix.Mkfifo(fifo, 0600); err != nil {
				t.Fatal(err)
			}
			m := restartFixture(t, []string{"/bin/sh", "-c", `printf x >> "$1"; read trigger < "$2"; kill -` + signal.name + ` $$`, "shiki", marker, fifo}, RestartPolicy{Enabled: true, MaxAttempts: 2, Backoff: 20 * time.Millisecond, StableWindow: time.Minute})
			value, err := m.Create(t.Context(), &pb.CreateSessionRequest{ProjectId: "proj_fixture", Name: "signal", RequestId: "signal"})
			if err != nil {
				t.Fatal(err)
			}
			unknownOutcome := false
			var observedRecord RestartRecord
		attempts:
			for attempt := 0; attempt < 3; attempt++ {
				probe, cancel := context.WithTimeout(t.Context(), 8*time.Second)
				for {
					contents, readErr := os.ReadFile(marker)
					if readErr == nil && len(contents) == attempt+1 {
						break
					}
					state, viewErr := m.store.View()
					if viewErr == nil && state.Get(restartBucket, value.Id, &observedRecord) == nil && observedRecord.ReasonCode == "EXIT_REASON_UNKNOWN" {
						unknownOutcome = true
						cancel()
						break attempts
					}

					select {
					case <-probe.Done():
						cancel()
						t.Fatalf("signal restart %d was not launched: %q, %v", attempt, contents, readErr)
					case <-time.After(10 * time.Millisecond):
					}
				}
				for {
					fd, openErr := unix.Open(fifo, unix.O_WRONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
					if openErr == nil {
						_, writeErr := unix.Write(fd, []byte("exit\n"))
						closeErr := unix.Close(fd)
						if writeErr != nil || closeErr != nil {
							cancel()
							t.Fatal(writeErr, closeErr)
						}
						break
					}
					if !errors.Is(openErr, unix.ENXIO) {
						cancel()
						t.Fatal(openErr)
					}
					select {
					case <-probe.Done():
						cancel()
						t.Fatal("signal fixture did not reach its exit gate")
					case <-time.After(10 * time.Millisecond):
					}
				}
				cancel()
			}
			record := observedRecord
			if !unknownOutcome {
				record = awaitRestart(t, m, value.Id, func(r RestartRecord) bool {
					return r.ReasonCode == "RESTART_LIMIT_REACHED" || r.ReasonCode == "EXIT_REASON_UNKNOWN"
				})
			}
			unknownOutcome = record.ReasonCode == "EXIT_REASON_UNKNOWN"
			if unknownOutcome {
				pane, err := m.tmux.Inspect(t.Context(), value.Id)
				if err != nil || !pane.Dead || pane.AbnormalExit() || record.NextAttempt != 0 || record.LastExit != nil || record.LastSignal != 0 || record.Attempts > 2 {
					t.Fatalf("unknown outcome not safely paused: %+v %+v %v", record, pane, err)
				}
				t.Logf("signal outcomes: known=%d unknown=1; unknown paused retries", record.Attempts)
			} else {
				if record.Attempts != 2 || record.LastSignal != signal.number || record.LastExit != nil {
					t.Fatalf("signal restart outcome: %+v", record)
				}
				t.Log("signal outcomes: known=3 unknown=0; persisted budget exhausted")
			}
			contents, err := os.ReadFile(marker)
			if err != nil || len(contents) != record.Attempts+1 || len(contents) > 3 {
				t.Fatalf("unbounded signal restart: %q %v", contents, err)
			}
			beforeStop := string(contents)
			if err := m.Close(); err != nil {
				t.Fatal(err)
			}
			next, err := NewManager(t.Context(), m.store, m.tmux, m.opts)
			if err != nil {
				t.Fatal(err)
			}
			defer next.Close()
			m = next
			m.operations.Lock()
			err = m.restartStep(t.Context(), map[string]stableProcess{})
			m.operations.Unlock()
			if err != nil {
				t.Fatal(err)
			}
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
			contents, err = os.ReadFile(marker)
			if err != nil || string(contents) != beforeStop {
				t.Fatal("explicit Stop failed to suppress signal restart", err)
			}
		})
	}
}
