package state

import "os"

// Get reads one committed record without cloning the complete store. It shares
// Update's lock, so it cannot observe a partially committed transaction. The
// record is copied under that lock, then decoded outside it into a fresh value;
// even a custom JSON decoder cannot retain or modify the live store's bytes.
// As with Snapshot.Get, missing records and decode errors leave value unchanged.
// Use View when multiple records must come from the same snapshot.
func (s *Store) Get(bucket, id string, value any) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return ErrClosed
	}
	data, ok := s.state.Buckets[bucket][id]
	if !ok {
		s.mu.Unlock()
		return os.ErrNotExist
	}
	copy := append([]byte(nil), data...)
	s.mu.Unlock()
	return decodeRecord(copy, value)
}
