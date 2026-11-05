package realtime

import (
	"encoding/json"
	"time"
)

// Event is one server frame (events.md 2).
type Event struct {
	Type     string // events.md 8장 name
	Seq      *int64 // nil for ephemeral
	TS       time.Time
	Data     json.RawMessage
	Replayed bool // received before `ready` on this connection
}

// Decode returns the typed payload (CA 4.4 table); unknown type => (nil, nil).
func (e Event) Decode() (any, error) {
	// TODO(core): Task 12
	return nil, nil
}
