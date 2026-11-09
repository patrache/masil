package workspace

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
	"github.com/patrache/masil/internal/workspace/fsroot"
	"github.com/patrache/masil/internal/workspace/share"
	"github.com/patrache/masil/internal/workspace/share/sharetest"
	"github.com/patrache/masil/internal/workspace/wire"
)

// guestRig is the participant's surface (the harness user, sam, seat 1) over
// a sharetest.Relay with minji's share.Host on the fixture folder, everything
// on the harness clock.
type guestRig struct {
	t     *testing.T
	h     *uitest.Harness
	s     *Surface
	relay *sharetest.Relay
	host  *share.Host
	docs  *share.DocManager
	dir   string
	hEv   []share.Event
	term  *outTerm
	// sentMu guards sent: every frame the participant's connection sent
	sentMu sync.Mutex
	sent   [][]byte
}

// sentKinds counts the frames the participant sent, by kind.
func (r *guestRig) sentKinds() map[wire.Kind]int {
	r.sentMu.Lock()
	defer r.sentMu.Unlock()
	n := map[wire.Kind]int{}
	for _, f := range r.sent {
		if hd, err := wire.ParseHeader(f); err == nil {
			n[hd.Kind]++
		}
	}
	return n
}

var samUser = api.UserSummary{ID: fixtures.UserSam, Handle: "sam", DisplayName: "sam"}

// outTerm is the owner's shared terminal: what Snapshot and the tap send.
type outTerm struct {
	mu         sync.Mutex
	snap       []byte
	tap        func([]byte)
	cols, rows int
	title      string
	injected   []byte
}

func (f *outTerm) Tap(fn func([]byte)) func() {
	f.mu.Lock()
	f.tap = fn
	f.mu.Unlock()
	return func() { f.mu.Lock(); f.tap = nil; f.mu.Unlock() }
}
func (f *outTerm) Snapshot() []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]byte(nil), f.snap...)
}
func (f *outTerm) Inject(p []byte) error {
	f.mu.Lock()
	f.injected = append(f.injected, p...)
	f.mu.Unlock()
	return nil
}
func (f *outTerm) Size() (int, int)      { return f.cols, f.rows }
func (f *outTerm) SetFixedSize(int, int) {}
func (f *outTerm) BracketedPaste() bool  { return false }
func (f *outTerm) Title() string         { return f.title }
func (f *outTerm) output(b []byte) {
	f.mu.Lock()
	fn := f.tap
	f.snap = append(f.snap, b...)
	f.mu.Unlock()
	if fn != nil {
		fn(b)
	}
}

// newGuestRig starts minji's share of a fixture copy (prep may add files)
// and opens sam's guest surface on {"join": share id}.
func newGuestRig(t *testing.T, h *uitest.Harness, prep func(dir string)) *guestRig {
	t.Helper()
	r := &guestRig{t: t, h: h, dir: copyFixture(t)}
	if prep != nil {
		prep(r.dir)
	}
	root, err := fsroot.Open(r.dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	root.SetIgnorer(fsroot.NewFileIgnorer(root))
	clock := harnessClock{h}
	r.docs = share.NewDocManager(root, nil, clock)
	r.relay = sharetest.NewRelay(rigShare, fixtures.UserMinji, map[uint8]string{1: fixtures.UserSam})
	r.relay.SetClock(h.Clock().Now)
	guestDial := r.relay.Dial(fixtures.UserSam)
	t.Cleanup(SetDialForTest(func(ctx context.Context, id string) (share.Conn, error) {
		c, err := guestDial(ctx, id)
		if err != nil {
			return nil, err
		}
		return recConn{Conn: c, rec: func(f []byte) {
			r.sentMu.Lock()
			r.sent = append(r.sent, append([]byte(nil), f...))
			r.sentMu.Unlock()
		}}, nil
	}))
	r.host = share.NewHost(share.HostConfig{ShareID: rigShare, OwnerID: fixtures.UserMinji, Title: "민지의 작업공간",
		Dial: r.relay.Dial(fixtures.UserMinji), Clock: clock, ManualTick: true, Root: root, Docs: r.docs})
	if err := r.host.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.host.Kill)
	r.term = &outTerm{cols: 120, rows: 30, title: "claude"}
	if err := r.host.ShareTerminal(1, r.term); err != nil {
		t.Fatal(err)
	}
	h.API.JSON("GET /v1/workspace-shares/{id}", 200, map[string]any{"share": api.WorkspaceShare{
		ID: rigShare, Title: "민지의 작업공간", Status: "live", Owner: minjiUser,
		Members: []api.WorkspaceShareMember{{User: samUser, Seat: 1}},
	}})
	route := joinRoute(rigShare)
	h.Send(ui.NavigateMsg{Route: route})
	s, ok := h.Instance(route.Key()).(*Surface)
	if !ok {
		t.Fatalf("no guest surface (got %T)", h.Instance(route.Key()))
	}
	r.s = s
	t.Cleanup(func() { s.CloseTool(h.Context()) })
	r.settle()
	return r
}

func joinRoute(id string) ui.Route {
	return ui.Route{Kind: ui.RouteTool, ID: joinInstanceID(id), Arg: ui.ToolWorkspace, Query: map[string]string{"join": id}}
}

// settle pumps the relay, the host and the guest surface until nothing moves.
func (r *guestRig) settle() {
	r.t.Helper()
	quiet := 0
	for i := 0; i < 300 && quiet < 3; i++ {
		n := r.relay.Pump()
		r.host.Tick(r.h.Clock().Now())
		r.hEv = append(r.hEv, r.host.Drain()...)
		r.h.Advance(pumpEvery)
		r.h.Flush()
		if n == 0 {
			quiet++
		} else {
			quiet = 0
		}
	}
}

// admit answers the check number as the guest (match) and lets seat 1 in at
// level (verified when level is editor or above).
func (r *guestRig) admit(level wire.Level) {
	r.t.Helper()
	verified := level >= wire.LevelEditor
	if verified {
		if r.s.dlg == nil || !strings.Contains(r.h.Screen(), formatSAS(r.s.guest.sas)) {
			r.t.Fatalf("no check-number dialog:\n%s", r.h.Screen())
		}
		r.h.Press("enter") // 일치 is the first choice
		r.settle()
	}
	if err := r.host.Approve(1, level, verified); err != nil {
		r.t.Fatal(err)
	}
	r.settle()
	if r.s.guest.phase != guestIn {
		r.t.Fatalf("guest not in (phase %d):\n%s", r.s.guest.phase, r.h.Screen())
	}
}

func TestGuestJoinsAndEdits(t *testing.T) {
	setup(t)
	h := uitest.New(t)
	r := newGuestRig(t, h, nil)
	if r.s.dlg == nil {
		t.Fatalf("no check-number dialog:\n%s", h.Screen())
	}
	scr := h.Screen()
	for _, want := range []string{r.s.guest.sas, "일치", "다름"} {
		if !strings.Contains(scr, want) {
			t.Fatalf("SAS dialog lacks %q:\n%s", want, scr)
		}
	}
	if pend := r.host.Pending(); len(pend) != 1 || pend[0].SAS != r.s.guest.sas {
		t.Fatalf("host pending %+v, guest SAS %q", pend, r.s.guest.sas)
	}
	r.admit(wire.LevelEditor)
	if !strings.Contains(flat(h.Screen()), "src/") {
		t.Fatalf("no remote tree:\n%s", h.Screen())
	}
	r.s.guestOpen(h.Context(), hubTS)
	r.settle()
	if d := r.s.code.current(); d == nil || d.rel != hubTS {
		t.Fatalf("document not open:\n%s", h.Screen())
	}
	h.Press("i", "Z", "esc")
	r.settle()
	text, _, ok := r.docs.Text(hubTS)
	if !ok || !strings.Contains(text, "Z") {
		t.Fatalf("owner text lacks the guest's Z: %q", text)
	}
	h.Check()
}

func TestGuestEndedWithinOneSecond(t *testing.T) {
	setup(t)
	h := uitest.New(t)
	r := newGuestRig(t, h, nil)
	r.admit(wire.LevelViewer)
	r.s.guestOpen(h.Context(), hubTS)
	r.settle()
	r.host.Kill()
	start := time.Now()
	for !r.relay.Ended() { // the kill switch tells the server in the background
		if time.Since(start) > time.Second {
			t.Fatal("server not told within 1s")
		}
		time.Sleep(time.Millisecond)
	}
	r.relay.Pump()
	for range 30 { // 30 beats of 33 ms: under a second
		h.Advance(pumpEvery)
		h.Flush()
		r.relay.Pump()
	}
	if !strings.Contains(h.Screen(), "공유가 끝났어요") {
		t.Fatalf("no ended notice within a second:\n%s", h.Screen())
	}
	if len(r.s.code.docs) != 0 || len(r.s.guest.terms) != 0 {
		t.Fatalf("remote documents or screens kept: %d docs, %d terms", len(r.s.code.docs), len(r.s.guest.terms))
	}
	h.Press("a")
	if h.Instance(joinRoute(rigShare).Key()) != nil {
		t.Fatal("any key did not close the ended share")
	}
}

func TestGuestPanFollowsCursor(t *testing.T) {
	setup(t)
	h := uitest.New(t)
	h.Resize(80, 24)
	r := newGuestRig(t, h, nil)
	r.admit(wire.LevelViewer)
	r.s.setFocus(h.Context(), PaneTerm)
	r.term.output([]byte("\x1b[2J\x1b[1;1HLEFTEDGE\x1b[2;1H"))
	r.settle()
	h.Advance(share.KeyframeEvery)
	r.settle()
	rt := r.s.guest.current()
	if rt == nil || !strings.Contains(h.Screen(), "LEFTEDGE") {
		t.Fatalf("no replay of the owner terminal:\n%s", h.Screen())
	}
	vw := rt.viewW
	if vw <= 0 || vw >= 120 {
		t.Fatalf("view width %d", vw)
	}
	if want := "→ " + strconv.Itoa(120-vw) + "칸 더"; !strings.Contains(h.Screen(), want) {
		t.Fatalf("no %q:\n%s", want, h.Screen())
	}
	r.term.output([]byte("\x1b[3;105HRIGHTMARK"))
	r.settle()
	scr := h.Screen()
	if !strings.Contains(scr, "RIGHTMARK") || strings.Contains(scr, "LEFTEDGE") {
		t.Fatalf("the window did not follow the cursor to col 114:\n%s", scr)
	}
	x0 := rt.x0
	h.Press("ctrl+\\", "[")
	h.Press("left", "left", "left", "left", "left")
	if rt.follow || rt.x0 != x0-5 {
		t.Fatalf("pan: follow %v, x0 %d (was %d)", rt.follow, rt.x0, x0)
	}
	h.Press("f")
	if !rt.follow || rt.x0 != x0 {
		t.Fatalf("f: follow %v, x0 %d (want %d)", rt.follow, rt.x0, x0)
	}
	h.Check()
}

func TestGuestClipboardUntouched(t *testing.T) {
	setup(t)
	h := uitest.New(t)
	r := newGuestRig(t, h, nil)
	r.admit(wire.LevelViewer)
	r.s.setFocus(h.Context(), PaneTerm)
	r.term.output([]byte("hello\x1b]52;c;aGk=\x07\x1b]0;evil\x07 world"))
	r.settle()
	h.Advance(share.KeyframeEvery)
	r.settle()
	if !strings.Contains(h.Screen(), "hello world") {
		t.Fatalf("no replayed output:\n%s", h.Screen())
	}
	if c := h.Clipboards(); len(c) != 0 {
		t.Fatalf("clipboard written: %q", c)
	}
	for _, m := range h.TeaMsgs() {
		if strings.Contains(strings.ToLower(fmt.Sprintf("%T", m)), "clipboard") {
			t.Fatalf("clipboard msg %T", m)
		}
	}
	if strings.Contains(h.Screen(), "evil") {
		t.Fatalf("the owner's title escape reached the screen:\n%s", h.Screen())
	}
}

func TestGuestTreeSanitizesHostileNames(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("such names cannot be made on Windows")
	}
	setup(t)
	h := uitest.New(t)
	hostile := "a\x1b[31mred\u202eb.txt"
	r := newGuestRig(t, h, func(dir string) {
		if err := os.WriteFile(filepath.Join(dir, hostile), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	})
	r.admit(wire.LevelViewer)
	scr := h.Screen()
	if !strings.Contains(scr, "red") {
		t.Fatalf("the hostile file is not listed:\n%s", scr)
	}
	if strings.ContainsRune(scr, 0x1b) || strings.ContainsRune(scr, 0x202e) {
		t.Fatalf("raw ESC or U+202E reached the screen:\n%q", scr)
	}
	h.Check()
}

func TestViewerIsReadOnly(t *testing.T) {
	setup(t)
	h := uitest.New(t)
	r := newGuestRig(t, h, nil)
	r.admit(wire.LevelViewer)
	r.s.guestOpen(h.Context(), hubTS)
	r.settle()
	if d := r.s.code.current(); d == nil {
		t.Fatalf("not open:\n%s", h.Screen())
	}
	d := r.s.code.current()
	if !d.ed.ReadOnly() {
		t.Fatal("a viewer's editor is writable")
	}
	before, _, _ := r.docs.Text(hubTS)
	mine := d.ed.Text()
	h.Press("i", "x", "esc", "x")
	r.settle()
	after, _, _ := r.docs.Text(hubTS)
	if after != before {
		t.Fatalf("a viewer changed the owner's text")
	}
	if d.ed.Text() != mine {
		t.Fatal("a viewer's keys changed its own copy")
	}
	if len(r.sentKinds()) == 0 {
		t.Fatal("no frame recorded from the participant (the recorder is not wired)")
	}
	if n := r.sentKinds()[wire.KindDocOp]; n != 0 {
		t.Fatalf("a viewer sent %d doc_op frames", n)
	}
	for _, ev := range r.hEv {
		if _, ok := ev.(share.EventDocApplied); ok {
			t.Fatal("a viewer's doc_op was sequenced")
		}
	}
}

func TestWindowsGuestCanWatch(t *testing.T) {
	setup(t)
	t.Cleanup(SetPaneSupportedForTest(false))
	h := uitest.New(t)
	r := newGuestRig(t, h, nil)
	r.admit(wire.LevelViewer)
	r.s.setFocus(h.Context(), PaneTerm)
	r.term.output([]byte("KEYFRAMEDATA"))
	h.Advance(share.KeyframeEvery)
	r.settle()
	if !strings.Contains(h.Screen(), "KEYFRAMEDATA") {
		t.Fatalf("no replay without panes:\n%s", h.Screen())
	}
}

func TestGuestProposes(t *testing.T) {
	setup(t)
	h := uitest.New(t)
	r := newGuestRig(t, h, nil)
	r.admit(wire.LevelPrompter)
	r.s.setFocus(h.Context(), PaneTerm)
	h.Type("npm test")
	h.Press("enter")
	r.settle()
	ps := r.host.Proposals()
	if len(ps) != 1 || ps[0].Text != "npm test" {
		t.Fatalf("host proposals %+v", ps)
	}
}

func TestGuestKeyboardInput(t *testing.T) {
	setup(t)
	h := uitest.New(t)
	r := newGuestRig(t, h, nil)
	r.admit(wire.LevelEditor)
	if err := r.host.GrantKeyboard(1, share.DefaultKeyboard); err != nil {
		t.Fatal(err)
	}
	r.settle()
	r.s.setFocus(h.Context(), PaneTerm)
	h.Type("ls")
	h.Press("enter")
	r.settle()
	r.term.mu.Lock()
	got := string(r.term.injected)
	r.term.mu.Unlock()
	if got != "ls\r" {
		t.Fatalf("injected %q", got)
	}
}

func TestGuestTiny(t *testing.T) {
	setup(t)
	h := uitest.New(t)
	h.Resize(40, 14)
	r := newGuestRig(t, h, nil)
	r.admit(wire.LevelViewer)
	if !strings.Contains(flat(h.Screen()), "민지의작업공간을보는중") {
		t.Fatalf("no tiny summary:\n%s", h.Screen())
	}
	h.Press("q")
	if h.Instance(joinRoute(rigShare).Key()) != nil {
		t.Fatal("q did not leave")
	}
}

func TestGuestReconnectingBand(t *testing.T) {
	setup(t)
	h := uitest.New(t)
	r := newGuestRig(t, h, nil)
	r.admit(wire.LevelViewer)
	r.relay.Drop(1)
	h.Advance(time.Second)
	h.Flush()
	if !strings.Contains(h.Screen(), "다시 연결 중…") {
		t.Fatalf("no reconnecting band:\n%s", h.Screen())
	}
	r.settle()
	h.Advance(5 * time.Second)
	r.settle()
	if strings.Contains(h.Screen(), "다시 연결 중…") {
		t.Fatalf("reconnecting band stayed:\n%s", h.Screen())
	}
}

func TestPickerListsInvites(t *testing.T) {
	setup(t)
	h := uitest.New(t)
	h.Event("workspace_share.invited", map[string]any{"share": api.WorkspaceShare{
		ID: "ws_09", Title: "민지의 작업공간", Status: "live", Owner: minjiUser, CreatedAt: h.Clock().Now(),
		Members: []api.WorkspaceShareMember{{User: samUser, Seat: 1}},
	}})
	h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteTool, ID: pickerID, Arg: ui.ToolWorkspace}})
	scr := flat(h.Screen())
	if !strings.Contains(scr, "초대받은공유") || !strings.Contains(scr, "민지님·민지의작업공간") {
		t.Fatalf("no invited share in the picker:\n%s", h.Screen())
	}
}
