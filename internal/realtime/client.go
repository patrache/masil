// Package realtime is the WebSocket client shared by the TUI and the daemon (CA 4.4).
package realtime

import (
	"context"
	"errors"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/patrache/masil/internal/api"
)

// Mode is the connection mode.
type Mode string

// Connection modes.
const (
	ModeFull   Mode = "full"
	ModeNotify Mode = "notify"
)

// ResumePoint is what the client sends in `resume`.
type ResumePoint struct {
	Epoch        *string // nil => first connect
	LastSeq      *int64
	UseDeviceAck bool // daemon restart without disk state
}

// Phase is the connection phase.
type Phase int

// Connection phases.
const (
	PhaseIdle       Phase = iota
	PhaseConnecting       // dialing
	PhaseSyncing          // got hello, sent resume, waiting for ready (UI: "동기화 중")
	PhaseLive             // ready received
	PhaseBackoff          // waiting before reconnect
	PhaseStopped          // terminal: unauthorized/suspended/upgrade_required/ctx done
)

// State is reported through Options.OnState.
type State struct {
	Phase       Phase
	Attempt     int       // consecutive failures (n in backoff formula)
	NextRetry   time.Time // PhaseBackoff only
	LastClose   int       // last close code (0 if none)
	Err         error
	Epoch       string
	Seq         int64 // last processed seq
	TUIOnDevice *bool // notify mode: hello.local_tui_connected / device.tui_state
}

// Options configures a Client.
type Options struct {
	API      *api.Client // base URL, token, UA
	DeviceID string
	Mode     Mode
	Resume   ResumePoint
	OnEvent  func(Event) // called on the client goroutine, in order; must not block long
	OnState  func(State)
	Logger   *slog.Logger
	Now      func() time.Time
	Rand     func() float64                                                     // jitter, tests inject
	Dial     func(ctx context.Context, url string, h http.Header) (Conn, error) // nil => coder/websocket
}

// Terminal errors returned by Run.
var (
	ErrUnauthorized    = errors.New("realtime: unauthorized (4001/401)")
	ErrSuspended       = errors.New("realtime: account suspended (4003/403)")
	ErrUpgradeRequired = errors.New("realtime: upgrade required (4010/426)")
)

// Protocol timings (events.md 1–3, 5; CA 4.4).
const (
	helloTimeout     = 15 * time.Second  // dial -> hello; the server sends hello at once
	pongTimeout      = 10 * time.Second  // ping -> any frame
	defaultHeartbeat = 25 * time.Second  // when hello.heartbeat_interval_ms is missing
	ackInterval      = 5 * time.Second   // ack at most once per window
	typingInterval   = 3 * time.Second   // per (channel, thread)
	watchRefresh     = 120 * time.Second // server lease is 300s
	watchMax         = 50
	rateLimitedDelay = 10 * time.Second // close 4008
	replacedWindow   = 60 * time.Second // second 4009 inside this window stops the client
	dialTimeout      = 30 * time.Second
	writeTimeout     = 10 * time.Second
	closeWait        = 3 * time.Second // bound on the 1000 close handshake when stopping
)

// errReplacedTwice is State.Err after a second 4009 within 60s.
var errReplacedTwice = errors.New("realtime: replaced twice within 60s (4009)")

// Client is the connect/resume/heartbeat/reconnect loop.
//
// Run owns the socket on its own goroutine and calls OnEvent/OnState there.
// Position, MarkProcessed, Typing, SetIdle, Watch and Close are safe to call
// from any goroutine.
type Client struct {
	api      *api.Client
	deviceID string
	mode     Mode
	onEvent  func(Event)
	onState  func(State)
	log      *slog.Logger
	now      func() time.Time
	rand     func() float64
	dial     func(ctx context.Context, url string, h http.Header) (Conn, error)
	after    func(time.Duration) <-chan time.Time // time.After; same-package tests swap it

	wake chan struct{} // capacity 1: "client state changed, look again"

	st State // last reported state; Run goroutine only

	mu sync.Mutex
	// Resume position (Position). processed only moves to seqs this client
	// delivered (<= deliveredMax) in epoch.
	epoch        *string
	processed    int64
	deliveredMax int64
	deviceAck    bool // resume with use_device_ack until the first ready
	// Current connection.
	live       bool  // ready received
	switching  bool  // between hello and ready on a connection whose epoch differs from Position
	recvMax    int64 // dedupe baseline: resumed last_seq, then max logged seq received
	pendingMax int64 // marks while switching (applied at ready)
	acked      int64 // last ack sent in epoch
	ackDirty   bool
	// Client-driven state (full mode only).
	idle       bool
	watch      []string
	watchSet   bool
	watchDirty bool
	typingLast map[string]time.Time
	out        outbox
	// Lifecycle.
	closing bool
	cancel  context.CancelFunc
}

// New creates a client. Nothing happens until Run.
func New(opts Options) *Client {
	c := &Client{
		api:        opts.API,
		deviceID:   opts.DeviceID,
		mode:       opts.Mode,
		onEvent:    opts.OnEvent,
		onState:    opts.OnState,
		log:        opts.Logger,
		now:        opts.Now,
		rand:       opts.Rand,
		dial:       opts.Dial,
		after:      time.After,
		wake:       make(chan struct{}, 1),
		typingLast: map[string]time.Time{},
	}
	if c.mode == "" {
		c.mode = ModeFull
	}
	if c.log == nil {
		c.log = slog.New(slog.DiscardHandler)
	}
	if c.now == nil {
		c.now = time.Now
	}
	if c.rand == nil {
		c.rand = rand.Float64
	}
	if c.dial == nil {
		c.dial = coderDial
	}
	// use_device_ack wins over epoch/last_seq, as in encodeResume.
	switch r := opts.Resume; {
	case r.UseDeviceAck:
		c.deviceAck = true
	case r.Epoch != nil:
		e := *r.Epoch
		c.epoch = &e
		if r.LastSeq != nil {
			c.processed = *r.LastSeq
		}
		c.deliveredMax = c.processed
	}
	return c
}

// result is how one connection attempt ended.
type result struct {
	dialErr   error     // dial failed (maybe *api.Error from the handshake)
	code      int       // close code from the server, 0 for network/protocol/heartbeat failures
	liveSince time.Time // zero if ready never arrived
}

// Run is the connect+reconnect loop; returns nil on ctx done, or one of the Err* terminal errors.
func (c *Client) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	c.mu.Lock()
	closing := c.closing
	c.cancel = cancel
	c.mu.Unlock()
	if closing {
		return c.stop(nil)
	}
	if c.api == nil {
		err := errors.New("realtime: Options.API is nil")
		c.stop(err)
		return err
	}
	var last4009 time.Time
	for {
		r := c.connectOnce(ctx)
		if ctx.Err() != nil {
			return c.stop(nil)
		}
		delay, terminal, halt := c.decide(r, &last4009)
		switch {
		case terminal != nil:
			c.log.Debug("realtime: client stopped", "reason", terminal.Error(), "close", c.st.LastClose)
			c.stop(terminal)
			return terminal
		case halt:
			c.log.Debug("realtime: client stopped", "reason", errReplacedTwice.Error())
			c.stop(errReplacedTwice)
			<-ctx.Done()
			return nil
		}
		if delay > 0 {
			c.setPhase(PhaseBackoff, nil, c.now().Add(delay))
			select {
			case <-c.after(delay):
			case <-ctx.Done():
				return c.stop(nil)
			}
		}
	}
}

// decide applies the close-code and handshake-error table (CA 4.4, events.md 1.2).
func (c *Client) decide(r result, last4009 *time.Time) (delay time.Duration, terminal error, halt bool) {
	now := c.now()
	if !r.liveSince.IsZero() && now.Sub(r.liveSince) >= liveResetAge {
		c.st.Attempt = 0
	}
	if r.dialErr != nil {
		var e *api.Error
		if errors.As(r.dialErr, &e) {
			switch {
			case e.Status == http.StatusUnauthorized:
				return 0, ErrUnauthorized, false
			case e.Status == http.StatusForbidden && e.Code == "account_suspended":
				return 0, ErrSuspended, false
			case e.Status == http.StatusUpgradeRequired:
				return 0, ErrUpgradeRequired, false
			case e.Status == http.StatusTooManyRequests && e.RetryAfter > 0:
				c.st.Attempt++
				return e.RetryAfter, nil, false
			}
		}
		c.log.Debug("realtime: dial failed", "err", r.dialErr)
		return c.backoff(), nil, false
	}
	if r.code > 0 {
		c.st.LastClose = r.code
	}
	switch r.code {
	case 1001, 4002: // going away / heartbeat timeout: reconnect at once
		c.st.Attempt = 0
		return 0, nil, false
	case 4001:
		return 0, ErrUnauthorized, false
	case 4003:
		return 0, ErrSuspended, false
	case 4010:
		return 0, ErrUpgradeRequired, false
	case 4008:
		c.st.Attempt++
		return rateLimitedDelay, nil, false
	case 4009:
		if !last4009.IsZero() && now.Sub(*last4009) < replacedWindow {
			return 0, nil, true
		}
		*last4009 = now
	}
	// 1000 from the server (we did not ask to stop), 1009, 1011, 4000, 4009,
	// network, protocol and heartbeat failures: back off.
	return c.backoff(), nil, false
}

func (c *Client) backoff() time.Duration {
	d := backoffDelay(c.st.Attempt, c.rand())
	c.st.Attempt++
	return d
}

func (c *Client) connectOnce(ctx context.Context) result {
	c.setPhase(PhaseConnecting, nil, time.Time{})
	dctx, dcancel := context.WithTimeout(ctx, dialTimeout)
	conn, err := c.dial(dctx, c.api.WebSocketURL(string(c.mode), c.deviceID), c.api.AuthHeader())
	dcancel()
	if err != nil {
		return result{dialErr: err}
	}
	s := &session{c: c, conn: conn}
	return s.run(ctx)
}

// stop reports the terminal state and returns nil (for Run's ctx-done paths).
func (c *Client) stop(err error) error {
	c.setPhase(PhaseStopped, err, time.Time{})
	return nil
}

func (c *Client) setPhase(p Phase, err error, next time.Time) {
	c.st.Phase = p
	c.st.Err = err
	c.st.NextRetry = next
	c.report()
}

func (c *Client) setTUIOnDevice(v bool) {
	c.st.TUIOnDevice = &v
}

// report fills the position fields and calls OnState.
func (c *Client) report() {
	c.mu.Lock()
	if c.epoch != nil {
		c.st.Epoch = *c.epoch
	}
	c.st.Seq = c.processed
	c.mu.Unlock()
	if c.onState != nil {
		c.onState(c.st)
	}
}

func (c *Client) emit(ev Event) {
	if c.onEvent != nil {
		c.onEvent(ev)
	}
}

func (c *Client) poke() {
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

// Position returns the current epoch + last processed seq.
func (c *Client) Position() ResumePoint {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.positionLocked()
}

func (c *Client) positionLocked() ResumePoint {
	if c.epoch != nil {
		e, s := *c.epoch, c.processed
		return ResumePoint{Epoch: &e, LastSeq: &s}
	}
	if c.deviceAck {
		return ResumePoint{UseDeviceAck: true}
	}
	return ResumePoint{}
}

// MarkProcessed records that the caller finished applying seq; acks are sent at most every 5s.
// Seqs this client never delivered are ignored.
func (c *Client) MarkProcessed(seq int64) {
	c.mu.Lock()
	if c.switching {
		if seq <= c.recvMax && seq > c.pendingMax {
			c.pendingMax = seq
		}
		c.mu.Unlock()
		return
	}
	if seq <= c.processed || seq > c.deliveredMax {
		c.mu.Unlock()
		return
	}
	c.processed = seq
	c.ackDirty = true
	c.mu.Unlock()
	c.poke()
}

// Typing sends a typing frame (full only); throttled to 1 per 3s per (channel,thread).
// Dropped while not live.
func (c *Client) Typing(channelID string, threadRootID *string) {
	if c.mode != ModeFull {
		return
	}
	now := c.now()
	key := channelID + "\x00"
	if threadRootID != nil {
		key += "#" + *threadRootID
	}
	c.mu.Lock()
	if !c.live {
		c.mu.Unlock()
		return
	}
	if last, ok := c.typingLast[key]; ok && now.Sub(last) < typingInterval {
		c.mu.Unlock()
		return
	}
	if len(c.typingLast) >= 256 {
		for k, t := range c.typingLast {
			if now.Sub(t) >= typingInterval {
				delete(c.typingLast, k)
			}
		}
	}
	c.typingLast[key] = now
	c.out.push(kindTyping, encodeTyping(channelID, threadRootID))
	c.mu.Unlock()
	c.poke()
}

// SetIdle sends idle state (full only); sends only on change, re-sent after reconnect.
// The initial value is false, like a fresh server connection.
func (c *Client) SetIdle(idle bool) {
	if c.mode != ModeFull {
		return
	}
	c.mu.Lock()
	if c.idle == idle {
		c.mu.Unlock()
		return
	}
	c.idle = idle
	if c.live {
		c.out.push(kindIdle, encodeIdle(idle))
	}
	c.mu.Unlock()
	c.poke()
}

// Watch replaces the watched topics (full only); capped at 50, re-sent every 120s and after reconnect.
// An unchanged list is not re-sent early.
func (c *Client) Watch(topics []string) {
	if c.mode != ModeFull {
		return
	}
	capped := slices.Clone(topics[:min(len(topics), watchMax)])
	if capped == nil {
		capped = []string{}
	}
	c.mu.Lock()
	if c.watchSet && slices.Equal(c.watch, capped) {
		c.mu.Unlock()
		return
	}
	c.watch, c.watchSet = capped, true
	if c.live {
		c.out.push(kindWatch, encodeWatch(capped))
		c.watchDirty = true
	}
	c.mu.Unlock()
	c.poke()
}

// Close sends 1000 and stops.
func (c *Client) Close() {
	c.mu.Lock()
	if c.closing {
		c.mu.Unlock()
		return
	}
	c.closing = true
	cancel := c.cancel
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// ---------------------------------------------------------------- one connection

type readResult struct {
	b   []byte
	err error
}

// session is one socket from dial to close; it lives on the Run goroutine.
type session struct {
	c          *Client
	conn       Conn
	connCtx    context.Context // independent of Run's ctx so stopping can still send 1000
	connCancel context.CancelFunc
	frames     chan readResult
	readerDone chan struct{}

	gotHello      bool
	helloEpoch    string
	helloSeq      int64
	firstReplayed int64 // first logged seq received before ready
	liveSince     time.Time
	interval      time.Duration

	helloT, pingT, pongT, ackT, outT, watchT <-chan time.Time
}

func (s *session) run(ctx context.Context) result {
	c := s.c
	s.connCtx, s.connCancel = context.WithCancel(context.Background())
	s.frames = make(chan readResult)
	s.readerDone = make(chan struct{})
	go s.read()
	defer s.finish()
	s.helloT = c.after(helloTimeout)
	for {
		select {
		case <-ctx.Done():
			s.closeGracefully()
			return s.result(0)
		case rr := <-s.frames:
			if rr.err != nil {
				code := int(websocket.CloseStatus(rr.err))
				if code < 0 {
					c.log.Debug("realtime: read failed", "err", rr.err)
					code = 0
				}
				return s.result(code)
			}
			if !s.handle(rr.b) {
				s.abandon(websocket.StatusProtocolError, "protocol error")
				return s.result(0)
			}
		case <-s.helloT:
			s.helloT = nil
			c.log.Debug("realtime: hello timeout")
			s.abandon(websocket.StatusGoingAway, "hello timeout")
			return s.result(0)
		case <-s.pingT:
			s.pingT = c.after(s.interval)
			if err := s.write(pingFrame); err != nil {
				s.abandon(websocket.StatusGoingAway, "write failed")
				return s.result(0)
			}
			if s.pongT == nil {
				s.pongT = c.after(pongTimeout)
			}
		case <-s.pongT:
			s.pongT = nil
			c.log.Debug("realtime: heartbeat timeout")
			s.abandon(websocket.StatusGoingAway, "heartbeat timeout")
			return s.result(0)
		case <-s.ackT:
			s.ackT = nil
			c.mu.Lock()
			if c.live && c.processed > c.acked {
				c.out.push(kindAck, encodeAck(c.processed))
				c.acked = c.processed
			}
			c.mu.Unlock()
		case <-s.outT:
			s.outT = nil
		case <-s.watchT:
			s.watchT = nil
			c.mu.Lock()
			if c.live && len(c.watch) > 0 {
				c.out.push(kindWatch, encodeWatch(c.watch))
				s.watchT = c.after(watchRefresh)
			}
			c.mu.Unlock()
		case <-c.wake:
			s.onWake()
		}
		if err := s.flush(); err != nil {
			s.abandon(websocket.StatusGoingAway, "write failed")
			return s.result(0)
		}
	}
}

func (s *session) result(code int) result {
	return result{code: code, liveSince: s.liveSince}
}

func (s *session) read() {
	defer close(s.readerDone)
	for {
		b, err := s.conn.Read(s.connCtx)
		select {
		case s.frames <- readResult{b, err}:
		case <-s.connCtx.Done():
			return
		}
		if err != nil {
			return
		}
	}
}

// finish tears the connection state down (the socket is already closing).
func (s *session) finish() {
	s.connCancel()
	<-s.readerDone
	c := s.c
	c.mu.Lock()
	c.live = false
	c.switching = false
	c.pendingMax = 0
	c.ackDirty = false
	c.watchDirty = false
	c.out.reset()
	c.mu.Unlock()
}

// abandon closes a connection we gave up on without waiting for the peer.
func (s *session) abandon(code websocket.StatusCode, reason string) {
	conn := s.conn
	go func() { _ = conn.Close(int(code), reason) }()
}

// closeGracefully sends 1000 and waits briefly for the close handshake.
func (s *session) closeGracefully() {
	done := make(chan struct{})
	conn := s.conn
	go func() {
		_ = conn.Close(int(websocket.StatusNormalClosure), "client closed")
		close(done)
	}()
	t := time.NewTimer(closeWait)
	defer t.Stop()
	select {
	case <-done:
	case <-t.C:
	}
}

func (s *session) write(data []byte) error {
	ctx, cancel := context.WithTimeout(s.connCtx, writeTimeout)
	defer cancel()
	return s.conn.Write(ctx, data)
}

// flush writes queued frames the rate window allows (live only).
func (s *session) flush() error {
	c := s.c
	for {
		c.mu.Lock()
		if !c.live {
			c.mu.Unlock()
			return nil
		}
		it, ok, wait := c.out.pop(c.now())
		c.mu.Unlock()
		if !ok {
			if wait > 0 && s.outT == nil {
				s.outT = c.after(wait)
			}
			return nil
		}
		if err := s.write(it.data); err != nil {
			return err
		}
	}
}

// onWake (re)arms the ack and watch timers after a public call changed state.
func (s *session) onWake() {
	c := s.c
	c.mu.Lock()
	live := c.live
	armAck := c.ackDirty && live && c.processed > c.acked
	if live {
		c.ackDirty = false
	}
	watchDirty := c.watchDirty && live
	watching := len(c.watch) > 0
	if live {
		c.watchDirty = false
	}
	c.mu.Unlock()
	if armAck && s.ackT == nil {
		s.ackT = c.after(ackInterval)
	}
	if watchDirty {
		s.watchT = nil
		if watching {
			s.watchT = c.after(watchRefresh)
		}
	}
}

// handle processes one server frame; false drops the connection (protocol error).
func (s *session) handle(b []byte) bool {
	c := s.c
	ev, err := parseFrame(b)
	if err != nil {
		c.log.Debug("realtime: bad frame ignored", "err", err)
		return true
	}
	s.pongT = nil // any frame proves the link is alive
	switch ev.Type {
	case "pong":
		return true
	case "hello":
		return s.onHello(ev)
	case "ready":
		return s.onReady(ev)
	}
	if !s.gotHello {
		c.log.Debug("realtime: frame before hello ignored", "type", ev.Type)
		return true
	}
	c.mu.Lock()
	if ev.Seq != nil {
		seq := *ev.Seq
		if seq <= c.recvMax {
			c.mu.Unlock()
			return true // duplicate at the replay boundary
		}
		c.recvMax = seq
		if !c.live && s.firstReplayed == 0 {
			s.firstReplayed = seq
		}
		if !c.switching {
			c.deliveredMax = max(c.deliveredMax, seq)
		}
	}
	ev.Replayed = !c.live
	c.mu.Unlock()
	if ev.Type == "device.tui_state" {
		if v, err := ev.Decode(); err == nil {
			c.setTUIOnDevice(v.(DeviceTUIState).TUIConnected)
			c.report()
		}
	}
	c.emit(ev)
	return true
}

func (s *session) onHello(ev Event) bool {
	c := s.c
	if s.gotHello {
		return true
	}
	v, err := ev.Decode()
	if err != nil {
		c.log.Debug("realtime: bad hello", "err", err)
		return false
	}
	h := v.(Hello)
	s.gotHello = true
	s.helloT = nil
	s.helloEpoch = h.Epoch
	s.helloSeq = h.Seq
	s.interval = time.Duration(h.HeartbeatIntervalMS) * time.Millisecond
	if s.interval <= 0 {
		s.interval = defaultHeartbeat
	}

	now := c.now()
	c.mu.Lock()
	c.switching = c.epoch == nil || *c.epoch != h.Epoch
	switch {
	case !c.switching:
		c.recvMax = c.processed // exactly the last_seq we resume with
	case c.epoch == nil && c.deviceAck:
		c.recvMax = 0 // the server picks the base from our last ack
	default:
		c.recvMax = h.Seq // first connect or new epoch: replay starts after hello.seq
	}
	c.pendingMax = 0
	resume := encodeResume(c.positionLocked())
	c.out.count(now)
	c.mu.Unlock()

	if err := s.write(resume); err != nil {
		return false
	}
	s.pingT = c.after(s.interval)
	c.setTUIOnDevice(h.LocalTUIConnected)
	c.setPhase(PhaseSyncing, nil, time.Time{})
	return true
}

func (s *session) onReady(ev Event) bool {
	c := s.c
	if !s.gotHello {
		c.log.Debug("realtime: ready before hello")
		return false
	}
	v, err := ev.Decode()
	if err != nil {
		c.log.Debug("realtime: bad ready", "err", err)
		return false
	}
	r := v.(Ready)
	epoch := r.Epoch
	if epoch == "" {
		epoch = s.helloEpoch
	}

	c.mu.Lock()
	if c.live {
		c.mu.Unlock()
		return true // duplicate ready
	}
	switch {
	case c.switching || c.epoch == nil || *c.epoch != epoch:
		// New baseline: hello.seq, or just before the first replayed event
		// when the server resumed from our device ack.
		base := s.helloSeq
		if r.Resumed && !r.ResyncRequired && s.firstReplayed > 0 {
			base = s.firstReplayed - 1
		}
		e := epoch
		c.epoch = &e
		c.processed = max(base, c.pendingMax)
		c.deliveredMax = max(base, c.recvMax, c.processed)
		c.acked = 0
	case r.ResyncRequired:
		// Same epoch, but our seq fell out of the log.
		c.processed = max(c.processed, s.helloSeq)
		c.deliveredMax = max(c.deliveredMax, c.processed)
	}
	c.deviceAck = false
	c.switching = false
	c.pendingMax = 0
	c.live = true
	full := c.mode == ModeFull
	if full && c.idle {
		c.out.push(kindIdle, encodeIdle(true))
	}
	watching := full && len(c.watch) > 0
	if watching {
		c.out.push(kindWatch, encodeWatch(c.watch))
	}
	needAck := c.processed > c.acked
	c.ackDirty = false
	c.watchDirty = false
	c.mu.Unlock()

	s.liveSince = c.now()
	if needAck && s.ackT == nil {
		s.ackT = c.after(ackInterval)
	}
	if watching {
		s.watchT = c.after(watchRefresh)
	}
	c.log.Debug("realtime: socket ready", "resumed", r.Resumed, "resync_required", r.ResyncRequired, "replayed", r.Replayed)
	c.setPhase(PhaseLive, nil, time.Time{})
	ev.Replayed = false
	c.emit(ev)
	return true
}
