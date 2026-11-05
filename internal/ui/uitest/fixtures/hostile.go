package fixtures

import (
	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/store"
)

// Hostile adds the hostile set to a seeded Store (separate space "☠ 적대적 데이터").
func Hostile(s *store.Store) {
	// TODO(core): Task 23
}

// HostileMessages returns the hostile message set.
func HostileMessages(channelID string) []api.Message {
	// TODO(core): Task 23
	return nil
}

// HostileTimeline returns the hostile feed set.
func HostileTimeline() []api.TimelineItem {
	// TODO(core): Task 23
	return nil
}
