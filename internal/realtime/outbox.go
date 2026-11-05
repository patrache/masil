package realtime

import "time"

// Outgoing frame queue (CA 4.4): at most outboxMax frames per sliding
// outboxWindow (ping excluded), so the server's 30/10s limit (events.md 2) is
// never hit. Frames over the budget wait in order.
const (
	outboxWindow = 10 * time.Second
	outboxMax    = 25
)

// Frame kinds in the queue. idle and watch are state, so a newer queued frame
// of the same kind replaces an older one that has not been written yet.
const (
	kindTyping = "typing"
	kindIdle   = "idle"
	kindWatch  = "watch"
	kindAck    = "ack"
)

type outItem struct {
	kind string
	data []byte
}

// outbox is not safe for concurrent use; Client guards it with its mutex.
type outbox struct {
	queue []outItem
	sent  []time.Time // non-ping writes inside the current window, oldest first
}

// push appends a frame; idle/watch/ack replace a queued frame of the same kind.
func (o *outbox) push(kind string, data []byte) {
	if kind != kindTyping {
		for i := range o.queue {
			if o.queue[i].kind == kind {
				o.queue[i].data = data
				return
			}
		}
	}
	o.queue = append(o.queue, outItem{kind: kind, data: data})
}

// prune drops send times that left the window.
func (o *outbox) prune(now time.Time) {
	cut := now.Add(-outboxWindow)
	i := 0
	for i < len(o.sent) && !o.sent[i].After(cut) {
		i++
	}
	o.sent = o.sent[i:]
}

// pop returns the next frame if the window allows it now (recording the send).
// Otherwise ok is false and wait is how long until the window frees a slot
// (0 when the queue is empty).
func (o *outbox) pop(now time.Time) (it outItem, ok bool, wait time.Duration) {
	if len(o.queue) == 0 {
		return outItem{}, false, 0
	}
	o.prune(now)
	if len(o.sent) >= outboxMax {
		return outItem{}, false, max(o.sent[0].Add(outboxWindow).Sub(now), time.Nanosecond)
	}
	it = o.queue[0]
	o.queue[0] = outItem{}
	o.queue = o.queue[1:]
	o.sent = append(o.sent, now)
	return it, true, 0
}

// count records a frame written outside the queue (resume) against the window.
func (o *outbox) count(now time.Time) {
	o.prune(now)
	o.sent = append(o.sent, now)
}

// reset forgets the queue and the window (new connection: the server limit is per connection).
func (o *outbox) reset() {
	o.queue = nil
	o.sent = nil
}
