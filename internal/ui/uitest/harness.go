// Package uitest is the deterministic UI test harness: fake clock, synchronous
// Cmd execution, sizes and integrity checks (CA 10.2).
package uitest

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"reflect"
	"regexp"
	"testing"
	"time"
	_ "time/tzdata" // Asia/Seoul without a system zoneinfo (CA 10.1)
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/golden"

	"github.com/patrache/masil/internal/api/apitest"
	"github.com/patrache/masil/internal/auth"
	"github.com/patrache/masil/internal/clipboard"
	mconfig "github.com/patrache/masil/internal/config"
	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/notify"
	"github.com/patrache/masil/internal/realtime"
	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/app"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

// Start is the fixed harness clock start (CA 10.1): 2026-11-05T02:05:00Z (서울 11:05).
var Start = time.Date(2026, 11, 5, 2, 5, 0, 0, time.UTC)

// Default harness size (sent right after New).
const defaultW, defaultH = 120, 40

// maxDepth is the deepest Cmd cascade Send follows (CA 10.2).
const maxDepth = 32

// blockLimit is how long one Cmd may run before the harness reports it (CA 10.2).
// CI runners (CI is set, as on GitHub Actions) run the whole parallel `go test ./...`
// several times slower, so a Cmd doing real work can pass 1s there; the limit is 10s on
// CI and stays 1s locally, where the gate runs.
var blockLimit = func() time.Duration {
	if os.Getenv("CI") != "" {
		return 10 * time.Second
	}
	return time.Second
}()

// teaPkg is the package path of bubbletea-internal messages (recorded, not re-dispatched).
const teaPkg = "charm.land/bubbletea/v2"

// Harness drives an app.Model synchronously.
type Harness struct {
	T      testing.TB
	Model  *app.Model
	API    *apitest.Server
	Notify *notify.Recorder
	// Clip records system-clipboard writes (ui.Copy); Clip.Fail makes them fail.
	Clip *clipboard.Recorder

	clock   *FakeClock
	teaMsgs []tea.Msg
	seq     int64 // last injected logged-event seq
}

// New builds a harness with a temp MASIL_CONFIG_DIR/MASIL_CACHE_DIR, an apitest server
// (MASIL_SERVER), the fixture user logged in, realtime off, the FakeClock, then applies
// the seed and sends a 120x40 WindowSizeMsg.
func New(tb testing.TB, opts ...Option) *Harness {
	tb.Helper()
	var c config
	for _, o := range opts {
		if o != nil {
			o(&c)
		}
	}
	tb.Setenv("MASIL_CONFIG_DIR", tb.TempDir())
	tb.Setenv("MASIL_CACHE_DIR", tb.TempDir())
	srv := c.api
	if srv == nil {
		srv = apitest.New(tb)
		fixtures.RegisterAPI(srv)
	}
	tb.Setenv("MASIL_SERVER", srv.URL)
	paths, err := mconfig.ResolvePaths()
	if err != nil {
		tb.Fatalf("uitest: ResolvePaths: %v", err)
		return nil
	}
	if err := mconfig.EnsureDir(paths); err != nil {
		tb.Fatalf("uitest: EnsureDir: %v", err)
		return nil
	}

	creds := &auth.Credentials{Version: 1, Server: srv.URL, Token: "msl_test", UserID: fixtures.UserSam, Handle: "sam"}
	if c.credsSet {
		creds = c.creds
	}
	cfg := mconfig.Defaults()
	cfg.Theme = string(theme.Default)
	if c.theme != "" {
		cfg.Theme = string(c.theme)
	}
	cfg.UI.Animations = false
	cfg.UI.Images = "halfblock"
	if c.pin != nil {
		cfg.Layout.Pin = c.pin.ID()
	}
	if c.keys != nil {
		cfg.Keys = c.keys
	}
	loc, err := time.LoadLocation("Asia/Seoul")
	if err != nil {
		tb.Fatalf("uitest: load Asia/Seoul: %v", err)
		return nil
	}
	clock := NewFakeClock(Start)
	rec := &notify.Recorder{}
	h := &Harness{T: tb, API: srv, Notify: rec, Clip: &clipboard.Recorder{}, clock: clock}
	lang := c.locale
	if lang == "" {
		lang = i18n.Ko
	}
	h.Model = app.New(app.Options{
		Locale:          &i18n.Resolution{Requested: lang, Effective: lang, Source: i18n.SourceEnv},
		Paths:           paths,
		Config:          cfg,
		Credentials:     creds,
		InitialRoute:    c.route,
		Now:             clock.Now,
		Loc:             loc,
		DisableRealtime: true,
		Notifier:        notify.New(rec, notify.OpenLedger(paths), slog.New(slog.DiscardHandler)),
		Timers:          clock,
		Clipboard:       h.Clip,
	})
	seed := c.seed
	if seed == nil {
		seed = fixtures.Seed
	}
	seed(h.Model.Context().Store)
	if !h.process(h.Model.Init()) {
		return h
	}
	if c.unicode != nil {
		v := ansi.ModeReset
		if *c.unicode {
			v = ansi.ModeSet
		}
		h.Send(tea.ModeReportMsg{Mode: ansi.ModeUnicodeCore, Value: v})
	}
	h.Resize(defaultW, defaultH)
	return h
}

// Resize sends a WindowSizeMsg.
func (h *Harness) Resize(w, hgt int) {
	h.T.Helper()
	h.Send(tea.WindowSizeMsg{Width: w, Height: hgt})
}

// Press sends keys: "ctrl+k", "j", "enter", "shift+tab", "esc".
func (h *Harness) Press(keys ...string) {
	h.T.Helper()
	for _, k := range keys {
		h.Send(parseKey(k))
	}
}

// csiRe matches one CSI escape sequence (it arrives as one unit, like a terminal).
var csiRe = regexp.MustCompile(`^\x1b\[[0-9;]*[A-Za-z]`)

// Type sends one KeyPressMsg per grapheme cluster (Korean syllables, emoji
// sequences): Text is the grapheme, Code its rune when it is a single printable rune,
// else tea.KeyExtended.
func (h *Harness) Type(s string) {
	h.T.Helper()
	for s != "" {
		g := csiRe.FindString(s)
		if g == "" {
			g, _ = ansi.FirstGraphemeCluster(s, ansi.GraphemeWidth)
		}
		if g == "" {
			_, n := utf8.DecodeRuneInString(s)
			g = s[:n]
		}
		k := tea.KeyPressMsg{Code: tea.KeyExtended, Text: g}
		if r, n := utf8.DecodeRuneInString(g); n == len(g) && r >= 0x20 && r != 0x7f {
			k.Code = r
		}
		h.Send(k)
		s = s[len(g):]
	}
}

// Paste sends a PasteMsg.
func (h *Harness) Paste(s string) {
	h.T.Helper()
	h.Send(tea.PasteMsg{Content: s})
}

// Click sends a left click: the press and its release at (x, y), as a
// terminal reports it.
func (h *Harness) Click(x, y int) {
	h.T.Helper()
	h.Send(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	h.Send(tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft})
}

// Drag presses the left button at (x0, y0), moves it with the button held
// to (x1, y1) one cell at a time (rows first, then columns; a terminal in
// cell-motion mode reports every cell change) and releases it there.
func (h *Harness) Drag(x0, y0, x1, y1 int) {
	h.T.Helper()
	h.Send(tea.MouseClickMsg{X: x0, Y: y0, Button: tea.MouseLeft})
	x, y := x0, y0
	for y != y1 {
		y += sign(y1 - y)
		h.Send(tea.MouseMotionMsg{X: x, Y: y, Button: tea.MouseLeft})
	}
	for x != x1 {
		x += sign(x1 - x)
		h.Send(tea.MouseMotionMsg{X: x, Y: y, Button: tea.MouseLeft})
	}
	h.Send(tea.MouseReleaseMsg{X: x1, Y: y1, Button: tea.MouseLeft})
}

func sign(n int) int {
	switch {
	case n > 0:
		return 1
	case n < 0:
		return -1
	}
	return 0
}

// Clipboards are the texts written to the system clipboard so far (ui.Copy).
func (h *Harness) Clipboards() []string {
	return h.Clip.Texts()
}

// Wheel sends |delta| wheel events at (x, y): negative = up, positive = down.
func (h *Harness) Wheel(x, y, delta int) {
	h.T.Helper()
	btn := tea.MouseWheelDown
	if delta < 0 {
		btn, delta = tea.MouseWheelUp, -delta
	}
	for range delta {
		h.Send(tea.MouseWheelMsg{X: x, Y: y, Button: btn})
	}
}

// queued is a message waiting for Update, with its cascade depth.
type queued struct {
	msg   tea.Msg
	depth int
}

// Send runs Update + resulting Cmds synchronously (CA 10.2 Send rules): breadth first,
// at most maxDepth levels (deeper => Fatal); tea.BatchMsg is expanded; bubbletea-internal
// msgs are recorded in TeaMsgs, never re-dispatched (tea.Sequence => Errorf); a Cmd
// running longer than 1s => Fatal.
func (h *Harness) Send(msg tea.Msg) {
	h.T.Helper()
	queue := []queued{{msg: msg}}
	for len(queue) > 0 {
		it := queue[0]
		queue = queue[1:]
		if it.depth > maxDepth {
			h.T.Fatalf("uitest: Cmd cascade deeper than %d levels (a msg loop?): %T", maxDepth, it.msg)
			return
		}
		_, cmd := h.Model.Update(it.msg)
		var ok bool
		if queue, ok = h.enqueue(queue, cmd, it.depth+1); !ok {
			return
		}
	}
}

// process runs cmd (Init) and Sends each resulting msg. It reports false when the
// harness gave up (Fatal through a non-stopping TB).
func (h *Harness) process(cmd tea.Cmd) bool {
	h.T.Helper()
	msgs, ok := h.runCmd(cmd)
	for _, m := range msgs {
		h.Send(m)
	}
	return ok
}

// enqueue runs cmd and appends its resulting msgs at depth.
func (h *Harness) enqueue(queue []queued, cmd tea.Cmd, depth int) ([]queued, bool) {
	h.T.Helper()
	msgs, ok := h.runCmd(cmd)
	for _, m := range msgs {
		queue = append(queue, queued{msg: m, depth: depth})
	}
	return queue, ok
}

// runCmd runs one Cmd (expanding tea.BatchMsg) and returns the msgs to dispatch.
func (h *Harness) runCmd(cmd tea.Cmd) ([]tea.Msg, bool) {
	h.T.Helper()
	if cmd == nil {
		return nil, true
	}
	msg, ok := h.call(cmd)
	if !ok {
		return nil, false
	}
	switch m := msg.(type) {
	case nil:
		return nil, true
	case tea.BatchMsg:
		var out []tea.Msg
		for _, c := range m {
			ms, ok := h.runCmd(c)
			out = append(out, ms...)
			if !ok {
				return out, false
			}
		}
		return out, true
	}
	if t := reflect.TypeOf(msg); t.PkgPath() == teaPkg {
		h.teaMsgs = append(h.teaMsgs, msg)
		if t.Name() == "sequenceMsg" {
			h.T.Errorf("tea.Sequence is forbidden in UI code (5.3.2)")
		}
		return nil, true
	}
	return []tea.Msg{msg}, true
}

// call runs cmd on its own goroutine and waits at most blockLimit. A panic inside the
// Cmd is re-raised on the caller's goroutine.
func (h *Harness) call(cmd tea.Cmd) (tea.Msg, bool) {
	h.T.Helper()
	type result struct {
		msg   tea.Msg
		panic any
	}
	done := make(chan result, 1)
	go func() {
		var r result
		defer func() {
			if p := recover(); p != nil {
				r.panic = p
			}
			done <- r
		}()
		r.msg = cmd()
	}()
	timer := time.NewTimer(blockLimit)
	defer timer.Stop()
	select {
	case r := <-done:
		if r.panic != nil {
			panic(fmt.Sprintf("uitest: Cmd panicked: %v", r.panic))
		}
		return r.msg, true
	case <-timer.C:
		h.T.Fatalf("Cmd blocked > %v: raw tea.Tick/Every or sleep in UI code?", blockLimit)
		return nil, false
	}
}

// Advance moves the FakeClock and delivers due timer msgs via Send, in order: the
// earliest due msg (ties: registration order) first, the clock set to its deadline;
// timers armed meanwhile are delivered in the same call when due. The clock ends at
// now+d.
func (h *Harness) Advance(d time.Duration) {
	h.T.Helper()
	target := h.clock.Now().Add(d)
	for {
		msg, ok := h.clock.nextDue(target)
		if !ok {
			break
		}
		h.Send(msg)
	}
	h.clock.mu.Lock()
	if target.After(h.clock.now) {
		h.clock.now = target
	}
	h.clock.mu.Unlock()
}

// Clock returns the fake clock.
func (h *Harness) Clock() *FakeClock {
	return h.clock
}

// TeaMsgs returns bubbletea-internal msgs produced by Cmds (recorded, not re-dispatched).
func (h *Harness) TeaMsgs() []tea.Msg {
	return append([]tea.Msg(nil), h.teaMsgs...)
}

// loggedEvents are the realtime types that carry a seq (events.md 4장; mirrors
// realtime's own table, which is unexported).
var loggedEvents = map[string]bool{
	"message.created": true, "message.updated": true, "message.deleted": true, "thread.updated": true,
	"reaction.added": true, "reaction.removed": true, "message.pinned": true, "message.unpinned": true,
	"poll.updated": true, "read_state.updated": true,
	"channel.created": true, "channel.updated": true, "channel.deleted": true, "channel.member_added": true,
	"channel.member_removed": true, "channel.prefs_updated": true,
	"space.joined": true, "space.updated": true, "space.deleted": true, "space.removed": true,
	"space.member_joined": true, "space.member_left": true, "space.member_updated": true,
	"space.invite_received": true, "space.invite_removed": true,
	"friend.request_received": true, "friend.request_accepted": true, "friend.request_removed": true,
	"friend.added": true, "friend.removed": true, "block.updated": true, "user.updated": true, "settings.updated": true,
	"focus.started": true, "focus.ended": true,
	"notification.created": true, "notifications.read": true, "alert": true,
}

// Event injects a ui.EventMsg wrapping realtime.Event{typ, data} (seq auto-increments
// for logged types; data is JSON-encoded unless it already is json.RawMessage/[]byte).
func (h *Harness) Event(typ string, data any) {
	h.T.Helper()
	var raw json.RawMessage
	switch d := data.(type) {
	case json.RawMessage:
		raw = d
	case []byte:
		raw = d
	default:
		b, err := json.Marshal(data)
		if err != nil {
			h.T.Fatalf("uitest: Event(%q): marshal data: %v", typ, err)
			return
		}
		raw = b
	}
	ev := realtime.Event{Type: typ, TS: h.clock.Now(), Data: raw}
	if loggedEvents[typ] {
		h.seq++
		s := h.seq
		ev.Seq = &s
	}
	h.Send(ui.EventMsg{Event: ev})
}

// Screen is ansi.Strip(View().Content).
func (h *Harness) Screen() string {
	return ansi.Strip(h.Model.View().Content)
}

// Styled is the raw View().Content.
func (h *Harness) Styled() string {
	return h.Model.View().Content
}

// Golden runs h.Check() + golden.RequireEqual(Screen) under t.Run(name)
// (testdata/<Test>/<sub>/<name>.golden).
func (h *Harness) Golden(name string) {
	h.T.Helper()
	t, ok := h.T.(*testing.T)
	if !ok {
		h.T.Fatalf("uitest: Golden needs a *testing.T, got %T", h.T)
		return
	}
	t.Run(name, func(t *testing.T) {
		t.Helper()
		content := h.Model.View().Content
		h.assert(t, content)
		golden.RequireEqual(t, ansi.Strip(content))
	})
}

// Check runs AssertIntegrity without a golden (sweeps).
func (h *Harness) Check() {
	h.T.Helper()
	h.assert(h.T, h.Model.View().Content)
}

// assert checks one rendered frame (View must have run so Frame() matches content).
func (h *Harness) assert(tb testing.TB, content string) {
	tb.Helper()
	l := h.Model.Frame().Layout
	AssertIntegrity(tb, content, l.Cols, l.Rows, BoxesOf(h.Model.Frame()), h.Context().Theme.Border)
}

// Context returns the app's ui.Context.
func (h *Harness) Context() *ui.Context {
	return h.Model.Context()
}

// Cursor returns the View cursor.
func (h *Harness) Cursor() *tea.Cursor {
	return h.Model.View().Cursor
}

// Nav returns the app's NavState.
func (h *Harness) Nav() app.NavState {
	return h.Model.Nav()
}

// Instance is Model.Instance; tests type-assert to *stub.Chat etc.
func (h *Harness) Instance(routeKey string) ui.Component {
	return h.Model.Instance(routeKey)
}
