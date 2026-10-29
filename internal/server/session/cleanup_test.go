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
)

func stoppedCleanupSession(t *testing.T, m *Manager) *pb.Session {
	t.Helper()
	s, err := m.Create(t.Context(), &pb.CreateSessionRequest{ProjectId: "proj_fixture", Name: "cleanup", RequestId: "cleanup-create"})
	if err != nil {
		t.Fatal(err)
	}
	s, err = m.StopClaude(t.Context(), &pb.SessionActionRequest{SessionId: s.Id})
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func exitCleanupShell(t *testing.T, m *Manager, id string) {
	t.Helper()
	p, err := m.tmux.Inspect(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("tmux", "-L", fmt.Sprintf("shiki-%d", os.Getuid()), "send-keys", "-t", p.PaneID, "exit", "Enter").CombinedOutput()
	if err != nil {
		t.Fatalf("exit stopped shell: %v %s", err, out)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		p, err = m.tmux.Inspect(t.Context(), id)
		if err == nil && p.Dead {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("shell did not become dead")
}
func readCleanup(t *testing.T, m *Manager, id string) CleanupRecord {
	t.Helper()
	s, err := m.store.View()
	if err != nil {
		t.Fatal(err)
	}
	r, err := m.cleanupRecord(s, id)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func cleanupAt(t *testing.T, m *Manager, at time.Time) {
	t.Helper()
	if err := m.cleanupStep(t.Context(), at); err != nil {
		t.Fatal(err)
	}
}

func TestAutomaticCleanupStopShellWarningRestartAndFiles(t *testing.T) {
	m, root := managerFixture(t, 2)
	s := stoppedCleanupSession(t, m)
	marker := filepath.Join(root, "keep.txt")
	if err := os.WriteFile(marker, []byte("project survives"), 0600); err != nil {
		t.Fatal(err)
	}
	m.operations.Lock()
	defer m.operations.Unlock()
	m.opts.AutoCleanup = CleanupPolicy{Enabled: true, IdleThreshold: 10 * time.Second, WarnBefore: 3 * time.Second}
	now := time.Now().Add(-20 * time.Second)
	cleanupAt(t, m, now)
	if r := readCleanup(t, m, s.Id); r.Since != 0 {
		t.Fatal("live shell entered automatic cleanup")
	}
	got, err := m.Get(t.Context(), &pb.GetSessionRequest{Id: s.Id})
	if err != nil || got.CleanupStatus != "live_pane" {
		t.Fatalf("live-shell diagnostic: %v %v", got, err)
	}
	exitCleanupShell(t, m, s.Id)
	cleanupAt(t, m, now)
	cleanupAt(t, m, now.Add(7*time.Second))
	warned := readCleanup(t, m, s.Id)
	if warned.Warning == 0 || warned.Due-warned.Warning < 3000 {
		t.Fatal("missing full warning window", warned)
	}
	// Model a new daemon observation epoch on the same durable store. Offline
	// elapsed time is not credited; the existing warning is retained and extended.
	m.cleanupSeen = map[string]bool{}
	cleanupAt(t, m, now.Add(20*time.Second))
	restarted := readCleanup(t, m, s.Id)
	got, err = m.Get(t.Context(), &pb.GetSessionRequest{Id: s.Id})
	if err != nil || got.CleanupStatus != "warned" || got.GetCleanupAfter() != restarted.Due/1000 {
		t.Fatalf("restarted warning not visible: %v %v", got, err)
	}
	if restarted.Warning != warned.Warning || restarted.Due < now.Add(30*time.Second).UnixMilli() {
		t.Fatal("restart shortened/forgot warning", restarted)
	}
	cleanupAt(t, m, now.Add(29*time.Second))
	if _, err := m.tmux.Inspect(t.Context(), s.Id); err != nil {
		t.Fatal("deleted before extended due", err)
	}
	cleanupAt(t, m, now.Add(30*time.Second))
	if _, err := m.tmux.Inspect(t.Context(), s.Id); !errors.Is(err, tmux.ErrNotFound) {
		t.Fatal("dead pane not deleted", err)
	}
	state, err := m.store.View()
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := state.Buckets[storage.SessionsBucket][s.Id]; exists {
		t.Fatal("session config retained after cleanup")
	}
	content, err := os.ReadFile(marker)
	if err != nil || string(content) != "project survives" {
		t.Fatal("project files changed", err)
	}
}

func TestAutomaticCleanupNeverKillsRespawnedTask(t *testing.T) {
	m, _ := managerFixture(t, 2)
	s := stoppedCleanupSession(t, m)
	m.operations.Lock()
	defer m.operations.Unlock()
	m.opts.AutoCleanup = CleanupPolicy{Enabled: true, IdleThreshold: 10 * time.Second, WarnBefore: 3 * time.Second}
	exitCleanupShell(t, m, s.Id)
	now := time.Now()
	cleanupAt(t, m, now)
	cleanupAt(t, m, now.Add(7*time.Second))
	// The exact tmux pane can be externally respawned after the last observation.
	if err := m.tmux.Respawn(t.Context(), s.Id, []string{"/bin/sleep", "30"}); err != nil {
		t.Fatal(err)
	}
	cleanupAt(t, m, now.Add(11*time.Second))
	pane, err := m.tmux.Inspect(t.Context(), s.Id)
	if err != nil || pane.Dead {
		t.Fatal("automatic cleanup killed new task", err)
	}
	if readCleanup(t, m, s.Id).Warning != 0 {
		t.Fatal("live task retained obsolete warning")
	}
	// A previously persisted deletion intent must also refuse the live pane.
	r := CleanupRecord{Since: now.UnixMilli(), Warning: now.Add(7 * time.Second).UnixMilli(), Due: now.Add(10 * time.Second).UnixMilli(), Idle: 10000, Warn: 3000, Session: pane.TmuxSessionID, Window: pane.WindowID, Pane: pane.PaneID, Deleting: true}
	if err := m.finishAutomaticCleanup(t.Context(), s.Id, r); err != nil {
		t.Fatal(err)
	}
	pane, err = m.tmux.Inspect(t.Context(), s.Id)
	if err != nil || pane.Dead {
		t.Fatal("recovery killed live task", err)
	}
	state, _ := m.store.View()
	if _, ok := state.Buckets[storage.SessionsBucket][s.Id]; !ok {
		t.Fatal("guard refusal deleted session record")
	}
}

func TestActivityCoalescingAndCleanupRollback(t *testing.T) {
	m, _ := managerFixture(t, 2)
	s := stoppedCleanupSession(t, m)
	m.operations.Lock()
	defer m.operations.Unlock()
	before, _ := m.store.View()
	for range 1000 {
		m.recordActivity(s.Id)
	}
	after, _ := m.store.View()
	if after.Revision != before.Revision {
		t.Fatal("input wrote disk eagerly")
	}
	now := time.Now()
	if err := m.flushActivity(now, true); err != nil {
		t.Fatal(err)
	}
	after, _ = m.store.View()
	if after.Revision != before.Revision+1 {
		t.Fatal("activity not coalesced")
	}
	m.opts.AutoCleanup = CleanupPolicy{Enabled: true, IdleThreshold: 10 * time.Second, WarnBefore: 3 * time.Second}
	exitCleanupShell(t, m, s.Id)
	cleanupAt(t, m, now)
	cleanupAt(t, m, now.Add(7*time.Second))
	cleanupAt(t, m, now.Add(6*time.Second))
	if r := readCleanup(t, m, s.Id); r.Warning != 0 {
		t.Fatal("clock rollback retained deletion deadline")
	}
}

func TestAutomaticCleanupDefaultDisabled(t *testing.T) {
	m, _ := managerFixture(t, 2)
	s := stoppedCleanupSession(t, m)
	m.operations.Lock()
	defer m.operations.Unlock()
	exitCleanupShell(t, m, s.Id)
	cleanupAt(t, m, time.Now().Add(365*24*time.Hour))
	if _, err := m.tmux.Inspect(context.Background(), s.Id); err != nil {
		t.Fatal(err)
	}
	if readCleanup(t, m, s.Id).Since != 0 {
		t.Fatal("default cleanup enabled")
	}
}

func TestActivityBetweenCleanupAndFlushCancelsDurableSchedule(t *testing.T) {
	m, _ := managerFixture(t, 2)
	s := stoppedCleanupSession(t, m)
	m.operations.Lock()
	defer m.operations.Unlock()
	m.opts.AutoCleanup = CleanupPolicy{Enabled: true, IdleThreshold: 10 * time.Second, WarnBefore: 3 * time.Second}
	exitCleanupShell(t, m, s.Id)
	now := time.Now()
	cleanupAt(t, m, now)
	m.recordActivity(s.Id)
	if err := m.flushActivity(now, true); err != nil {
		t.Fatal(err)
	}
	if r := readCleanup(t, m, s.Id); r.Since != 0 {
		t.Fatal("flushed activity disappeared before cleanup could cancel intent")
	}
	cleanupAt(t, m, now.Add(11*time.Second))
	if r := readCleanup(t, m, s.Id); r.Warning != 0 {
		t.Fatal("activity did not restart idle observation")
	}
}

func TestCleanupEpochRebaseWriteFailureCannotSkipDelay(t *testing.T) {
	m, _ := managerFixture(t, 2)
	s := stoppedCleanupSession(t, m)
	m.operations.Lock()
	defer m.operations.Unlock()
	m.opts.AutoCleanup = CleanupPolicy{Enabled: true, IdleThreshold: 10 * time.Second, WarnBefore: 3 * time.Second}
	exitCleanupShell(t, m, s.Id)
	now := time.Now()
	cleanupAt(t, m, now)
	cleanupAt(t, m, now.Add(7*time.Second))
	original := m.store
	snapshot, err := original.View()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "copy-state")
	copyStore, err := storage.Open(dir, storage.Options{Validate: storage.ValidateDaemon("tester")})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { m.store = original; copyStore.Close() }()
	if err := copyStore.Update(func(s *storage.Snapshot) error { *s = *snapshot; return nil }); err != nil {
		t.Fatal(err)
	}
	m.store = copyStore
	m.cleanupSeen = map[string]bool{}
	backup := filepath.Join(dir, "state.previous.json")
	if err := os.Remove(backup); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(backup, 0700); err != nil {
		t.Fatal(err)
	}
	if err := m.cleanupStep(t.Context(), now.Add(20*time.Second)); err == nil {
		t.Fatal("injected backup rename failure not exercised")
	}
	if m.cleanupSeen[s.Id] {
		t.Fatal("failed rebase published a successful runtime epoch")
	}
	if err := os.Remove(backup); err != nil {
		t.Fatal(err)
	}
	cleanupAt(t, m, now.Add(20*time.Second))
	if r := readCleanup(t, m, s.Id); r.Due < now.Add(30*time.Second).UnixMilli() {
		t.Fatal("retry skipped full new observation window", r)
	}
	if _, err := m.tmux.Inspect(t.Context(), s.Id); err != nil {
		t.Fatal("expired old schedule deleted session", err)
	}
}

func TestCleanupAndAttachmentAreSerializedAtBridge(t *testing.T) {
	for _, openFirst := range []bool{true, false} {
		t.Run(fmt.Sprint(openFirst), func(t *testing.T) {
			m, _ := managerFixture(t, 2)
			s := stoppedCleanupSession(t, m)
			bridge, err := m.Terminal(s.Id)
			if err != nil {
				t.Fatal(err)
			}
			m.operations.Lock()
			defer m.operations.Unlock()
			m.opts.AutoCleanup = CleanupPolicy{Enabled: true, IdleThreshold: 10 * time.Second, WarnBefore: 3 * time.Second}
			exitCleanupShell(t, m, s.Id)
			now := time.Now()
			cleanupAt(t, m, now)
			cleanupAt(t, m, now.Add(7*time.Second))
			open := func() (*Subscription, error) {
				return bridge.Open(t.Context(), &pb.TerminalOpen{SessionId: s.Id, Size: &pb.ResizeEvent{Rows: 24, Cols: 80}}, true)
			}
			if openFirst {
				sub, err := open()
				if err != nil {
					t.Fatal(err)
				}
				defer sub.Close()
				cleanupAt(t, m, now.Add(11*time.Second))
				if _, err := m.tmux.Inspect(t.Context(), s.Id); err != nil {
					t.Fatal("attached pane deleted", err)
				}
				if r := readCleanup(t, m, s.Id); r.Since != 0 {
					t.Fatal("attached pane retained schedule")
				}
			} else {
				cleanupAt(t, m, now.Add(11*time.Second))
				sub, err := open()
				if err == nil {
					sub.Close()
					t.Fatal("previously obtained bridge reopened a deleted session")
				}
			}
		})
	}
}

func TestCleanupRealManagerAndStoreReopenPreservesAndExtendsWarning(t *testing.T) {
	m, _ := managerFixture(t, 2)
	s := stoppedCleanupSession(t, m)
	m.operations.Lock()
	m.opts.AutoCleanup = CleanupPolicy{Enabled: true, IdleThreshold: 10 * time.Second, WarnBefore: 3 * time.Second}
	exitCleanupShell(t, m, s.Id)
	now := time.Now().Add(-20 * time.Second)
	cleanupAt(t, m, now)
	cleanupAt(t, m, now.Add(7*time.Second))
	before := readCleanup(t, m, s.Id)
	snapshot, err := m.store.View()
	if err != nil {
		t.Fatal(err)
	}
	m.operations.Unlock()
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	// Reopen a byte-serialized durable snapshot through the production store,
	// preserving the actual tmux pane while replacing the Manager instance.
	dir := filepath.Join(t.TempDir(), "reopened")
	disk, err := storage.Open(dir, storage.Options{Validate: storage.ValidateDaemon("tester")})
	if err != nil {
		t.Fatal(err)
	}
	if err := disk.Update(func(s *storage.Snapshot) error { *s = *snapshot; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := disk.Close(); err != nil {
		t.Fatal(err)
	}
	disk, err = storage.Open(dir, storage.Options{Validate: storage.ValidateDaemon("tester")})
	if err != nil {
		t.Fatal(err)
	}
	defer disk.Close()
	recovered, err := NewManager(t.Context(), disk, m.tmux, m.opts)
	if err != nil {
		t.Fatal(err)
	}
	defer recovered.Close()
	recovered.operations.Lock()
	defer recovered.operations.Unlock()
	if _, err := recovered.recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	cleanupAt(t, recovered, now.Add(20*time.Second))
	after := readCleanup(t, recovered, s.Id)
	if after.Warning != before.Warning || after.Due < now.Add(30*time.Second).UnixMilli() {
		t.Fatal("durable reopen shortened or lost warning", before, after)
	}
	got, err := recovered.Get(t.Context(), &pb.GetSessionRequest{Id: s.Id})
	if err != nil || got.CleanupStatus != "warned" || got.GetCleanupAfter() != after.Due/1000 {
		t.Fatalf("reopen diagnostic: %v %v", got, err)
	}
	cleanupAt(t, recovered, now.Add(29*time.Second))
	if _, err := m.tmux.Inspect(t.Context(), s.Id); err != nil {
		t.Fatal("reopen removed pane early", err)
	}
	cleanupAt(t, recovered, now.Add(30*time.Second))
	if _, err := m.tmux.Inspect(t.Context(), s.Id); !errors.Is(err, tmux.ErrNotFound) {
		t.Fatal("reopened due cleanup failed", err)
	}
}

func TestUserActivityComesFromAttachAndAcceptedInputNotOutput(t *testing.T) {
	bridge, terminal := bridgeFixture(t, 32)
	count := 0
	bridge.activity = func() { count++ }
	controller := openTest(t, bridge, true)
	if count != 1 {
		t.Fatal("successful attach did not count")
	}
	if err := bridge.Input(controller.ID, &pb.TerminalInput{Payload: &pb.TerminalInput_Data{Data: []byte("hello")}}); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatal("accepted input did not count")
	}
	observer := openTest(t, bridge, false)
	if err := bridge.Input(observer.ID, &pb.TerminalInput{Payload: &pb.TerminalInput_Data{Data: []byte("forbidden")}}); err == nil {
		t.Fatal("observer input accepted")
	}
	if count != 3 {
		t.Fatal("denied input counted as activity")
	}
	if _, err := terminal().w.Write([]byte("background output")); err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatal("output counted as user activity")
	}
}

func TestGlobalObservationFailureCancelsCleanupAcrossDeadline(t *testing.T) {
	for _, writeFails := range []bool{false, true} {
		t.Run(fmt.Sprint(writeFails), func(t *testing.T) {
			m, _ := managerFixture(t, 2)
			s := stoppedCleanupSession(t, m)
			m.operations.Lock()
			defer m.operations.Unlock()
			m.opts.AutoCleanup = CleanupPolicy{Enabled: true, IdleThreshold: 10 * time.Second, WarnBefore: 3 * time.Second}
			exitCleanupShell(t, m, s.Id)
			now := time.Now().Add(-20 * time.Second)
			cleanupAt(t, m, now)
			cleanupAt(t, m, now.Add(7*time.Second))
			originalStore := m.store
			snapshot, err := m.store.View()
			if err != nil {
				t.Fatal(err)
			}
			dir := filepath.Join(t.TempDir(), "state")
			disk, err := storage.Open(dir, storage.Options{Validate: storage.ValidateDaemon("tester")})
			if err != nil {
				t.Fatal(err)
			}
			if err := disk.Update(func(s *storage.Snapshot) error { *s = *snapshot; return nil }); err != nil {
				t.Fatal(err)
			}
			m.store = disk
			defer func() { m.store = originalStore; disk.Close() }()
			backup := filepath.Join(dir, "state.previous.json")
			if writeFails {
				if err := os.Remove(backup); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(backup, 0700); err != nil {
					t.Fatal(err)
				}
			}
			script := filepath.Join(t.TempDir(), "failed-tmux")
			if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 71\n"), 0700); err != nil {
				t.Fatal(err)
			}
			failed, err := tmux.New(script)
			if err != nil {
				t.Fatal(err)
			}
			originalTmux := m.tmux
			m.tmux = failed
			defer func() { m.tmux = originalTmux }()
			if err := m.maintenanceStep(t.Context(), map[string]stableProcess{}, now.Add(20*time.Second)); err == nil {
				t.Fatal("global inventory failure not exercised")
			}
			m.tmux = originalTmux
			if writeFails {
				if !m.cleanupInvalidated.Load() {
					t.Fatal("failed cancellation did not remain latched")
				}
				view, err := m.Get(t.Context(), &pb.GetSessionRequest{Id: s.Id})
				if err != nil || view.CleanupStatus != "unknown" || view.CleanupAfter != nil {
					t.Fatalf("invalidated plan still advertised: %v %v", view, err)
				}
				if err := m.maintenanceStep(t.Context(), map[string]stableProcess{}, now.Add(21*time.Second)); err == nil {
					t.Fatal("restored observation bypassed failed persistent cancellation")
				}
				if err := os.Remove(backup); err != nil {
					t.Fatal(err)
				}
			} else if r := readCleanup(t, m, s.Id); r.Since != 0 {
				t.Fatal("failed observation retained old schedule")
			}
			if err := m.maintenanceStep(t.Context(), map[string]stableProcess{}, now.Add(22*time.Second)); err != nil {
				t.Fatal(err)
			}
			r := readCleanup(t, m, s.Id)
			if r.Since < now.Add(22*time.Second).UnixMilli() || r.Warning != 0 || m.cleanupInvalidated.Load() {
				t.Fatal("old deadline was reused", r)
			}
			if _, err := m.tmux.Inspect(t.Context(), s.Id); err != nil {
				t.Fatal("failed interval counted as continuous idle", err)
			}
		})
	}
}

func TestCleanupClockCheckUsesWallTimestamps(t *testing.T) {
	previous := time.Now()
	// Round(0) deliberately strips monotonic state; the comparison must obey
	// the same wall timestamp regardless of the hidden monotonic component.
	for _, before := range []time.Time{previous, previous.Round(0)} {
		for _, now := range []time.Time{previous.Add(-time.Second), previous.Add(-time.Second).Round(0)} {
			if !wallClockRegressed(now, before) {
				t.Fatal("wall timestamp regression ignored")
			}
		}
		if wallClockRegressed(previous.Add(time.Second), before) {
			t.Fatal("increasing wall timestamp rejected")
		}
	}
}
