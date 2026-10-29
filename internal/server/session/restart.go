package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/DDSRem-Dev/Shikigami/internal/server/storage"
	"github.com/DDSRem-Dev/Shikigami/internal/server/tmux"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
)

const restartBucket = "session.restart"

type RestartPolicy struct {
	Enabled               bool
	MaxAttempts           int
	Backoff, StableWindow time.Duration
}

func (p RestartPolicy) validate() error {
	if p.Enabled && (p.MaxAttempts < 1 || p.MaxAttempts > 100 || p.Backoff <= 0 || p.StableWindow <= 0) {
		return errors.New("automatic restart requires bounded attempts and positive durations")
	}
	return nil
}

// RestartRecord stores retry decisions, never process handles or PIDs. A daemon
// restart does not replenish a repeatedly crashing session's retry budget.
type RestartRecord struct {
	Attempts    int    `json:"attempts"`
	NextAttempt int64  `json:"next_attempt,omitempty"`
	Suppressed  bool   `json:"suppressed"`
	LastExit    *int   `json:"last_exit,omitempty"`
	LastSignal  int    `json:"last_signal,omitempty"`
	ReasonCode  string `json:"reason_code,omitempty"`
}

func (m *Manager) setRestartIntent(id string, restart bool) error {
	return m.store.Update(func(state *storage.Snapshot) error {
		delete(state.Buckets[cleanupBucket], id)
		record := RestartRecord{Suppressed: !restart}
		if restart {
			record.ReasonCode = "USER_RESTART"
		} else {
			record.ReasonCode = "USER_STOP"
		}
		return state.Put(restartBucket, id, &record)
	})
}

func (m *Manager) watchProcesses() {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	// Stable windows require continuously observed runtime identity. They reset
	// across daemon restarts and any pane/PID change; retry budgets remain durable.
	stable := map[string]stableProcess{}
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
		}
		ctx, cancel := context.WithTimeout(m.ctx, 5*time.Second)
		m.operations.Lock()
		err := m.maintenanceStep(ctx, stable, time.Now())
		m.operations.Unlock()
		cancel()
		if err != nil {
			clear(stable)
		} // failed observation never counts as stability
	}
}

// maintenanceStep treats an incomplete global inventory exactly like an
// unsuccessful per-session observation. A failed durable invalidation remains
// latched: no later successful probe may reuse the old idle/warning window.
func (m *Manager) maintenanceStep(ctx context.Context, stable map[string]stableProcess, now time.Time) error {
	if m.cleanupInvalidated.Load() {
		if err := m.cancelCleanupPlans(); err != nil {
			return err
		}
	}
	err := m.restartStep(ctx, stable)
	if err == nil {
		err = m.cleanupStep(ctx, now)
	}
	if err == nil {
		err = m.flushActivity(now, false)
	}
	if err != nil {
		clear(stable)
		m.cleanupInvalidated.Store(true)
		return errors.Join(err, m.cancelCleanupPlans())
	}
	return nil
}
func (m *Manager) cancelCleanupPlans() error {
	snapshot, err := m.store.View()
	if err != nil {
		return err
	}
	if len(snapshot.Buckets[cleanupBucket]) > 0 {
		if err := m.store.Update(func(s *storage.Snapshot) error { delete(s.Buckets, cleanupBucket); return nil }); err != nil {
			return err
		}
	}
	clear(m.cleanupSeen)
	m.lastCleanupObservation = time.Time{}
	m.cleanupInvalidated.Store(false)
	return nil
}

type stableProcess struct {
	pane  string
	pid   int
	since time.Time
}

func (m *Manager) restartStep(ctx context.Context, stable map[string]stableProcess) error {
	state, err := m.store.View()
	if err != nil {
		return err
	}
	panes, err := m.tmux.List(ctx)
	if err != nil {
		return err
	}
	byID := map[string][]tmux.Pane{}
	for _, pane := range panes {
		byID[pane.SessionID] = append(byID[pane.SessionID], pane)
	}
	for id := range m.crashNotified {
		if _, ok := state.Buckets[storage.SessionsBucket][id]; !ok {
			delete(m.crashNotified, id)
		}
	}
	for id := range stable {
		if _, ok := state.Buckets[storage.SessionsBucket][id]; !ok {
			delete(stable, id)
		}
	}
	for id, data := range state.Buckets[storage.SessionsBucket] {
		if err := ctx.Err(); err != nil {
			return err
		}
		var value pb.Session
		if err := json.Unmarshal(data, &value); err != nil {
			return err
		}
		list := byID[id]
		if value.Lifecycle != "ready" || len(list) != 1 {
			delete(stable, id)
			continue
		}
		pane := list[0]
		if pane.PaneID != value.TmuxPane || pane.WindowID != value.TmuxWindow {
			delete(stable, id)
			continue
		}
		var record RestartRecord
		if err := state.Get(restartBucket, id, &record); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		before, _ := json.Marshal(record)
		oldStatus := value.ClaudeStatus
		if record.Suppressed || value.ClaudeStatus == "stopped" {
			delete(stable, id)
			continue
		}
		// A previous unknown exit observation is not permanent. Reinspect the
		// same registered pane, but never treat an unknown live command or an
		// unavailable exit reason as evidence for restarting it.
		if value.ClaudeStatus == "unknown" && !pane.Dead {
			delete(stable, id)
			continue
		}
		now := time.Now()
		due := false
		if !pane.Dead {
			delete(m.crashNotified, id)
			observed, ok := stable[id]
			identity := pane.TmuxSessionID + "/" + pane.WindowID + "/" + pane.PaneID
			if !ok || observed.pane != identity || observed.pid != pane.PID {
				stable[id] = stableProcess{pane: identity, pid: pane.PID, since: now}
			} else if m.opts.AutoRestart.Enabled && now.Sub(observed.since) >= m.opts.AutoRestart.StableWindow && record.Attempts > 0 {
				record.Attempts = 0
				record.NextAttempt = 0
				record.ReasonCode = "STABLE_WINDOW_REACHED"
			}
		} else {
			delete(stable, id)
			applyPane(&value, pane)
			record.LastExit = nil
			record.LastSignal = pane.ExitSignal
			if pane.ExitSignal == 0 && !pane.ExitSignalUnknown {
				record.LastExit = pane.ExitStatus
			}
			if !m.opts.AutoRestart.Enabled {
				record.ReasonCode = "AUTO_RESTART_DISABLED"
				record.NextAttempt = 0
			} else if pane.ExitSignalUnknown || pane.ExitSignal == 0 && pane.ExitStatus == nil {
				record.ReasonCode = "EXIT_REASON_UNKNOWN"
				record.NextAttempt = 0
			} else if !pane.AbnormalExit() {
				record.ReasonCode = "NORMAL_EXIT"
				record.NextAttempt = 0
			} else if record.Attempts >= m.opts.AutoRestart.MaxAttempts {
				record.ReasonCode = "RESTART_LIMIT_REACHED"
				record.NextAttempt = 0
			} else if record.NextAttempt == 0 {
				record.NextAttempt = now.Add(restartDelay(m.opts.AutoRestart.Backoff, record.Attempts)).UnixMilli()
				record.ReasonCode = "RESTART_BACKOFF"
			} else if now.UnixMilli() >= record.NextAttempt {
				record.Attempts++
				record.NextAttempt = 0
				record.ReasonCode = "RESTART_ATTEMPT"
				due = true
			}
		}
		after, _ := json.Marshal(record)
		if string(before) != string(after) || oldStatus != value.ClaudeStatus {
			if err := m.store.Update(func(current *storage.Snapshot) error {
				if err := current.Put(restartBucket, id, &record); err != nil {
					return err
				}
				return current.Put(storage.SessionsBucket, id, &value)
			}); err != nil {
				return err
			}
		}
		if pane.Dead && (pane.ExitSignalUnknown || pane.ExitSignal != 0 || pane.ExitStatus == nil || *pane.ExitStatus != 0) {
			m.notifyExit(id, pane)
		}
		if due {
			if _, err := m.processActionLocked(ctx, &pb.SessionActionRequest{SessionId: id}, true, true); err != nil {
				record.ReasonCode = "RESTART_FAILED"
				if persist := m.store.Update(func(current *storage.Snapshot) error { return current.Put(restartBucket, id, &record) }); persist != nil {
					return errors.Join(err, persist)
				}
			}
		}
	}
	return nil
}

func restartDelay(base time.Duration, attempt int) time.Duration {
	const maximum = 30 * time.Second
	if base >= maximum {
		return maximum
	}
	for range attempt {
		if base >= maximum/2 {
			return maximum
		}
		base *= 2
	}
	return base
}

// notifyExit runs under operations after the observed state is committed.
// A fresh daemon epoch has fresh attachment sequences; no event is replayed.
func (m *Manager) notifyExit(id string, pane tmux.Pane) {
	known := !pane.ExitSignalUnknown && (pane.ExitSignal != 0 || pane.ExitStatus != nil)
	kind := pb.SessionEvent_CLAUDE_STOPPED
	message := "Claude process ended; exit reason unavailable"
	data := map[string]string{"reason_code": "EXIT_REASON_UNKNOWN"}
	if known {
		kind = pb.SessionEvent_CLAUDE_CRASHED
		message = "Claude process terminated unexpectedly"
		data = map[string]string{}
	}
	identity := fmt.Sprintf("%s/%s/%s/%d", pane.TmuxSessionID, pane.WindowID, pane.PaneID, pane.PID)
	previous := m.crashNotified[id]
	if previous == identity+"/"+kind.String() || previous == identity+"/"+pb.SessionEvent_CLAUDE_CRASHED.String() {
		return // Once known, a later missing observation cannot repeat unknown.
	}
	m.crashNotified[id] = identity + "/" + kind.String()
	m.mu.Lock()
	bridge := m.bridges[id]
	m.mu.Unlock()
	if bridge == nil {
		return
	}
	if known && pane.ExitSignal == 0 && pane.ExitStatus != nil {
		data["exit_code"] = strconv.Itoa(*pane.ExitStatus)
	}
	if known && pane.ExitSignal != 0 {
		data["exit_signal"] = strconv.Itoa(pane.ExitSignal)
	}
	bridge.PublishEvent(&pb.SessionEvent{Type: kind, Message: message, Data: data})
}
