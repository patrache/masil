package store

import "github.com/patrache/masil/internal/realtime"

// A bootstrap fetches more than ten REST resources and lands hundreds of
// milliseconds after it started, so its snapshot is older than the realtime
// events applied meanwhile. The flight journal records those events (and
// channels observed from REST responses outside Apply, e.g. a DM the user just
// opened) and ApplyBootstrap re-applies them on top of the snapshot: events.md
// 5.3 step 5, "realtime events arriving during the resync are still applied".

// maxFlightOps bounds the journal; past it the journal is dropped and
// Sync.Stale asks for another bootstrap.
const maxFlightOps = 4096

type flight struct {
	ops      []func()
	overflow bool
}

// BeginBootstrap starts the flight journal of a bootstrap about to be fetched;
// the next ApplyBootstrap replays and ends it. Calling it again restarts it.
func (s *Store) BeginBootstrap() {
	s.flight = &flight{}
}

// record appends op to the flight journal, if one is open.
func (s *Store) record(op func()) {
	f := s.flight
	if f == nil || f.overflow || s.replaying {
		return
	}
	if len(f.ops) >= maxFlightOps {
		f.ops, f.overflow = nil, true
		return
	}
	f.ops = append(f.ops, op)
}

// recordEvent journals a decoded core event for replay. Alerts (the ring would
// get duplicates) and events whose state no bootstrap replaces are skipped.
func (s *Store) recordEvent(ev realtime.Event, v any) {
	switch v.(type) {
	case realtime.AlertEvent, realtime.PresenceSnapshot, realtime.PresenceUpdated:
		return
	case realtime.MessageEvent:
		if ev.Type != "message.created" {
			return
		}
	}
	s.record(func() {
		if v, err := ev.Decode(); err == nil && v != nil {
			s.applyCore(ev, v)
		}
	})
}

// replayFlight re-applies the journal over the snapshot ApplyBootstrap just
// stored and ends the flight. Counters are guarded: a message at or before the
// channel's snapshot last_message_id is already in the REST counts, and member
// counts (no ID to merge by) are left to the snapshot.
func (s *Store) replayFlight() {
	f := s.flight
	s.flight = nil
	if f == nil {
		return
	}
	if f.overflow {
		s.Sync.Stale = true
		return
	}
	s.snapshotLast = make(map[string]string, len(s.Channels))
	for id, ch := range s.Channels {
		if ch.LastMessageID != nil {
			s.snapshotLast[id] = *ch.LastMessageID
		}
	}
	counted, mentioned := s.counted, s.mentioned
	s.counted, s.mentioned = newIDSet(dedupeIDs), newIDSet(dedupeIDs) // de-dupe within the replay only
	s.replaying = true
	for _, op := range f.ops {
		op()
	}
	s.replaying = false
	s.counted, s.mentioned = counted, mentioned
	s.snapshotLast = nil
}

// inSnapshot reports, during a replay, that messageID is already counted by the
// REST snapshot of channelID.
func (s *Store) inSnapshot(channelID, messageID string) bool {
	last := s.snapshotLast[channelID]
	return last != "" && messageID <= last
}
