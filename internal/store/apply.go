package store

import "github.com/patrache/masil/internal/realtime"

// Apply applies a realtime event to core entities, then every registered reducer.
func (s *Store) Apply(ev realtime.Event) {
	// TODO(core): Task 14
}
