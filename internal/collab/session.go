package collab

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/realtime"
)

// Options opens a Session.
type Options struct {
	API   *api.Client
	DocID string
	// OnEvent gets every Event in order, on the session's own goroutine (never
	// while a Session lock is held). A tool hands them to the screen with
	// ctx.Post.Post(...). nil = no events.
	OnEvent func(Event)
	// Dial opens the document socket; nil = the real one
	// (realtime.DialConn with ReadLimit). Tools pass ctx.CollabDial.
	Dial func(ctx context.Context, url string, h http.Header) (realtime.Conn, error)
}

// Presence is one person's cursor, selection and drag (collab.md 3). In
// SetPresence a nil field is "not changed"; User is ignored there.
type Presence struct {
	User      string
	Cursor    json.RawMessage
	Dragging  json.RawMessage
	Selection []string
}

// Event is what a Session reports through Options.OnEvent.
type Event interface{ isEvent() }

// EventState: the socket went live (after the server's hello) or dropped.
// Err is why it dropped; a session that stopped for good (kicked, 4001,
// 4003, a 404/401/403 upgrade) reports its last EventState with that error
// and does not reconnect.
type EventState struct {
	Connected bool
	Err       error
}

// EventChanged: the view changed. Remote = from the server (someone else's
// op, a catch-up, a reset); Author is that op's author (user ID), "" for my
// own edits.
type EventChanged struct {
	Changes []Change
	Remote  bool
	Author  string
}

// EventRejected: the server (or the local frame check) refused my edit; its
// ops are gone from the view and Pending. Detail is reject.detail ("" when
// absent). Code "forbidden" is handled like "locked".
type EventRejected struct {
	ClientOpID, Code, Detail string
	Ops                      []Op
}

// EventOverwritten: a value I wrote was replaced by someone else's later op
// (By = its author). Mine is my value, kept for "restore my text".
type EventOverwritten struct {
	ID, Field string
	Mine      json.RawMessage
	By        string
}

// EventPresence: the other people's presence (by user ID).
type EventPresence struct{ Peers []Presence }

// EventKicked: the server closed the document for me (left, kicked, banned,
// space_deleted, doc_deleted). Pending are my unsent edits, kept; the session
// does not reconnect.
type EventKicked struct {
	Reason  string
	Pending []Op
}

func (EventState) isEvent()       {}
func (EventChanged) isEvent()     {}
func (EventRejected) isEvent()    {}
func (EventOverwritten) isEvent() {}
func (EventPresence) isEvent()    {}
func (EventKicked) isEvent()      {}

// Session close codes (collab.md 4).
const (
	CloseUnauthorized     = 4001
	CloseAccountSuspended = 4003
	CloseRateLimited      = 4008
	CloseKicked           = 4100
)

// ErrKicked is the last EventState's error after EventKicked.
var ErrKicked = errors.New("collab: kicked from the document")

const (
	presenceInterval = 100 * time.Millisecond // presence <= 10 frames a second
	opFramesPerSec   = 20                     // op frames <= 20 a second (collab.md 4)
	writeTimeout     = 10 * time.Second
	rejectRetry      = time.Second // a rate_limited reject is sent again after this
)

// test seams (SetBackoffForTest; the package tests shorten rateLimitedDelay)
var (
	seamMu           sync.Mutex
	backoffRand      func() float64
	rateLimitedDelay = 10 * time.Second // close 4008: "10초 뒤 재접속"
)

// SetBackoffForTest makes later Sessions draw their reconnect jitter from rnd
// (0 = reconnect at once) and returns the undo. Tests only.
func SetBackoffForTest(rnd func() float64) (restore func()) {
	seamMu.Lock()
	prev := backoffRand
	backoffRand = rnd
	seamMu.Unlock()
	return func() {
		seamMu.Lock()
		backoffRand = prev
		seamMu.Unlock()
	}
}

type fkey struct{ id, field string }

// me marks a field whose last confirmed value is my own (acknowledged) write
// (user IDs are never this; "" = writer unknown, e.g. after a snapshot).
const me = "\x00me"

type pendingOp struct {
	id      string
	ops     []Op
	sent    bool      // on the current connection
	retryAt time.Time // after a rate_limited reject
}

// Session is one open document: the confirmed state from the server plus my
// edits not yet confirmed (View), reconnecting with backoff and resending
// them with the same client_op_id. Safe for concurrent use.
type Session struct {
	o      Options
	dial   func(ctx context.Context, url string, h http.Header) (realtime.Conn, error)
	ctx    context.Context
	cancel context.CancelFunc
	wake   chan struct{}
	done   chan struct{}
	once   sync.Once

	mu        sync.Mutex
	confirmed *Doc
	view      *Doc
	pending   []*pendingOp
	writers   map[fkey]string
	peers     map[string]Presence
	presence  *Presence
	presSent  time.Time
	opSent    []time.Time
	live      bool
	synced    bool // a hello answered at least once (since = confirmed seq from then on)
	prefix    string
	counter   int

	evMu     sync.Mutex
	events   []Event
	evWake   chan struct{}
	evClosed bool
}

// Open starts the session: it dials at once and keeps the socket up until
// Close, a kick or a terminal close code. ctx bounds the whole session.
func Open(ctx context.Context, o Options) *Session {
	cctx, cancel := context.WithCancel(ctx)
	s := &Session{
		o:         o,
		dial:      o.Dial,
		ctx:       cctx,
		cancel:    cancel,
		wake:      make(chan struct{}, 1),
		done:      make(chan struct{}),
		confirmed: NewDoc(Snapshot{}),
		view:      NewDoc(Snapshot{}),
		writers:   map[fkey]string{},
		peers:     map[string]Presence{},
		evWake:    make(chan struct{}, 1),
		prefix:    randomPrefix(),
	}
	if s.dial == nil {
		s.dial = func(ctx context.Context, url string, h http.Header) (realtime.Conn, error) {
			return realtime.DialConn(ctx, url, h, ReadLimit)
		}
	}
	go s.dispatch()
	go s.run()
	return s
}

func randomPrefix() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// ---- public API ----

// Submit applies ops to the view at once (optimistic) and queues them as one
// op frame; it returns that frame's client_op_id. A frame the server would
// refuse on shape or size (collab.md 3.3 step 1, 4) is refused here at once
// with EventRejected (code invalid / too_large).
func (s *Session) Submit(ops []Op) (clientOpID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.counter++
	id := s.prefix + "-" + strconv.Itoa(s.counter)
	ops = cloneOps(ops)
	code := ""
	if err := CheckOps(ops); err != nil {
		code = "invalid"
	} else if b, err := encodeOp(id, 0, ops); err != nil {
		code = "invalid"
	} else if len(b)+20 > MaxClientFrame { // + room for a longer base_seq
		code = "too_large"
	}
	if code != "" {
		s.emit(EventRejected{ClientOpID: id, Code: code, Ops: ops})
		return id
	}
	s.pending = append(s.pending, &pendingOp{id: id, ops: ops})
	s.emit(EventChanged{Changes: s.view.Apply(s.view.Seq(), ops)})
	s.poke()
	return id
}

// SetPresence sends my presence, at most 10 frames a second (the latest wins).
func (s *Session) SetPresence(p Presence) {
	s.mu.Lock()
	if s.presence == nil {
		s.presence = &Presence{}
	}
	if p.Cursor != nil {
		s.presence.Cursor = append(json.RawMessage(nil), p.Cursor...)
	}
	if p.Dragging != nil {
		s.presence.Dragging = append(json.RawMessage(nil), p.Dragging...)
	}
	if p.Selection != nil {
		s.presence.Selection = append([]string{}, p.Selection...)
	}
	s.mu.Unlock()
	s.poke()
}

// View is the confirmed state with my unconfirmed edits on top; a copy.
func (s *Session) View() *Doc {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.view.Clone()
}

// Peers are the other people's last presence, by user ID.
func (s *Session) Peers() []Presence {
	s.mu.Lock()
	defer s.mu.Unlock()
	return sortedPeers(s.peers)
}

// Pending are my edits the server has not confirmed yet, in order.
func (s *Session) Pending() []Op {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pendingOps()
}

func (s *Session) pendingOps() []Op {
	var out []Op
	for _, p := range s.pending {
		out = append(out, cloneOps(p.ops)...)
	}
	return out
}

// Close stops the session and its socket; no events are delivered after it
// returns. Pending edits are kept (Pending still answers).
func (s *Session) Close() {
	s.once.Do(func() {
		s.cancel()
		<-s.done
		s.evMu.Lock()
		s.evClosed = true
		s.events = nil
		s.evMu.Unlock()
		s.notifyEv()
	})
}

// ---- events ----

// emit queues e for OnEvent. Callers emit while holding s.mu so events keep
// the order of the state changes (emit only takes evMu; the dispatcher never
// holds s.mu).
func (s *Session) emit(e Event) {
	s.evMu.Lock()
	if s.evClosed {
		s.evMu.Unlock()
		return
	}
	s.events = append(s.events, e)
	s.evMu.Unlock()
	s.notifyEv()
}

func (s *Session) notifyEv() {
	select {
	case s.evWake <- struct{}{}:
	default:
	}
}

func (s *Session) dispatch() {
	for range s.evWake {
		for {
			s.evMu.Lock()
			if s.evClosed {
				s.evMu.Unlock()
				return
			}
			if len(s.events) == 0 {
				s.evMu.Unlock()
				break
			}
			e := s.events[0]
			s.events = s.events[1:]
			s.evMu.Unlock()
			if s.o.OnEvent != nil {
				s.o.OnEvent(e)
			}
		}
	}
}

func (s *Session) poke() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// ---- connection loop ----

// errStop ends the loop for good (Close, kick, terminal close code).
type errStop struct{ err error }

func (e errStop) Error() string { return e.err.Error() }

func (s *Session) run() {
	defer close(s.done)
	seamMu.Lock()
	rnd := backoffRand
	seamMu.Unlock()
	bo := realtime.NewBackoff(rnd)
	for {
		if s.ctx.Err() != nil {
			return
		}
		url := ""
		var hdr http.Header
		if s.o.API != nil {
			url, hdr = s.o.API.DocSocketURL(s.o.DocID), s.o.API.AuthHeader()
		}
		conn, err := s.dial(s.ctx, url, hdr)
		var wait time.Duration
		if err != nil {
			if s.ctx.Err() != nil {
				return
			}
			if terminalDialErr(err) {
				s.emit(EventState{Err: err})
				return
			}
			s.emit(EventState{Err: err})
			wait = bo.Next()
			var ae *api.Error
			if errors.As(err, &ae) && ae.RetryAfter > wait {
				wait = ae.RetryAfter
			}
		} else {
			wentLive, err := s.serve(conn)
			if s.ctx.Err() != nil {
				return
			}
			var stop errStop
			if errors.As(err, &stop) {
				s.emit(EventState{Err: stop.err})
				return
			}
			s.emit(EventState{Err: err})
			if wentLive {
				bo.Reset()
			}
			wait = bo.Next()
			if websocket.CloseStatus(err) == CloseRateLimited {
				seamMu.Lock()
				wait = max(wait, rateLimitedDelay)
				seamMu.Unlock()
			}
		}
		if wait > 0 {
			t := time.NewTimer(wait)
			select {
			case <-s.ctx.Done():
				t.Stop()
				return
			case <-t.C:
			}
		}
	}
}

// terminalDialErr: an upgrade the server refused for good (no doc / not a
// member 404, session gone 401, suspended 403).
func terminalDialErr(err error) bool {
	var ae *api.Error
	if !errors.As(err, &ae) {
		return false
	}
	return ae.Status == http.StatusNotFound || ae.Status == http.StatusUnauthorized || ae.Status == http.StatusForbidden
}

type readResult struct {
	b   []byte
	err error
}

// serve runs one connection until it ends; wentLive = the server's hello came.
func (s *Session) serve(conn realtime.Conn) (wentLive bool, err error) {
	cctx, cancel := context.WithCancel(s.ctx)
	defer cancel()
	reads := make(chan readResult, 16)
	go func() {
		for {
			b, err := conn.Read(cctx)
			select {
			case reads <- readResult{b, err}:
			case <-cctx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()
	defer func() {
		s.mu.Lock()
		s.live = false
		for _, p := range s.pending {
			p.sent = false
		}
		clear(s.peers)
		s.mu.Unlock()
	}()

	s.mu.Lock()
	var since *int64
	if s.synced {
		v := s.confirmed.Seq()
		since = &v
	}
	s.mu.Unlock()
	if err := s.write(conn, encodeHello(since)); err != nil {
		conn.Close(int(websocket.StatusNormalClosure), "")
		return false, err
	}

	timer := time.NewTimer(time.Hour)
	timer.Stop()
	defer timer.Stop()
	for {
		select {
		case <-s.ctx.Done():
			conn.Close(int(websocket.StatusNormalClosure), "")
			return wentLive, s.ctx.Err()
		case r := <-reads:
			if r.err != nil {
				conn.Close(int(websocket.StatusNormalClosure), "")
				switch websocket.CloseStatus(r.err) {
				case CloseUnauthorized, CloseAccountSuspended:
					return wentLive, errStop{r.err}
				case CloseKicked:
					s.kicked("")
					return wentLive, errStop{ErrKicked}
				}
				return wentLive, r.err
			}
			f, err := decodeFrame(r.b)
			if err != nil {
				continue // not JSON: ignore (collab.md 3.4)
			}
			if f.T == "kicked" {
				s.kicked(f.Reason)
				conn.Close(int(websocket.StatusNormalClosure), "")
				return wentLive, errStop{ErrKicked}
			}
			if f.T == "hello" {
				wentLive = true
			}
			s.handle(f)
		case <-s.wake:
		case <-timer.C:
		}
		next, err := s.flush(conn)
		if err != nil {
			conn.Close(int(websocket.StatusNormalClosure), "")
			return wentLive, err
		}
		timer.Stop()
		if !next.IsZero() {
			timer.Reset(max(time.Until(next), time.Millisecond))
		}
	}
}

func (s *Session) write(conn realtime.Conn, b []byte) error {
	ctx, cancel := context.WithTimeout(s.ctx, writeTimeout)
	defer cancel()
	return conn.Write(ctx, b)
}

func (s *Session) kicked(reason string) {
	s.mu.Lock()
	s.emit(EventKicked{Reason: reason, Pending: s.pendingOps()})
	s.mu.Unlock()
}

// flush sends what is due: unsent op frames (<= 20 a second) and the latest
// presence (<= 10 a second). It returns when to look again (zero = nothing
// waits).
func (s *Session) flush(conn realtime.Conn) (time.Time, error) {
	now := time.Now()
	var frames [][]byte
	var next time.Time
	later := func(t time.Time) {
		if next.IsZero() || t.Before(next) {
			next = t
		}
	}
	s.mu.Lock()
	if s.live {
		for _, p := range s.pending {
			if p.sent {
				continue
			}
			if now.Before(p.retryAt) {
				later(p.retryAt)
				continue
			}
			if n := len(s.opSent); n >= opFramesPerSec {
				if at := s.opSent[n-opFramesPerSec].Add(time.Second); now.Before(at) {
					later(at)
					break
				}
			}
			b, err := encodeOp(p.id, s.confirmed.Seq(), p.ops)
			if err != nil {
				continue
			}
			p.sent = true
			s.opSent = append(s.opSent, now)
			if len(s.opSent) > opFramesPerSec {
				s.opSent = s.opSent[len(s.opSent)-opFramesPerSec:]
			}
			frames = append(frames, b)
		}
		if s.presence != nil {
			if at := s.presSent.Add(presenceInterval); now.Before(at) {
				later(at)
			} else {
				frames = append(frames, encodePresence(*s.presence))
				s.presence = nil
				s.presSent = now
			}
		}
	}
	s.mu.Unlock()
	for _, b := range frames {
		if err := s.write(conn, b); err != nil {
			return time.Time{}, err
		}
	}
	return next, nil
}

// ---- frames ----

func (s *Session) handle(f frameIn) {
	switch f.T {
	case "hello":
		s.onHello(f)
	case "op":
		var ops []Op
		if json.Unmarshal(f.Ops, &ops) != nil {
			return
		}
		s.mu.Lock()
		evs := s.applyRemote(f.Seq, f.Author, ops)
		s.rebuild()
		s.emitAll(evs)
		s.mu.Unlock()
	case "ack":
		s.onAck(f)
	case "reject":
		s.onReject(f)
	case "presence":
		s.mu.Lock()
		s.peers[f.User] = Presence{User: f.User, Cursor: f.Cursor, Dragging: f.Dragging, Selection: f.Selection}
		s.emit(EventPresence{Peers: sortedPeers(s.peers)})
		s.mu.Unlock()
	}
}

func (s *Session) emitAll(evs []Event) {
	for _, e := range evs {
		s.emit(e)
	}
}

func (s *Session) onHello(f frameIn) {
	var evs []Event
	s.mu.Lock()
	if len(bytes.TrimSpace(f.Snapshot)) > 0 && string(bytes.TrimSpace(f.Snapshot)) != "null" {
		snap, err := DecodeSnapshot(f.Snapshot)
		if err == nil {
			snap.Seq = f.Seq
			changes := s.confirmed.Apply(f.Seq, []Op{{Op: OpReset, Snapshot: &snap}})
			clear(s.writers)
			evs = append(evs, EventChanged{Changes: changes, Remote: true})
		}
	} else if len(f.Ops) > 0 {
		var hops []helloOp
		if json.Unmarshal(f.Ops, &hops) == nil {
			for _, h := range hops {
				evs = append(evs, s.applyRemote(h.Seq, h.Author, h.Ops)...)
			}
		}
	}
	s.synced = true
	s.live = true
	s.rebuild()
	s.emit(EventState{Connected: true})
	s.emitAll(evs)
	s.mu.Unlock()
}

// applyRemote applies one sequenced frame by author to the confirmed state
// (a frame already seen is skipped) and returns its events. s.mu held.
func (s *Session) applyRemote(seq int64, author string, ops []Op) []Event {
	if seq <= s.confirmed.Seq() {
		return nil
	}
	before := map[fkey]json.RawMessage{}
	reset := false
	for _, op := range ops {
		switch op.Op {
		case OpSet:
			k := fkey{op.ID, op.Field}
			before[k] = s.confirmed.Field(op.ID, op.Field)
		case OpPut:
			for f := range op.Fields {
				k := fkey{op.ID, f}
				before[k] = s.confirmed.Field(op.ID, f)
			}
		case OpReset:
			reset = true
		}
	}
	changes := s.confirmed.Apply(seq, ops)
	evs := []Event{EventChanged{Changes: changes, Remote: true, Author: author}}
	if reset {
		clear(s.writers)
		return evs
	}
	for _, c := range changes {
		if c.Field == "" {
			continue
		}
		k := fkey{c.ID, c.Field}
		if w, ok := s.writers[k]; ok && w == me && !s.pendingTouches(k, nil) {
			if old := before[k]; old != nil && !bytes.Equal(old, s.confirmed.Field(c.ID, c.Field)) {
				evs = append(evs, EventOverwritten{ID: c.ID, Field: c.Field, Mine: old, By: author})
			}
		}
		s.writers[k] = author
	}
	return evs
}

// written are the fields ops set, with their values (last write wins).
func written(ops []Op) ([]fkey, map[fkey]json.RawMessage) {
	var keys []fkey
	vals := map[fkey]json.RawMessage{}
	add := func(k fkey, v json.RawMessage) {
		if _, ok := vals[k]; !ok {
			keys = append(keys, k)
		}
		vals[k] = compact(v)
	}
	for _, op := range ops {
		switch op.Op {
		case OpSet:
			v := op.Value
			if len(v) == 0 {
				v = json.RawMessage("null")
			}
			add(fkey{op.ID, op.Field}, v)
		case OpPut:
			for _, f := range sortedKeys(op.Fields) {
				add(fkey{op.ID, f}, op.Fields[f])
			}
		}
	}
	return keys, vals
}

// pendingTouches: a pending op other than skip writes field k. s.mu held.
func (s *Session) pendingTouches(k fkey, skip *pendingOp) bool {
	for _, p := range s.pending {
		if p == skip {
			continue
		}
		if _, vals := written(p.ops); vals[k] != nil {
			return true
		}
	}
	return false
}

func (s *Session) take(id string) *pendingOp {
	for i, p := range s.pending {
		if p.id == id {
			s.pending = slices.Delete(s.pending, i, i+1)
			return p
		}
	}
	return nil
}

func (s *Session) onAck(f frameIn) {
	var evs []Event
	s.mu.Lock()
	p := s.take(f.ClientOpID)
	if p == nil { // a duplicate ack (resend after a reconnect): handled once
		s.mu.Unlock()
		return
	}
	if f.Seq > s.confirmed.Seq() {
		for _, c := range s.confirmed.Apply(f.Seq, p.ops) {
			if c.Field != "" {
				s.writers[fkey{c.ID, c.Field}] = me
			}
		}
	} else {
		// already in the confirmed state (the catch-up after a reconnect carried
		// it, without its client_op_id): was it overwritten since?
		keys, vals := written(p.ops)
		var changed []Change
		for _, k := range keys {
			if s.pendingTouches(k, nil) {
				continue
			}
			cur := s.confirmed.Field(k.id, k.field)
			if cur == nil {
				continue
			}
			if bytes.Equal(cur, vals[k]) {
				s.writers[k] = me
				continue
			}
			if by := s.writers[k]; by != me { // "" = unknown (a snapshot replaced the log)
				evs = append(evs, EventOverwritten{ID: k.id, Field: k.field, Mine: vals[k], By: by})
				changed = append(changed, Change{ID: k.id, Field: k.field})
			}
		}
		if len(changed) > 0 {
			by := evs[0].(EventOverwritten).By
			evs = append([]Event{EventChanged{Changes: changed, Remote: true, Author: by}}, evs...)
		}
	}
	s.rebuild()
	s.emitAll(evs)
	s.mu.Unlock()
}

func (s *Session) onReject(f frameIn) {
	s.mu.Lock()
	var p *pendingOp
	for _, q := range s.pending {
		if q.id == f.ClientOpID {
			p = q
		}
	}
	if p == nil {
		s.mu.Unlock()
		return
	}
	if f.Code == "rate_limited" { // not the edit's fault: send it again shortly
		p.sent = false
		p.retryAt = time.Now().Add(rejectRetry)
		s.mu.Unlock()
		s.poke()
		return
	}
	s.take(p.id)
	s.rebuild()
	s.emit(EventRejected{ClientOpID: p.id, Code: f.Code, Detail: f.Detail, Ops: cloneOps(p.ops)})
	s.emit(EventChanged{Changes: touched(p.ops, s.view)})
	s.mu.Unlock()
}

// touched lists what ops touched (objects and fields) for an EventChanged.
func touched(ops []Op, after *Doc) []Change {
	var out []Change
	seen := map[fkey]bool{}
	add := func(id, field string) {
		k := fkey{id, field}
		if seen[k] {
			return
		}
		seen[k] = true
		o, ok := after.Object(id)
		out = append(out, Change{ID: id, Field: field, Removed: !ok || o.Deleted})
	}
	for _, op := range ops {
		switch op.Op {
		case OpSet:
			add(op.ID, op.Field)
		case OpPut:
			add(op.ID, "")
			for _, f := range sortedKeys(op.Fields) {
				add(op.ID, f)
			}
		case OpDel, OpUndel:
			add(op.ID, "")
		}
	}
	return out
}

// rebuild sets view = confirmed + pending. s.mu held.
func (s *Session) rebuild() {
	v := s.confirmed.Clone()
	for _, p := range s.pending {
		v.Apply(v.Seq(), p.ops)
	}
	s.view = v
}

func cloneOps(ops []Op) []Op {
	out := make([]Op, len(ops))
	for i, op := range ops {
		c := op
		c.Value = append(json.RawMessage(nil), op.Value...)
		if op.Value == nil {
			c.Value = nil
		}
		if op.Fields != nil {
			c.Fields = make(map[string]json.RawMessage, len(op.Fields))
			for k, v := range op.Fields {
				c.Fields[k] = append(json.RawMessage(nil), v...)
			}
		}
		if op.Snapshot != nil {
			snap := NewDoc(*op.Snapshot).Snapshot()
			c.Snapshot = &snap
		}
		out[i] = c
	}
	return out
}
