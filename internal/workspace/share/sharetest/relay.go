// Package sharetest is an in-memory WorkspaceHub for tests: the server's
// routing rules (docs/contract/workspace.md 4 / 4.1, server
// src/do/workspace-hub.ts) in Go, with synchronous delivery.
//
// A Send routes at once (levels as they are then) and queues the
// deliveries; Pump hands every queued frame to its connection's Recv. Close
// codes take effect at once. The per-connection rate limits (rule 6) read the
// relay's clock (time.Now, or SetClock). Not carried over: the 2-minute owner
// grace alarm (rule 7; call End) and the 30-second attention gap.
package sharetest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/workspace/share"
	"github.com/patrache/masil/internal/workspace/wire"
)

// inboxSize is how many delivered frames a connection holds before Pump
// panics: a test must drain Recv between pumps.
const inboxSize = 4096

// cacheCap is the keyframe cache cap (8 MiB, as the hub).
const cacheCap = 8 << 20

// Rule 6 limits, as the hub's WORKSPACE_HUB_LIMITS.
const (
	termInputPerSec  = 50
	docOpPerSec      = 30
	propPerMin       = 10
	ownerBytesPerSec = 2 << 20
	overSeconds      = 3 // consecutive seconds over a limit before 4008
)

// ErrDropped is the Err of a connection cut by Drop (no close code, like a
// network loss).
var ErrDropped = errors.New("sharetest: connection dropped")

// Relay is one share's hub.
type Relay struct {
	mu        sync.Mutex
	shareID   string
	ownerID   string
	members   map[uint8]string // seat -> user id (kicked seats removed)
	levels    map[uint8]wire.Level
	verified  map[uint8]bool
	conns     map[uint8]*conn // the live connection of each seat
	queue     []delivery
	tamper    func(from, to uint8, frame []byte) []byte
	cache     [][]byte
	cacheSize int
	cacheOn   bool
	ended     bool
	attention int
	now       func() time.Time
}

type delivery struct {
	to    *conn
	frame []byte
}

// NewRelay is a live share owned by ownerID with members by seat (1..8).
func NewRelay(shareID, ownerID string, members map[uint8]string) *Relay {
	m := make(map[uint8]string, len(members))
	for s, u := range members {
		m[s] = u
	}
	return &Relay{shareID: shareID, ownerID: ownerID, members: m,
		levels: map[uint8]wire.Level{}, verified: map[uint8]bool{}, conns: map[uint8]*conn{}, now: time.Now}
}

// SetClock replaces the clock the rate limits read (rule 6); nil restores
// time.Now. Each Send reads it once, as the hub reads its clock per frame.
func (r *Relay) SetClock(now func() time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if now == nil {
		now = time.Now
	}
	r.now = now
}

// Dial is the DialFunc of userID: the owner gets seat 0, a member their
// seat. Anyone else, a kicked member, or any dial after the share ended gets
// a 404 *api.Error like the server. A second dial of the same user replaces
// the first connection (4102, no srv_peer left).
func (r *Relay) Dial(userID string) share.DialFunc {
	return func(ctx context.Context, shareID string) (share.Conn, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		r.mu.Lock()
		defer r.mu.Unlock()
		seat, ok := r.seatOf(userID)
		if !ok || r.ended || shareID != r.shareID {
			return nil, &api.Error{Status: http.StatusNotFound, Code: "not_found", Message: "workspace share not found", Details: json.RawMessage(`{}`)}
		}
		if old := r.conns[seat]; old != nil {
			old.end(websocket.CloseError{Code: websocket.StatusCode(share.CloseReplaced), Reason: "replaced"})
		}
		c := &conn{r: r, seat: seat, user: userID, recv: make(chan []byte, inboxSize), done: make(chan struct{})}
		r.conns[seat] = c
		seats := []wire.SeatInfo{}
		for _, s := range r.connectedSeats() {
			seats = append(seats, wire.SeatInfo{Seat: s, UserID: r.conns[s].user})
		}
		r.enqueueServer(c, wire.KindSrvHello, wire.SrvHello{ShareID: r.shareID, Seat: seat, OwnerID: r.ownerID, Seats: seats, MaxFrame: wire.MaxFrame})
		if seat != wire.SeatOwner {
			r.peer(seat, userID, wire.PeerJoined)
		}
		return c, nil
	}
}

// SetLevels replaces the level table and verified marks, as the owner's
// ctl_levels does; a seat that had no level and gets one is sent the cached
// keyframe group.
func (r *Relay) SetLevels(levels map[uint8]wire.Level, verified map[uint8]bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.setLevels(levels, verified)
}

// Tamper installs fn over every later delivery (relayed and server frames,
// not Inject): it returns the frame to deliver, or nil to drop it. nil
// removes it.
func (r *Relay) Tamper(fn func(from, to uint8, frame []byte) []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tamper = fn
}

// Inject queues frame for seat to's connection as is, past every rule.
func (r *Relay) Inject(to uint8, frame []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if c := r.conns[to]; c != nil {
		r.queue = append(r.queue, delivery{to: c, frame: append([]byte(nil), frame...)})
	}
}

// Drop cuts seat's connection without a close code (ErrDropped), as a lost
// network would; the owner hears srv_peer left.
func (r *Relay) Drop(seat uint8) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if c := r.conns[seat]; c != nil {
		r.closeConn(c, ErrDropped)
	}
}

// End ends the share (the REST DELETE or the owner timeout): every
// connection closes 4100 and later dials get 404.
func (r *Relay) End() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.endAll()
}

// Pump delivers every queued frame to its connection's Recv, in order, and
// returns how many it delivered (frames for closed connections are dropped).
func (r *Relay) Pump() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for len(r.queue) > 0 {
		d := r.queue[0]
		r.queue = r.queue[1:]
		if d.to.isDone() {
			continue
		}
		select {
		case d.to.recv <- d.frame:
			n++
		default:
			panic(fmt.Sprintf("sharetest: seat %d inbox is full (%d frames); drain Recv between pumps", d.to.seat, inboxSize))
		}
	}
	return n
}

// Levels is a copy of the level table (seats without a level are absent).
func (r *Relay) Levels() map[uint8]wire.Level {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[uint8]wire.Level, len(r.levels))
	for s, l := range r.levels {
		out[s] = l
	}
	return out
}

// Verified is a copy of the verified marks.
func (r *Relay) Verified() map[uint8]bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[uint8]bool, len(r.verified))
	for s, v := range r.verified {
		out[s] = v
	}
	return out
}

// Attention counts the owner's ctl_attention frames.
func (r *Relay) Attention() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.attention
}

// Ended reports whether the share ended.
func (r *Relay) Ended() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ended
}

// Connected reports whether seat has a live connection.
func (r *Relay) Connected(seat uint8) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.conns[seat] != nil
}

// ---- routing (r.mu held) ----

func (r *Relay) seatOf(userID string) (uint8, bool) {
	if userID == r.ownerID {
		return wire.SeatOwner, true
	}
	for s, u := range r.members {
		if u == userID {
			return s, true
		}
	}
	return 0, false
}

func (r *Relay) connectedSeats() []uint8 {
	out := make([]uint8, 0, len(r.conns))
	for s := range r.conns {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// deliver queues frame (from seat from) for c, through the tamper hook.
func (r *Relay) deliver(c *conn, from uint8, frame []byte) {
	f := append([]byte(nil), frame...)
	if r.tamper != nil {
		if f = r.tamper(from, c.seat, f); f == nil {
			return
		}
	}
	r.queue = append(r.queue, delivery{to: c, frame: f})
}

func (r *Relay) enqueueServer(c *conn, kind wire.Kind, body any) {
	p, err := json.Marshal(body)
	if err != nil {
		panic(err)
	}
	h := wire.Header{Kind: kind, From: wire.SeatServer, To: c.seat, Len: uint32(len(p))}
	b := h.Marshal()
	r.deliver(c, wire.SeatServer, append(b[:], p...))
}

// peer tells the owner (if connected) that seat joined or left.
func (r *Relay) peer(seat uint8, userID, state string) {
	if o := r.conns[wire.SeatOwner]; o != nil {
		r.enqueueServer(o, wire.KindSrvPeer, wire.SrvPeer{Seat: seat, UserID: userID, State: state})
	}
}

// receive is rules 1-4 for one frame c sent.
func (r *Relay) receive(c *conn, frame []byte) {
	h, _, err := wire.Parse(frame)
	if err != nil || h.From != c.seat {
		r.closeConn(c, websocket.CloseError{Code: websocket.StatusCode(share.CloseProtocol), Reason: "bad frame"})
		return
	}
	switch r.withinLimits(c, h.Kind, len(frame)) {
	case limitDrop:
		return
	case limitClose:
		r.closeConn(c, websocket.CloseError{Code: websocket.StatusCode(share.CloseRateLimited), Reason: "rate_limited"})
		return
	}
	if !h.Kind.Valid() {
		return
	}
	if h.Kind.Class() == wire.ClassServer {
		if c.seat == wire.SeatOwner {
			r.control(h.Kind, frame[wire.HeaderLen:])
		}
		return
	}
	if c.seat == wire.SeatOwner {
		r.fromOwner(h, frame)
		return
	}
	if h.To != wire.SeatOwner || !r.levels[c.seat].Allows(h.Kind) {
		return
	}
	if o := r.conns[wire.SeatOwner]; o != nil {
		r.deliver(o, c.seat, frame)
	}
}

func (r *Relay) fromOwner(h wire.Header, frame []byte) {
	switch {
	case h.To == wire.SeatAll:
		r.cacheFrame(h, frame)
		for _, s := range r.connectedSeats() {
			if s != wire.SeatOwner && r.levels[s] != wire.LevelNone {
				r.deliver(r.conns[s], wire.SeatOwner, frame)
			}
		}
	case h.To >= 1 && h.To <= wire.MaxSeat:
		if c := r.conns[h.To]; c != nil {
			r.deliver(c, wire.SeatOwner, frame)
		}
	}
}

type limitResult int

const (
	limitOK limitResult = iota
	limitDrop
	limitClose
)

// rate is one connection's rule 6 counters.
type rate struct {
	sec, min                int64
	termInput, docOp, bytes int
	prop                    int
	overSec                 int64
	overRun                 int
	started                 bool
}

// withinLimits is rule 6 for one frame of c (1-second windows; prop_submit
// per minute): over a limit the frame is dropped, and the third second in a
// row with a drop closes 4008 (a second with no drop starts the count again).
func (r *Relay) withinLimits(c *conn, kind wire.Kind, size int) limitResult {
	ms := r.now().UnixMilli()
	sec, min := floorDiv(ms, 1000), floorDiv(ms, 60_000) // as the hub's Math.floor
	rt := &c.rate
	if !rt.started {
		*rt = rate{sec: sec, min: min, overSec: sec - 2, started: true} // overSec: no over second yet
	}
	if rt.sec != sec {
		rt.sec, rt.termInput, rt.docOp, rt.bytes = sec, 0, 0, 0
	}
	if rt.min != min {
		rt.min, rt.prop = min, 0
	}
	over := false
	switch {
	case c.seat == wire.SeatOwner:
		rt.bytes += size
		over = rt.bytes > ownerBytesPerSec
	case kind == wire.KindTermInput:
		rt.termInput++
		over = rt.termInput > termInputPerSec
	case kind == wire.KindDocOp:
		rt.docOp++
		over = rt.docOp > docOpPerSec
	case kind == wire.KindPropSubmit:
		rt.prop++
		over = rt.prop > propPerMin
	}
	if !over {
		return limitOK
	}
	if rt.overSec != sec {
		if rt.overSec == sec-1 {
			rt.overRun++
		} else {
			rt.overRun = 1
		}
		rt.overSec = sec
	}
	if rt.overRun >= overSeconds {
		return limitClose
	}
	return limitDrop
}

func floorDiv(a, b int64) int64 {
	q := a / b
	if (a%b != 0) && ((a < 0) != (b < 0)) {
		q--
	}
	return q
}

// cacheFrame is rule 4: a term_key with FlagGroupStart starts the group,
// later term_key / term_out join it; past 8 MiB the cache waits for the next
// group start.
func (r *Relay) cacheFrame(h wire.Header, frame []byte) {
	f := append([]byte(nil), frame...)
	switch {
	case h.Kind == wire.KindTermKey && h.Flags&wire.FlagGroupStart != 0:
		r.cache, r.cacheSize, r.cacheOn = [][]byte{f}, len(f), true
	case (h.Kind == wire.KindTermKey || h.Kind == wire.KindTermOut) && r.cacheOn:
		r.cache = append(r.cache, f)
		r.cacheSize += len(f)
	default:
		return
	}
	if r.cacheSize > cacheCap {
		r.cache, r.cacheSize, r.cacheOn = nil, 0, false
	}
}

// control is rule 2: the owner's ctl_* frames, consumed here.
func (r *Relay) control(kind wire.Kind, body []byte) {
	switch kind {
	case wire.KindCtlLevels:
		var m struct {
			Levels   map[string]string `json:"levels"`
			Verified map[string]bool   `json:"verified"`
		}
		if json.Unmarshal(body, &m) != nil || m.Levels == nil {
			return
		}
		levels := map[uint8]wire.Level{}
		for k, v := range m.Levels {
			s, ok := seatKey(k)
			l, lok := wire.ParseLevel(v)
			if !ok || !lok {
				return // malformed table: the whole frame is ignored
			}
			levels[s] = l
		}
		verified := map[uint8]bool{}
		for k, v := range m.Verified {
			s, ok := seatKey(k)
			if !ok {
				return
			}
			verified[s] = v
		}
		r.setLevels(levels, verified)
	case wire.KindCtlKick:
		var m struct {
			Seat *int `json:"seat"`
		}
		if json.Unmarshal(body, &m) != nil || m.Seat == nil || *m.Seat < 1 || *m.Seat > int(wire.MaxSeat) {
			return
		}
		seat := uint8(*m.Seat)
		delete(r.members, seat)
		delete(r.levels, seat)
		delete(r.verified, seat)
		if c := r.conns[seat]; c != nil {
			r.closeConn(c, websocket.CloseError{Code: websocket.StatusCode(share.CloseKicked), Reason: "kicked"})
		}
	case wire.KindCtlEnd:
		r.endAll()
	case wire.KindCtlAttention:
		r.attention++
	}
}

// seatKey parses a ctl_levels seat key: "1".."8".
func seatKey(k string) (uint8, bool) {
	if len(k) != 1 {
		return 0, false
	}
	n, err := strconv.Atoi(k)
	if err != nil || n < 1 || n > int(wire.MaxSeat) {
		return 0, false
	}
	return uint8(n), true
}

func (r *Relay) setLevels(levels map[uint8]wire.Level, verified map[uint8]bool) {
	before := r.levels
	r.levels = map[uint8]wire.Level{}
	for s, l := range levels {
		if l != wire.LevelNone {
			r.levels[s] = l
		}
	}
	r.verified = map[uint8]bool{}
	for s, v := range verified {
		r.verified[s] = v
	}
	if !r.cacheOn {
		return
	}
	for _, s := range r.connectedSeats() {
		if s != wire.SeatOwner && r.levels[s] != wire.LevelNone && before[s] == wire.LevelNone {
			for _, f := range r.cache {
				r.deliver(r.conns[s], wire.SeatOwner, f)
			}
		}
	}
}

func (r *Relay) endAll() {
	r.ended = true
	r.cache, r.cacheSize, r.cacheOn = nil, 0, false
	for _, s := range r.connectedSeats() {
		r.conns[s].end(websocket.CloseError{Code: websocket.StatusCode(share.CloseEnded), Reason: "share_ended"})
		delete(r.conns, s)
	}
}

// closeConn ends c with err and, for a participant's live connection, tells
// the owner it left.
func (r *Relay) closeConn(c *conn, err error) {
	if c.isDone() {
		return
	}
	c.end(err)
	if r.conns[c.seat] != c {
		return
	}
	delete(r.conns, c.seat)
	if c.seat != wire.SeatOwner && !r.ended {
		r.peer(c.seat, c.user, wire.PeerLeft)
	}
}

// ---- conn ----

type conn struct {
	r    *Relay
	seat uint8
	user string
	recv chan []byte
	done chan struct{}
	err  error // set once, under r.mu
	rate rate  // rule 6 counters, under r.mu
}

// end closes the conn with err (r.mu held).
func (c *conn) end(err error) {
	if c.isDone() {
		return
	}
	c.err = err
	close(c.done)
	close(c.recv)
}

func (c *conn) isDone() bool {
	select {
	case <-c.done:
		return true
	default:
		return false
	}
}

func (c *conn) Send(frame []byte) error {
	c.r.mu.Lock()
	defer c.r.mu.Unlock()
	if c.isDone() {
		return c.err
	}
	c.r.receive(c, frame)
	return nil
}

func (c *conn) Recv() <-chan []byte   { return c.recv }
func (c *conn) Done() <-chan struct{} { return c.done }

func (c *conn) Err() error {
	c.r.mu.Lock()
	defer c.r.mu.Unlock()
	return c.err
}

func (c *conn) Close(code int, reason string) {
	c.r.mu.Lock()
	defer c.r.mu.Unlock()
	c.r.closeConn(c, websocket.CloseError{Code: websocket.StatusCode(code), Reason: reason})
}
