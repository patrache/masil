package arch

import (
	"context"
	"encoding/json"
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/archdoc"
	"github.com/patrache/masil/internal/archdraw"
	"github.com/patrache/masil/internal/collab"
	"github.com/patrache/masil/internal/collab/collabtest"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

// waitUntil delivers posted msgs until cond holds (sessions post on their own
// goroutines); Fatal after 5 seconds.
func waitUntil(t *testing.T, h *uitest.Harness, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		h.Flush()
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s:\n%s", what, h.Screen())
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// sharedFixture is the pay_v3 design seeded as room document D1, and the keys
// of two of its nodes.
func sharedFixture(t *testing.T, hub *collabtest.Hub, meta api.Doc) (d *archdoc.Design, ka, kb string) {
	t.Helper()
	d = loadFixture(t, "pay_v3")
	meta.ID = "D1"
	hub.SeedDoc(meta, snapshotOf(d))
	return d, "n:" + d.Nodes[0].ID, "n:" + d.Nodes[1].ID
}

// docHarness opens room document D1 at 120x40 (mouse on) and waits for its
// state.
func docHarness(t *testing.T, hub *collabtest.Hub) (*uitest.Harness, *canvasModel, *sharedSink) {
	t.Helper()
	r := toolRoute(keyDoc+"D1", map[string]string{queryDoc: "D1"})
	h, c := mouseHarness(t, r, 120, 40, uitest.WithCollab(hub))
	sh := c.shared()
	if sh == nil {
		t.Fatalf("the doc route did not open a room document (sink %T)", c.sink)
	}
	t.Cleanup(sh.close)
	waitUntil(t, h, "the document state", func() bool { return sh.online && len(sh.objs) > 0 })
	return h, c, sh
}

// peer is hana's session on document docID.
func peer(t *testing.T, h *uitest.Harness, hub *collabtest.Hub, docID string) *collab.Session {
	t.Helper()
	s := collab.Open(context.Background(), collab.Options{
		API:    api.New(h.API.URL, api.WithToken(fixtures.UserHana)),
		DocID:  docID,
		UserID: fixtures.UserHana,
		Dial:   hub.Dial,
	})
	t.Cleanup(s.Close)
	deadline := time.Now().Add(5 * time.Second)
	for hub.Conns(docID, fixtures.UserHana) == 0 || len(s.View().Snapshot().Objects) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("hana's session did not connect")
		}
		time.Sleep(5 * time.Millisecond)
	}
	return s
}

// objJSON is objects as comparable JSON.
func objJSON(t *testing.T, o archdoc.Objects) string {
	t.Helper()
	b, err := json.Marshal(o)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// shown reports whether text is on screen (a toast wraps a long text: then
// its first eight characters).
func shown(screen, text string) bool {
	if strings.Contains(screen, text) {
		return true
	}
	r := []rune(text)
	return len(r) > 8 && strings.Contains(screen, string(r[:8]))
}

// styledRun reports whether text is drawn in foreground colour fg on the
// styled line: the last SGR before it sets that colour.
func styledRun(line, text string, fg color.Color) bool {
	r, g, b, _ := fg.RGBA()
	want := fmt.Sprintf("38;2;%d;%d;%d", r>>8, g>>8, b>>8)
	for from := 0; ; {
		i := strings.Index(line[from:], text)
		if i < 0 {
			return false
		}
		i += from
		if j := strings.LastIndex(line[:i], "\x1b["); j >= 0 {
			if k := strings.IndexByte(line[j:i], 'm'); k >= 0 && strings.Contains(line[j:j+k], want) {
				return true
			}
		}
		from = i + len(text)
	}
}

func noSpace(s string) string { return strings.Join(strings.Fields(s), "") }

func setOp(id, field, v string) archdoc.Change {
	return archdoc.Change{Kind: archdoc.ChangeSet, Obj: id, Field: field, Value: json.RawMessage(v)}
}

func field(o archdoc.Objects, id, f string) string { return string(o[id][f]) }

func TestShareConfirmShowsPlaintextWarning(t *testing.T) {
	r := toolRoute("arch:file:t1", map[string]string{"file": fixtureName})
	sizes := append(uitest.AllSizes(), uitest.Tiny)
	for _, s := range sizes {
		if s.W < 40 || s.H < 14 {
			continue // too small: no tool screen
		}
		h := newHarness(t, uitest.WithRoute(r), withDesign(t, fixtureName, loadFixture(t, "pay_v3")))
		h.Resize(s.W, s.H)
		h.Send(ui.RunCommandMsg{ID: "arch.share"})
		c := canvasOf(t, h, r)
		if c.shareDlg == nil {
			t.Fatalf("%dx%d: arch.share opened no dialog", s.W, s.H)
		}
		if !strings.Contains(h.Screen(), "노바 개발방") {
			t.Errorf("%dx%d: the space list is not shown:\n%s", s.W, s.H, h.Screen())
		}
		h.Press("enter")
		if c.shareDlg.step != 1 {
			t.Fatalf("%dx%d: enter did not reach the warning", s.W, s.H)
		}
		warn := h.Context().T.Archcanvas.SharePlaintextWarning()
		if warn != "방에 올린 설계는 masil 서버에 암호화하지 않고 저장돼요. 방 멤버와 서버 운영자가 볼 수 있어요. 민감한 설계는 개인 설계로 두세요." {
			t.Fatalf("warning text %q", warn)
		}
		// the dialog as drawn: the whole warning, wrapped (spaces aside), no "…"
		view := ansi.Strip(c.shareDlg.View(h.Context(), true))
		if c.shareDlg.size.W <= 0 || !strings.Contains(h.Screen(), strings.TrimSpace(strings.SplitN(view, "\n", 2)[0])) {
			t.Fatalf("%dx%d: the dialog is not on screen:\n%s", s.W, s.H, h.Screen())
		}
		if !strings.Contains(noSpace(view), noSpace(warn)) || strings.Contains(view, "…") {
			t.Errorf("%dx%d: the warning is not shown whole:\n%s", s.W, s.H, view)
		}
		h.Check()
	}
}

func TestShareCreatesDocAndKeepsLocal(t *testing.T) {
	hub := collabtest.New(t)
	r := toolRoute("arch:file:t1", map[string]string{"file": fixtureName})
	d := loadFixture(t, "pay_v3")
	h := newHarness(t, uitest.WithRoute(r), withDesign(t, fixtureName, d), uitest.WithCollab(hub))
	h.Resize(120, 40)
	file := filepath.Join(os.Getenv("MASIL_CONFIG_DIR"), "arch")
	st, err := archdoc.OpenStore(file)
	if err != nil {
		t.Fatal(err)
	}
	before, err := st.Load(fixtureName)
	if err != nil {
		t.Fatal(err)
	}
	beforeJSON := string(mustEncode(t, before))

	h.Send(ui.RunCommandMsg{ID: "arch.share"})
	h.Press("enter")
	h.Press("enter") // the warning's default button: 올리기
	var body map[string]json.RawMessage
	for _, rq := range h.API.Requests() {
		if rq.Method == "POST" && strings.HasSuffix(rq.Path, "/spaces/"+fixtures.SpaceNova+"/docs") {
			if err := json.Unmarshal(rq.Body, &body); err != nil {
				t.Fatal(err)
			}
		}
	}
	if body == nil {
		t.Fatalf("no CreateDoc request: %+v", h.API.Requests())
	}
	if string(body["type"]) != `"arch"` {
		t.Errorf("type %s", body["type"])
	}
	snap, err := collab.DecodeSnapshot(body["snapshot"])
	if err != nil {
		t.Fatal(err)
	}
	got := archdoc.Objects{}
	for _, o := range snap.Objects {
		got[o.ID] = o.Fields
	}
	if objJSON(t, got) != objJSON(t, d.ToObjects()) {
		t.Errorf("snapshot objects differ from the design's:\n%s\n%s", objJSON(t, got), objJSON(t, d.ToObjects()))
	}
	// the room document opened, the personal file untouched
	if len(hub.Snapshot("D0001").Objects) == 0 {
		t.Error("the room document D0001 has no objects")
	}
	opened, ok := h.Instance(toolRoute(keyDoc+"D0001", nil).Key()).(*canvasModel)
	if !ok || opened.shared() == nil {
		t.Fatalf("the room document did not open (%T)", h.Instance(toolRoute(keyDoc+"D0001", nil).Key()))
	}
	t.Cleanup(opened.shared().close)
	after, err := st.Load(fixtureName)
	if err != nil {
		t.Fatal(err)
	}
	if string(mustEncode(t, after)) != beforeJSON {
		t.Error("the personal design changed")
	}
	if !strings.Contains(h.Screen(), "노바 개발방") {
		t.Errorf("no share toast:\n%s", h.Screen())
	}
}

func TestConcurrentDifferentNodes(t *testing.T) {
	hub := collabtest.New(t)
	_, ka, kb := sharedFixture(t, hub, api.Doc{})
	h, c, sh := docHarness(t, hub)
	b := peer(t, h, hub, "D1")

	// different nodes: both edits land on both sides
	c.commit(h.Context(), []archdoc.Change{setOp(ka, "cpu_cores", "8")})
	b.Submit([]collab.Op{{Op: collab.OpSet, ID: kb, Field: "memory_mib", Value: json.RawMessage("16384")}})
	waitUntil(t, h, "both edits on both sides", func() bool {
		v := objectsOf(b.View())
		return field(sh.objs, ka, "cpu_cores") == "8" && field(sh.objs, kb, "memory_mib") == "16384" &&
			objJSON(t, v) == objJSON(t, sh.objs) && len(sh.s.Pending()) == 0
	})
	if c.node(ka[2:]).CPUCores != 8 || c.node(kb[2:]).MemoryMiB != 16384 {
		t.Error("the canvas design does not show both edits")
	}

	// the same field: the later op wins and I am told who overwrote mine
	n := len(hub.Ops("D1"))
	c.commit(h.Context(), []archdoc.Change{setOp(ka, "cpu_cores", "4")})
	waitUntil(t, h, "my op sequenced", func() bool { return len(hub.Ops("D1")) > n && len(sh.s.Pending()) == 0 })
	b.Submit([]collab.Op{{Op: collab.OpSet, ID: ka, Field: "cpu_cores", Value: json.RawMessage("16")}})
	waitUntil(t, h, "hana's value on both sides", func() bool {
		return field(sh.objs, ka, "cpu_cores") == "16" && field(objectsOf(b.View()), ka, "cpu_cores") == "16" && sh.lastOverwrite != nil
	})
	if sh.lastOverwrite.By != fixtures.UserHana || sh.lastOverwrite.ID != ka || sh.lastOverwrite.Field != "cpu_cores" {
		t.Errorf("overwritten %+v", *sh.lastOverwrite)
	}
}

func TestOfflineThirtySecondsMerges(t *testing.T) {
	defer collab.SetBackoffForTest(func() float64 { return 0.02 })() // reconnect within ~0.6s
	hub := collabtest.New(t)
	_, ka, kb := sharedFixture(t, hub, api.Doc{})
	h, c, sh := docHarness(t, hub)
	b := peer(t, h, hub, "D1")

	hub.SetOnline(fixtures.UserSam, false)
	offline := h.Context().T.Archcanvas.RoomOffline()
	waitUntil(t, h, "the offline header", func() bool { return !sh.online && strings.Contains(h.Screen(), offline) })
	c.commit(h.Context(), []archdoc.Change{setOp(ka, "cpu_cores", "8")})
	b.Submit([]collab.Op{{Op: collab.OpSet, ID: kb, Field: "memory_mib", Value: json.RawMessage("16384")}})
	h.Advance(30 * time.Second)
	if !strings.Contains(h.Screen(), offline) {
		t.Errorf("not offline after 30s:\n%s", h.Screen())
	}
	if field(sh.objs, ka, "cpu_cores") != "8" {
		t.Error("my offline edit is not on my canvas")
	}
	hub.SetOnline(fixtures.UserSam, true)
	waitUntil(t, h, "both sides merged", func() bool {
		v := objectsOf(b.View())
		return sh.online && len(sh.s.Pending()) == 0 && field(sh.objs, kb, "memory_mib") == "16384" &&
			field(v, ka, "cpu_cores") == "8" && objJSON(t, v) == objJSON(t, sh.objs)
	})
	if strings.Contains(h.Screen(), offline) {
		t.Errorf("still offline after the reconnect:\n%s", h.Screen())
	}
}

func TestRejectDetailToast(t *testing.T) {
	hub := collabtest.New(t)
	_, ka, _ := sharedFixture(t, hub, api.Doc{})
	h, c, sh := docHarness(t, hub)
	was := field(sh.objs, ka, "cpu_cores")
	hub.RejectNext("D1", "invalid", "arch_cycle")
	c.commit(h.Context(), []archdoc.Change{setOp(ka, "cpu_cores", "8")})
	want := h.Context().T.Archcanvas.RejectArchCycle()
	waitUntil(t, h, "the reject toast", func() bool { return shown(h.Screen(), want) })
	if got := field(sh.objs, ka, "cpu_cores"); got != was {
		t.Errorf("the refused edit stayed: cpu_cores %s, want %s", got, was)
	}
	if c.node(ka[2:]).CPUCores == 8 {
		t.Error("the canvas still shows the refused edit")
	}
}

func TestKickedClosesSession(t *testing.T) {
	hub := collabtest.New(t)
	_, ka, _ := sharedFixture(t, hub, api.Doc{})
	h, c, sh := docHarness(t, hub)
	hub.Kick("D1", fixtures.UserSam, "kicked")
	waitUntil(t, h, "kicked", func() bool { return sh.kicked })
	a := h.Context().T.Archcanvas
	if !shown(h.Screen(), a.KickedNotice()) {
		t.Errorf("no kicked notice:\n%s", h.Screen())
	}
	if ro, _ := c.sink.ReadOnly(); !ro {
		t.Error("not read-only after the kick")
	}
	n, sub := len(hub.Ops("D1")), sh.submits
	c.sel = ka[2:]
	h.Press("d")
	c.commit(h.Context(), []archdoc.Change{setOp(ka, "cpu_cores", "8")})
	time.Sleep(50 * time.Millisecond)
	h.Flush()
	if len(hub.Ops("D1")) != n || sh.submits != sub {
		t.Errorf("edits after the kick were sent: ops %d→%d, submits %d→%d", n, len(hub.Ops("D1")), sub, sh.submits)
	}
	if c.node(ka[2:]) == nil {
		t.Error("d deleted a node of a closed document")
	}
	if hub.Conns("D1", fixtures.UserSam) != 0 {
		t.Error("the session reconnected after the kick")
	}
}

func TestDragSendsPresenceThenOneOp(t *testing.T) {
	hub := collabtest.New(t)
	sharedFixture(t, hub, api.Doc{})
	h, c, sh := docHarness(t, hub)
	detail(t, h, c)
	n := visibleNode(t, c)
	x, y := worldScr(t, h, c, n.Pos[0], n.Pos[1])
	ops, sub := len(hub.Ops("D1")), sh.submits
	start := h.Clock().Now()
	h.Send(tea.MouseClickMsg{X: x + 2, Y: y + 1, Button: tea.MouseLeft})
	for i := 1; i <= 20; i++ {
		h.Advance(50 * time.Millisecond)
		h.Send(tea.MouseMotionMsg{X: x + 2 + i%5, Y: y + 1 + i/5, Button: tea.MouseLeft})
	}
	secs := h.Clock().Now().Sub(start).Seconds()
	if sh.presenceSent == 0 {
		t.Fatal("no drag presence")
	}
	if float64(sh.presenceSent) > 10*secs+1 {
		t.Errorf("%d presence frames in %.1fs (over 10 a second)", sh.presenceSent, secs)
	}
	if sh.submits != sub {
		t.Errorf("submitted during the drag: %d", sh.submits-sub)
	}
	h.Send(tea.MouseReleaseMsg{X: x + 2, Y: y + 1 + 4, Button: tea.MouseLeft})
	if sh.submits != sub+1 {
		t.Errorf("%d submits at the release, want 1", sh.submits-sub)
	}
	if c.dragShared {
		t.Error("the drag presence was not cleared at the release")
	}
	key := "n:" + n.ID
	waitUntil(t, h, "the drop sequenced", func() bool { return len(hub.Ops("D1")) > ops })
	got := hub.Ops("D1")[ops:]
	if len(got) != 1 || got[0].ID != key || got[0].Field != "pos" {
		t.Errorf("ops after the drop %+v, want one pos set on %s", got, key)
	}
}

// A drag the app cuts short (an overlay, focus leaving, a lock) clears its
// shared ghost: the release that follows finds no gesture, so it cannot.
func TestDragCancelClearsPresence(t *testing.T) {
	for _, tc := range []struct {
		name   string
		cancel func(h *uitest.Harness, c *canvasModel)
	}{
		{"ClearDrag", func(_ *uitest.Harness, c *canvasModel) { c.ClearDrag() }},
		{"focus", func(h *uitest.Harness, _ *canvasModel) { h.Send(ui.FocusChangedMsg{}) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hub := collabtest.New(t)
			sharedFixture(t, hub, api.Doc{})
			h, c, _ := docHarness(t, hub)
			detail(t, h, c)
			n := visibleNode(t, c)
			b := peer(t, h, hub, "D1")
			samGhost := func() *archdraw.Ghost {
				for _, p := range b.Peers() {
					if p.User == fixtures.UserSam {
						return peerGhost(p)
					}
				}
				return nil
			}
			x, y := worldScr(t, h, c, n.Pos[0], n.Pos[1])
			h.Send(tea.MouseClickMsg{X: x + 2, Y: y + 1, Button: tea.MouseLeft})
			for i := 1; i <= 3; i++ {
				h.Advance(150 * time.Millisecond)
				h.Send(tea.MouseMotionMsg{X: x + 2 + i, Y: y + 1 + i, Button: tea.MouseLeft})
			}
			waitUntil(t, h, "sam's ghost at hana", func() bool { return samGhost() != nil })
			tc.cancel(h, c)
			h.Send(tea.MouseReleaseMsg{X: x + 5, Y: y + 4, Button: tea.MouseLeft})
			if c.dragShared {
				t.Error("the drag presence is still shared after the gesture was cut short")
			}
			waitUntil(t, h, "sam's ghost gone at hana", func() bool { return samGhost() == nil })
		})
	}
}

func TestRemoteDragGhostVisible(t *testing.T) {
	hub := collabtest.New(t)
	sharedFixture(t, hub, api.Doc{})
	h, c, sh := docHarness(t, hub)
	detail(t, h, c)
	n := visibleNode(t, c)
	b := peer(t, h, hub, "D1")
	drag, _ := json.Marshal(dragPresence{ID: n.ID, X: n.Pos[0] + 6, Y: n.Pos[1] + 3})
	b.SetPresence(collab.Presence{Dragging: drag, Selection: []string{n.ID}})
	waitUntil(t, h, "hana's presence", func() bool { return len(sh.peers) > 0 && peerGhost(sh.peers[0]) != nil })
	marks := c.remoteMarks(h.Context())
	if len(marks) != 1 || marks[0].User != "하나" || marks[0].Ghost == nil || marks[0].Ghost.ID != n.ID {
		t.Fatalf("remote marks %+v", marks)
	}
	sx, sy := worldScr(t, h, c, n.Pos[0]+6, n.Pos[1]+3)
	lines := strings.Split(h.Screen(), "\n")
	if sy-1 < 0 || !strings.Contains(lines[sy-1], "하나") {
		t.Errorf("no name tag above hana's ghost at %d,%d:\n%s", sx, sy, h.Screen())
	}
	// in hana's colour (the first peer: sRemote+0)
	if sy-1 >= 0 && !styledRun(strings.Split(h.Styled(), "\n")[sy-1], "하나", h.Context().Theme.P.Acc2) {
		t.Errorf("hana's name tag is not in her colour: %q", strings.Split(h.Styled(), "\n")[sy-1])
	}
	if !strings.Contains(h.Screen(), "sam·하나") {
		t.Errorf("the header does not list both people:\n%s", h.Screen())
	}
	h.Check()
}

func TestLockedReadOnly(t *testing.T) {
	t.Run("GetDoc", func(t *testing.T) {
		hub := collabtest.New(t)
		_, ka, _ := sharedFixture(t, hub, api.Doc{SpaceID: "S_elsewhere", Locked: true, CreatedBy: api.UserSummary{ID: fixtures.UserHana}})
		h, c, sh := docHarness(t, hub)
		waitUntil(t, h, "the lock", func() bool { return sh.locked })
		n := len(hub.Ops("D1"))
		c.sel = ka[2:]
		h.Press("d")
		if !shown(h.Screen(), h.Context().T.Archcanvas.LockedNotice()) {
			t.Errorf("no locked notice:\n%s", h.Screen())
		}
		if sh.submits != 0 || len(hub.Ops("D1")) != n || c.node(ka[2:]) == nil {
			t.Error("a locked document was edited")
		}
	})
	t.Run("reject", func(t *testing.T) {
		hub := collabtest.New(t)
		_, ka, _ := sharedFixture(t, hub, api.Doc{})
		h, c, sh := docHarness(t, hub)
		hub.RejectNext("D1", "locked", "")
		c.commit(h.Context(), []archdoc.Change{setOp(ka, "cpu_cores", "8")})
		waitUntil(t, h, "the lock", func() bool { return sh.locked })
		sub := sh.submits
		c.sel = ka[2:]
		h.Press("d")
		if sh.submits != sub || c.node(ka[2:]) == nil {
			t.Error("edited after a locked reject")
		}
		if !shown(h.Screen(), h.Context().T.Archcanvas.LockedNotice()) {
			t.Errorf("no locked notice:\n%s", h.Screen())
		}
	})
}
