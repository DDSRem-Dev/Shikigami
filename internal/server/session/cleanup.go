package session

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/DDSRem-Dev/Shikigami/internal/server/storage"
	"github.com/DDSRem-Dev/Shikigami/internal/server/tmux"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
)

const cleanupBucket = "session.cleanup"

type CleanupPolicy struct {
	Enabled                   bool
	IdleThreshold, WarnBefore time.Duration
}

func (p CleanupPolicy) validate() error {
	if p.Enabled && (p.WarnBefore < time.Second || p.IdleThreshold <= p.WarnBefore) {
		return errors.New("automatic cleanup requires 1s <= warning duration < idle threshold")
	}
	return nil
}

// CleanupRecord is an independent durable decision, never inferred from last_active.
// Deleting records may finish after a crash only if the exact old pane remains dead
// or is absent. They never enter the unconditional explicit-delete recovery path.
type CleanupRecord struct {
	Since    int64  `json:"since"`
	Warning  int64  `json:"warning,omitempty"`
	Due      int64  `json:"due,omitempty"`
	Idle     int64  `json:"idle"`
	Warn     int64  `json:"warn"`
	Session  string `json:"session"`
	Window   string `json:"window"`
	Pane     string `json:"pane"`
	Deleting bool   `json:"deleting,omitempty"`
}

func (m *Manager) cleanupRecord(state *storage.Snapshot, id string) (CleanupRecord, error) {
	var r CleanupRecord
	err := state.Get(cleanupBucket, id, &r)
	if errors.Is(err, os.ErrNotExist) {
		return r, nil
	}
	if err != nil {
		return r, err
	}
	if r.Since <= 0 || r.Idle <= r.Warn || r.Warn < 1000 || r.Idle > int64((time.Duration(1<<63-1))/time.Millisecond) || r.Session == "" || r.Window == "" || r.Pane == "" || r.Warning < 0 || r.Due < 0 || (r.Warning == 0) != (r.Due == 0) || (r.Warning > 0 && (r.Due-r.Warning < r.Warn || r.Due-r.Since < r.Idle)) || r.Deleting && r.Warning == 0 {
		return r, errors.New("invalid session cleanup state")
	}
	return r, nil
}
func (m *Manager) cleanupEligibility(ctx context.Context, state *storage.Snapshot, value *pb.Session) (string, tmux.Pane, error) {
	if !m.opts.AutoCleanup.Enabled {
		return "disabled", tmux.Pane{}, nil
	}
	var intent RestartRecord
	if err := state.Get(restartBucket, value.Id, &intent); err != nil && !errors.Is(err, os.ErrNotExist) {
		return "unknown", tmux.Pane{}, err
	}
	if value.Lifecycle != "ready" || value.ClaudeStatus != "stopped" || !intent.Suppressed || intent.ReasonCode != "USER_STOP" {
		return "not_explicitly_stopped", tmux.Pane{}, nil
	}
	m.mu.Lock()
	bridge := m.bridges[value.Id]
	m.mu.Unlock()
	if bridge != nil {
		if count, _, _ := bridge.State(); count > 0 {
			return "attached", tmux.Pane{}, nil
		}
	}
	pane, err := m.tmux.Inspect(ctx, value.Id)
	if err != nil {
		return "unknown", pane, err
	}
	if pane.PaneID != value.TmuxPane || pane.WindowID != value.TmuxWindow {
		return "unknown", pane, nil
	}
	if !pane.Dead {
		return "live_pane", pane, nil
	}
	return "observing", pane, nil
}
func (m *Manager) decorateCleanup(ctx context.Context, state *storage.Snapshot, value *pb.Session) {
	value.CleanupWarningAt = nil
	value.CleanupAfter = nil
	if m.cleanupInvalidated.Load() {
		value.CleanupStatus = "unknown"
		return
	}
	status, pane, err := m.cleanupEligibility(ctx, state, value)
	value.CleanupStatus = status
	r, e := m.cleanupRecord(state, value.Id)
	if err != nil || e != nil {
		value.CleanupStatus = "unknown"
		return
	}
	m.activityMu.Lock()
	activity := m.activity[value.Id]
	m.activityMu.Unlock()
	if activity >= r.Since && r.Since > 0 {
		return
	}
	if status == "observing" && r.Warning > 0 && r.Session == pane.TmuxSessionID && r.Window == pane.WindowID && r.Pane == pane.PaneID && time.Now().UnixMilli() >= r.Warning {
		value.CleanupStatus = "warned"
		w, d := r.Warning/1000, r.Due/1000
		value.CleanupWarningAt = &w
		value.CleanupAfter = &d
	}
}

// cleanupStep runs under operations, sharing serialization with explicit lifecycle
// changes. No wall-clock absence/terminal silence is evidence that a task stopped.
func (m *Manager) cleanupStep(ctx context.Context, now time.Time) error {
	if !m.lastCleanupObservation.IsZero() && wallClockRegressed(now, m.lastCleanupObservation) {
		if err := m.store.Update(func(s *storage.Snapshot) error { delete(s.Buckets, cleanupBucket); return nil }); err != nil {
			return err
		}
	}
	m.lastCleanupObservation = now
	state, err := m.store.View()
	if err != nil {
		return err
	}
	for id := range m.cleanupSeen {
		if _, ok := state.Buckets[storage.SessionsBucket][id]; !ok {
			delete(m.cleanupSeen, id)
		}
	}
	for id := range state.Buckets[storage.SessionsBucket] {
		value := new(pb.Session)
		if err := state.Get(storage.SessionsBucket, id, value); err != nil {
			return err
		}
		r, err := m.cleanupRecord(state, id)
		if err != nil {
			return err
		}
		status, pane, observeErr := m.cleanupEligibility(ctx, state, value)
		if !m.cleanupSeen[id] {
			if r.Since > 0 && !r.Deleting {
				r.Since = now.UnixMilli()
				if r.Warning > 0 {
					r.Due = max(r.Due, r.Since+r.Idle, r.Warning+r.Warn)
				}
				if err := m.store.Update(func(s *storage.Snapshot) error { return s.Put(cleanupBucket, id, &r) }); err != nil {
					return err
				}
			}
			m.cleanupSeen[id] = true
		}
		if r.Deleting && m.opts.AutoCleanup.Enabled && errors.Is(observeErr, tmux.ErrNotFound) {
			if err := m.finishAutomaticCleanup(ctx, id, r); err != nil {
				return err
			}
			continue
		}
		m.activityMu.Lock()
		activity := m.activity[id]
		m.activityMu.Unlock()
		same := r.Since > 0 && r.Session == pane.TmuxSessionID && r.Window == pane.WindowID && r.Pane == pane.PaneID && r.Idle == m.opts.AutoCleanup.IdleThreshold.Milliseconds() && r.Warn == m.opts.AutoCleanup.WarnBefore.Milliseconds() && now.UnixMilli() >= r.Since && now.UnixMilli() >= r.Warning && activity < r.Since
		if status != "observing" || observeErr != nil || !same {
			if r.Since > 0 {
				if err := m.store.Update(func(s *storage.Snapshot) error { delete(s.Buckets[cleanupBucket], id); return nil }); err != nil {
					return err
				}
			}
			if status != "observing" || observeErr != nil {
				continue
			}
			r = CleanupRecord{Since: now.UnixMilli(), Idle: m.opts.AutoCleanup.IdleThreshold.Milliseconds(), Warn: m.opts.AutoCleanup.WarnBefore.Milliseconds(), Session: pane.TmuxSessionID, Window: pane.WindowID, Pane: pane.PaneID}
			if err := m.store.Update(func(s *storage.Snapshot) error { return s.Put(cleanupBucket, id, &r) }); err != nil {
				return err
			}
		}
		if r.Warning == 0 && now.UnixMilli()-r.Since >= r.Idle-r.Warn {
			r.Warning = now.UnixMilli()
			r.Due = max(r.Since+r.Idle, r.Warning+r.Warn)
			if err := m.store.Update(func(s *storage.Snapshot) error { return s.Put(cleanupBucket, id, &r) }); err != nil {
				return err
			}
		}
		if r.Warning > 0 && now.UnixMilli() >= r.Due {
			if err := m.finishAutomaticCleanup(ctx, id, r); err != nil {
				return err
			}
		}
	}
	return nil
}
func (m *Manager) finishAutomaticCleanup(ctx context.Context, id string, r CleanupRecord) error {
	m.mu.Lock()
	bridge := m.bridges[id]
	m.mu.Unlock()
	if bridge != nil {
		bridge.op.Lock()
		defer bridge.op.Unlock()
		if count, _, _ := bridge.State(); count > 0 {
			return nil
		}
	}
	m.activityMu.Lock()
	activity := m.activity[id]
	m.activityMu.Unlock()
	if activity >= r.Since {
		return m.store.Update(func(s *storage.Snapshot) error { delete(s.Buckets[cleanupBucket], id); return nil })
	}
	// Persist conditional intent before touching tmux. A failed commit performs no
	// deletion, and a post-delete persistence failure can safely finish on restart.
	r.Deleting = true
	if err := m.store.Update(func(s *storage.Snapshot) error { return s.Put(cleanupBucket, id, &r) }); err != nil {
		return err
	}
	deleted, err := m.tmux.DeleteDead(ctx, id, tmux.Pane{TmuxSessionID: r.Session, WindowID: r.Window, PaneID: r.Pane})
	if err != nil {
		return err
	}
	if !deleted {
		return m.store.Update(func(s *storage.Snapshot) error { delete(s.Buckets[cleanupBucket], id); return nil })
	}
	if bridge != nil {
		bridge.mu.Lock()
		bridge.closed = true
		bridge.cancel()
		bridge.mu.Unlock()
	}
	if err := m.store.Update(func(s *storage.Snapshot) error {
		delete(s.Buckets[storage.SessionsBucket], id)
		delete(s.Buckets[restartBucket], id)
		delete(s.Buckets[cleanupBucket], id)
		return nil
	}); err != nil {
		return err
	}
	delete(m.cleanupSeen, id)
	m.mu.Lock()
	delete(m.bridges, id)
	m.mu.Unlock()
	m.activityMu.Lock()
	delete(m.activity, id)
	m.activityMu.Unlock()
	return nil
}

// Deadlines are durable wall-clock timestamps. time.Time.Before would use
// monotonic readings for two time.Now values and miss a wall-only adjustment.
func wallClockRegressed(now, previous time.Time) bool { return now.UnixMilli() < previous.UnixMilli() }
