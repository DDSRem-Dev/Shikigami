package session

import (
	"github.com/DDSRem-Dev/Shikigami/internal/server/storage"
	pb "github.com/DDSRem-Dev/Shikigami/pkg/protocol"
	"time"
)

// Activity callbacks never acquire operations or write storage under a bridge
// lock. Output, health polling and passive time do not count as user activity.
func (m *Manager) recordActivity(id string) {
	m.activityMu.Lock()
	m.activity[id] = max(m.activity[id], time.Now().UnixMilli())
	m.activityMu.Unlock()
}
func (m *Manager) flushActivity(now time.Time, force bool) error {
	if !force && now.Sub(m.lastActivityFlush) < 5*time.Second {
		return nil
	}
	m.activityMu.Lock()
	pending := make(map[string]int64, len(m.activity))
	for id, at := range m.activity {
		pending[id] = at
	}
	m.activityMu.Unlock()
	if len(pending) == 0 {
		m.lastActivityFlush = now
		return nil
	}
	err := m.store.Update(func(s *storage.Snapshot) error {
		for id, at := range pending {
			if _, ok := s.Buckets[storage.SessionsBucket][id]; !ok {
				continue
			}
			cleanup, err := m.cleanupRecord(s, id)
			if err != nil {
				return err
			}
			if cleanup.Since > 0 && at >= cleanup.Since {
				delete(s.Buckets[cleanupBucket], id)
			}
			var value pb.Session
			if err := s.Get(storage.SessionsBucket, id, &value); err != nil {
				return err
			}
			if at/1000 > value.LastActive {
				value.LastActive = at / 1000
				if err := s.Put(storage.SessionsBucket, id, &value); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	m.activityMu.Lock()
	for id, at := range pending {
		if m.activity[id] <= at {
			delete(m.activity, id)
		}
	}
	m.activityMu.Unlock()
	m.lastActivityFlush = now
	return nil
}
