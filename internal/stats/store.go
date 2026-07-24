// Package stats provides an in-memory, concurrency-safe event counter.
package stats

import "sync"

// Store counts processed events by event type.
//
// Store must be constructed with NewStore. Snapshot returns an independent
// point-in-time copy; callers never receive the store's internal map.
type Store struct {
	mu     sync.RWMutex
	counts map[string]uint64
}

// NewStore returns an initialized, empty Store.
func NewStore() *Store {
	return &Store{
		counts: make(map[string]uint64),
	}
}

// Increment increases the processed count for eventType by one.
func (s *Store) Increment(eventType string) {
	s.mu.Lock()
	s.counts[eventType]++
	s.mu.Unlock()
}

// Snapshot returns an independent point-in-time copy of all counters.
func (s *Store) Snapshot() map[string]uint64 {
	s.mu.RLock()
	snapshot := make(map[string]uint64, len(s.counts))
	for eventType, count := range s.counts {
		snapshot[eventType] = count
	}
	s.mu.RUnlock()

	return snapshot
}
