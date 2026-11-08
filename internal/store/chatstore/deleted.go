package chatstore

import (
	"slices"

	"github.com/patrache/masil/internal/api"
)

// Events of different requests can cross (events.md 6.4): a message.deleted can
// arrive before the message.created (or a message.updated, or a slower REST
// copy) of the same message, while we hold no copy to tombstone. Every
// deletion is therefore remembered per channel, held or not, so a later copy
// of that id lands as the placeholder (tombstone) or not at all (removed). The
// server never undeletes, so a remembered id is final.

// maxDeleted bounds the deleted ids remembered per channel (oldest leave first).
const maxDeleted = 256

// deletedSet is the recently deleted message ids of one channel.
type deletedSet struct {
	ids  []string        // deletion order, oldest first
	tomb map[string]bool // id -> it left a placeholder (tombstone=true)
}

func (d *deletedSet) has(id string) bool {
	if d == nil {
		return false
	}
	_, ok := d.tomb[id]
	return ok
}

// noteDeleted remembers a deletion of id in channelID.
func (c *ChatStore) noteDeleted(channelID, id string, tombstone bool) {
	d := c.deleted[channelID]
	if d == nil {
		d = &deletedSet{tomb: map[string]bool{}}
		c.deleted[channelID] = d
	}
	if _, ok := d.tomb[id]; !ok {
		d.ids = append(d.ids, id)
		if len(d.ids) > maxDeleted {
			delete(d.tomb, d.ids[0])
			d.ids = slices.Delete(d.ids, 0, 1)
		}
	}
	d.tomb[id] = d.tomb[id] || tombstone
}

// deletedCopy maps a copy of a message that is known deleted to what may be
// held of it: the placeholder (keep = true) or nothing (keep = false). ok is
// false when the message is not known deleted (m is used as it is).
func (c *ChatStore) deletedCopy(m api.Message) (out api.Message, keep, ok bool) {
	d := c.deleted[m.ChannelID]
	if !d.has(m.ID) {
		return m, true, false
	}
	if !d.tomb[m.ID] {
		return m, false, true
	}
	tombstone(&m)
	return m, true, true
}
