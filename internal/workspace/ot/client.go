package ot

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strconv"
)

// ErrUnexpectedAck: an ack that does not match the outstanding operation.
var ErrUnexpectedAck = errors.New("ot: ack for an operation that is not pending")

// Pending is one operation sent to the sequencer and awaiting its ack.
type Pending struct {
	ClientOpID string
	BaseSeq    int64
	Op         Op
}

// Client is one editor's view of a sequenced document (ot.js Client:
// synchronized / awaiting confirm / awaiting with buffer). Local edits apply
// at once; at most one operation is outstanding and later edits compose into
// a buffer. Not safe for concurrent use.
type Client struct {
	prefix  string
	counter int
	seq     int64
	base    []rune   // the sequencer's document at seq
	pending *Pending // Op transformed so far, relative to base
	buffer  Op       // relative to base+pending; nil when empty
	text    []rune   // base + pending + buffer
	resync  bool
}

// NewClient starts synchronized at seq with text. Client op ids are
// idPrefix + "-" + 16 random hex digits (new for every Client) + "-" +
// counter, so a document reopened (or a masil restarted) with the same
// prefix never reuses an id the sequencer still remembers.
func NewClient(idPrefix string, seq int64, text []rune) *Client {
	t := append([]rune(nil), text...)
	var nonce [8]byte
	_, _ = rand.Read(nonce[:]) // crypto/rand.Read never fails (Go 1.24+)
	prefix := idPrefix + "-" + hex.EncodeToString(nonce[:])
	return &Client{prefix: prefix, seq: seq, base: t, text: append([]rune(nil), t...)}
}

// Text is a copy of the local document.
func (c *Client) Text() []rune { return append([]rune(nil), c.text...) }

// Seq is the last sequence number this client has incorporated.
func (c *Client) Seq() int64 { return c.seq }

// Busy reports unconfirmed local edits or a resync in progress.
func (c *Client) Busy() bool { return c.pending != nil || c.buffer != nil || c.resync }

func (c *Client) newPending(op Op) *Pending {
	c.counter++
	c.pending = &Pending{ClientOpID: c.prefix + "-" + strconv.Itoa(c.counter), BaseSeq: c.seq, Op: op}
	out := *c.pending
	return &out
}

// Local applies a local edit. It returns what to send when nothing is
// outstanding; otherwise the edit joins the buffer and Local returns nil.
func (c *Client) Local(op Op) (*Pending, error) {
	text, err := op.Apply(c.text)
	if err != nil {
		return nil, err
	}
	if c.pending != nil || c.resync {
		if c.buffer == nil {
			c.buffer = op
		} else if c.buffer, err = Compose(c.buffer, op); err != nil {
			return nil, err
		}
		c.text = text
		return nil, nil
	}
	c.text = text
	return c.newPending(op), nil
}

// Ack confirms the outstanding operation at seq and returns the buffer as
// the next operation to send, if any. Ignored while resyncing.
func (c *Client) Ack(clientOpID string, seq int64) (*Pending, error) {
	if c.resync {
		return nil, nil
	}
	if c.pending == nil || c.pending.ClientOpID != clientOpID {
		return nil, ErrUnexpectedAck
	}
	base, err := c.pending.Op.Apply(c.base)
	if err != nil {
		return nil, err
	}
	c.base, c.seq, c.pending = base, seq, nil
	if c.buffer == nil {
		return nil, nil
	}
	buf := c.buffer
	c.buffer = nil
	return c.newPending(buf), nil
}

// Remote takes someone else's sequenced operation and returns it
// transformed over the outstanding and buffered edits — the operation to
// apply to the local view. While resyncing it is ignored (a no-op returned).
func (c *Client) Remote(seq int64, op Op) (Op, error) {
	if c.resync {
		return Noop(len(c.text)), nil
	}
	base, err := op.Apply(c.base)
	if err != nil {
		return nil, err
	}
	m := op
	var p2, b2 Op
	if c.pending != nil {
		if p2, m, err = Transform(c.pending.Op, m); err != nil {
			return nil, err
		}
	}
	if c.buffer != nil {
		if b2, m, err = Transform(c.buffer, m); err != nil {
			return nil, err
		}
	}
	text, err := m.Apply(c.text)
	if err != nil {
		return nil, err
	}
	if c.pending != nil {
		c.pending.Op = p2
	}
	if c.buffer != nil {
		c.buffer = b2
	}
	c.base, c.text, c.seq = base, text, seq
	return m, nil
}

// Resync starts a reconnect: until Reset/ResetAcked/Drop, nothing is sent
// (local edits buffer) and acks and remote operations are ignored. It returns
// the outstanding operation's client op id ("" if none) so the sequencer can
// say whether it was already applied.
func (c *Client) Resync() string {
	c.resync = true
	if c.pending == nil {
		return ""
	}
	return c.pending.ClientOpID
}

// Reset lays the unconfirmed local edits (outstanding + buffer) on top of a
// freshly received sequencer state that does not contain the outstanding
// operation, and returns them as the next operation to send.
func (c *Client) Reset(seq int64, text []rune) (*Pending, error) {
	from := c.base
	u := c.buffer
	if c.pending != nil {
		if u == nil {
			u = c.pending.Op
		} else {
			var err error
			if u, err = Compose(c.pending.Op, c.buffer); err != nil {
				return nil, err
			}
		}
	}
	return c.relay(seq, text, from, u)
}

// ResetAcked is Reset for a state that already contains the outstanding
// operation (its ack was lost): only the buffer is laid on top.
func (c *Client) ResetAcked(seq int64, text []rune) (*Pending, error) {
	from := c.base
	if c.pending != nil {
		var err error
		if from, err = c.pending.Op.Apply(c.base); err != nil {
			return nil, err
		}
	}
	return c.relay(seq, text, from, c.buffer)
}

// relay rebases u (made against from) onto text at seq.
func (c *Client) relay(seq int64, text, from []rune, u Op) (*Pending, error) {
	t := append([]rune(nil), text...)
	local := append([]rune(nil), t...)
	if u != nil && !u.IsNoop() {
		x := Diff(from, t)
		u2, _, err := Transform(u, x)
		if err != nil {
			return nil, err
		}
		if local, err = u2.Apply(t); err != nil {
			return nil, err
		}
		u = u2
	} else {
		u = nil
	}
	c.seq, c.base, c.text = seq, t, local
	c.pending, c.buffer, c.resync = nil, nil, false
	if u == nil {
		return nil, nil
	}
	return c.newPending(u), nil
}

// Drop discards every unconfirmed edit and adopts the sequencer's state
// (after doc_reject).
func (c *Client) Drop(seq int64, text []rune) {
	t := append([]rune(nil), text...)
	c.seq, c.base, c.text = seq, t, append([]rune(nil), t...)
	c.pending, c.buffer, c.resync = nil, nil, false
}

// trimDiff keeps the common prefix and suffix and replaces the middle
// (Diff uses it inside each changed hunk).
func trimDiff(a, b []rune) Op {
	p := 0
	for p < len(a) && p < len(b) && a[p] == b[p] {
		p++
	}
	s := 0
	for s < len(a)-p && s < len(b)-p && a[len(a)-1-s] == b[len(b)-1-s] {
		s++
	}
	var bl builder
	bl.retain(p)
	bl.delete(len(a) - p - s)
	bl.insert(string(b[p : len(b)-s]))
	bl.retain(s)
	return bl.result()
}
