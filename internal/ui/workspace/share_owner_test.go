package workspace

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/termhost"
	"github.com/patrache/masil/internal/ui/termpane/termpanetest"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
	"github.com/patrache/masil/internal/workspace/share"
	"github.com/patrache/masil/internal/workspace/share/sharetest"
	"github.com/patrache/masil/internal/workspace/wire"
)

const rigShare = "ws_01"

// shareRig is the owner's surface over a sharetest.Relay with scripted
// guests, everything on the harness clock (ManualTick).
type shareRig struct {
	t      *testing.T
	h      *uitest.Harness
	s      *Surface
	fake   *termpanetest.Fake
	relay  *sharetest.Relay
	users  map[uint8]api.UserSummary
	guests map[uint8]*share.Guest
	gEv    map[uint8][]share.Event
}

type harnessClock struct{ h *uitest.Harness }

func (c harnessClock) Now() time.Time { return c.h.Clock().Now() }

// newShareRig opens the fixture folder; members are the share's seats (the
// API answer and the relay).
func newShareRig(t *testing.T, h *uitest.Harness, fake *termpanetest.Fake, members map[uint8]api.UserSummary) *shareRig {
	t.Helper()
	ids := map[uint8]string{}
	var ms []api.WorkspaceShareMember
	for seat := uint8(1); seat <= 8; seat++ {
		if u, ok := members[seat]; ok {
			ids[seat] = u.ID
			ms = append(ms, api.WorkspaceShareMember{User: u, Seat: int(seat)})
		}
	}
	r := &shareRig{t: t, h: h, fake: fake, users: members, guests: map[uint8]*share.Guest{}, gEv: map[uint8][]share.Event{}}
	r.relay = sharetest.NewRelay(rigShare, fixtures.UserSam, ids)
	r.relay.SetClock(h.Clock().Now)
	t.Cleanup(SetDialForTest(r.relay.Dial(fixtures.UserSam)))
	h.API.JSON("POST /v1/workspace-shares", 201, map[string]any{"share": api.WorkspaceShare{
		ID: rigShare, Title: "sam의 작업공간", Status: "live",
		Owner: api.UserSummary{ID: fixtures.UserSam, Handle: "sam", DisplayName: "sam"}, Members: ms,
	}})
	h.API.JSON("DELETE /v1/workspace-shares/{id}", 204, nil)
	r.s = openFolder(t, h, "workspace:share", copyFixture(t))
	t.Cleanup(func() { r.s.CloseTool(h.Context()) })
	return r
}

// shareFriends opens the share dialog, ticks the friends by handle and
// starts with enter.
func (r *shareRig) shareFriends(handles ...string) {
	r.t.Helper()
	r.h.Press("ctrl+\\", "S")
	for _, hd := range handles {
		r.pickRow(hd)
		r.h.Press("space")
	}
	r.h.Press("enter")
	r.settle()
	if !r.s.sharing() {
		r.t.Fatalf("not sharing:\n%s", r.h.Screen())
	}
}

// pickRow moves the dialog's selection to the first row containing text.
func (r *shareRig) pickRow(text string) {
	r.t.Helper()
	d := r.s.dlg
	if d == nil {
		r.t.Fatalf("no dialog:\n%s", r.h.Screen())
	}
	ctx := r.h.Context()
	rows := d.build(ctx)
	for i, idx := range d.selectable(rows) {
		if strings.Contains(rows[idx].text, text) || rows[idx].input != nil && strings.Contains(rows[idx].input.label, text) {
			d.sel = i
			return
		}
	}
	r.t.Fatalf("no dialog row %q", text)
}

func (r *shareRig) addGuest(seat uint8) *share.Guest {
	r.t.Helper()
	u := r.users[seat]
	g := share.NewGuest(share.GuestConfig{ShareID: rigShare, OwnerID: fixtures.UserSam, UserID: u.ID,
		Dial: r.relay.Dial(u.ID), Clock: harnessClock{r.h}, ManualTick: true})
	if err := g.Start(context.Background()); err != nil {
		r.t.Fatal(err)
	}
	r.guests[seat] = g
	r.settle()
	return g
}

// settle pumps the relay, the guests and the surface until nothing moves.
func (r *shareRig) settle() {
	r.t.Helper()
	quiet := 0
	for i := 0; i < 300 && quiet < 3; i++ {
		n := r.relay.Pump()
		for seat, g := range r.guests {
			g.Tick(r.h.Clock().Now())
			r.gEv[seat] = append(r.gEv[seat], g.Drain()...)
		}
		r.h.Advance(pumpEvery)
		r.h.Flush()
		if n == 0 {
			quiet++
		} else {
			quiet = 0
		}
	}
}

// join lets seat in at level through the check-number dialog.
func (r *shareRig) join(seat uint8, level string, verified bool) {
	r.t.Helper()
	g := r.addGuest(seat)
	if r.s.dlg == nil || !strings.Contains(r.h.Screen(), formatSAS(g.SAS())) {
		r.t.Fatalf("no check-number dialog with %q:\n%s", g.SAS(), r.h.Screen())
	}
	r.pickRow(level)
	r.h.Press("space")
	if verified {
		r.pickRow("일치 · 허용")
	} else {
		r.pickRow("확인 없이 허용")
	}
	r.h.Press("enter")
	if verified {
		if err := g.Confirm(true); err != nil {
			r.t.Fatal(err)
		}
	}
	r.settle()
}

func (r *shareRig) shareRequests() []map[string]any {
	var out []map[string]any
	for _, q := range r.h.API.Requests() {
		if q.Method == "POST" && q.Path == "/v1/workspace-shares" {
			var m map[string]any
			_ = json.Unmarshal(q.Body, &m)
			out = append(out, m)
		}
	}
	return out
}

func TestShareStartsOnlyOnEnter(t *testing.T) {
	fake := setup(t)
	h := uitest.New(t)
	r := newShareRig(t, h, fake, map[uint8]api.UserSummary{1: {ID: fixtures.UserMinji, Handle: "minji", DisplayName: "민지"}})
	h.Press("ctrl+\\", "S")
	if r.s.dlg == nil || !strings.Contains(h.Screen(), "공유 시작") {
		t.Fatalf("no share dialog:\n%s", h.Screen())
	}
	if n := len(r.shareRequests()); n != 0 {
		t.Fatalf("opening the dialog sent %d requests", n)
	}
	h.Press("esc")
	if r.s.dlg != nil || len(r.shareRequests()) != 0 || r.s.sharing() {
		t.Fatalf("esc started something: dlg %v, requests %d", r.s.dlg != nil, len(r.shareRequests()))
	}
	r.shareFriends("minji")
	reqs := r.shareRequests()
	if len(reqs) != 1 {
		t.Fatalf("POST count = %d", len(reqs))
	}
	if reqs[0]["title"] != "sam의 작업공간" {
		t.Errorf("title = %v", reqs[0]["title"])
	}
	if users, _ := reqs[0]["users"].([]any); len(users) != 1 || users[0] != "@minji" {
		t.Errorf("users = %v", reqs[0]["users"])
	}
	if !strings.Contains(h.Screen(), "🔴 공유 중 · 0명") {
		t.Fatalf("no sharing band:\n%s", h.Screen())
	}

	// A restart never brings the share back.
	h2 := uitest.New(t)
	openFolder(t, h2, "workspace:share", r.s.dir)
	if !strings.Contains(h2.Screen(), "🔒 나만 보기") {
		t.Fatalf("after restart:\n%s", h2.Screen())
	}
	for _, q := range h2.API.Requests() {
		if strings.Contains(q.Path, "workspace-shares") {
			t.Errorf("restart sent %s %s", q.Method, q.Path)
		}
	}
}

func TestBypassProgramCannotBeShared(t *testing.T) {
	fake := setup(t)
	h := uitest.New(t)
	r := newShareRig(t, h, fake, nil)
	h.Send(launchMsgForTest(r.s, []string{"claude", "--dangerously-skip-permissions"}))
	if r.s.term.count() != 1 {
		t.Fatalf("terminals = %d", r.s.term.count())
	}
	h.Press("ctrl+\\", "1", "ctrl+\\", "S")
	rows := r.s.dlg.build(h.Context())
	found := false
	for _, row := range rows {
		if strings.Contains(row.text, "claude") && row.checked != nil {
			found = true
			if !row.disabled || *row.checked {
				t.Errorf("bypass terminal row selectable: %+v", row)
			}
		}
	}
	if !found {
		t.Fatalf("no terminal row:\n%s", h.Screen())
	}
	if !strings.Contains(flat(h.Screen()), flat("권한 확인을 건너뛰는 모드")) {
		// the reason may be below the fold at this size; it must be in the dialog
		all := ""
		for _, row := range rows {
			all += row.text
		}
		if !strings.Contains(all, "권한 확인을 건너뛰는 모드") {
			t.Errorf("no block reason in the dialog")
		}
	}
	h.Press("enter")
	r.settle()
	if !r.s.sharing() {
		t.Fatal("files-only share did not start")
	}
	if n := len(r.s.own.terms); n != 0 {
		t.Fatalf("shared terminals = %d", n)
	}
}

func TestJoinApprovalFlow(t *testing.T) {
	fake := setup(t)
	h := uitest.New(t)
	r := newShareRig(t, h, fake, map[uint8]api.UserSummary{1: {ID: fixtures.UserMinji, Handle: "minji", DisplayName: "민지"}})
	r.shareFriends("minji")
	g := r.addGuest(1)
	scr := h.Screen()
	if !strings.Contains(scr, "민지님이 참가하려고 해요") {
		t.Errorf("no join toast:\n%s", scr)
	}
	if g.SAS() == "" || !strings.Contains(scr, g.SAS()) {
		t.Fatalf("check number %q not shown:\n%s", g.SAS(), scr)
	}
	r.pickRow("편집")
	h.Press("space")
	r.pickRow("일치 · 허용")
	h.Press("enter")
	if err := g.Confirm(true); err != nil {
		t.Fatal(err)
	}
	r.settle()
	if !strings.Contains(h.Screen(), "minji[편집]") {
		t.Fatalf("band:\n%s", h.Screen())
	}
	if lv := r.relay.Levels()[1]; lv != wire.LevelEditor {
		t.Errorf("level = %v", lv)
	}
}

func TestSASMismatchWarns(t *testing.T) {
	fake := setup(t)
	h := uitest.New(t)
	r := newShareRig(t, h, fake, map[uint8]api.UserSummary{1: {ID: fixtures.UserMinji, Handle: "minji", DisplayName: "민지"}})
	r.shareFriends("minji")
	r.addGuest(1)
	r.pickRow("다름")
	h.Press("enter")
	r.settle()
	if !strings.Contains(flat(h.Screen()), flat("중간에서 누가 가로챘을 수 있어요")) {
		t.Fatalf("no MITM warning:\n%s", h.Screen())
	}
	if len(r.s.own.host.Members()) != 0 {
		t.Fatal("mismatch let someone in")
	}
}

var junUser = api.UserSummary{ID: "01JA00000000000000000000JN", Handle: "jun", DisplayName: "Jun\u200b"}

func TestKeyboardConfirmRequiresHandle(t *testing.T) {
	fake := setup(t)
	h := uitest.New(t)
	h.Resize(200, 60)
	r := newShareRig(t, h, fake, map[uint8]api.UserSummary{1: junUser})
	r.shareFriends()
	r.join(1, "편집", true)
	h.Press("ctrl+\\", "4", "k")
	if r.s.dlg == nil {
		t.Fatalf("no keyboard dialog:\n%s", h.Screen())
	}
	r.pickRow("핸들")
	h.Type("Jun\u200b")
	h.Press("enter")
	if m := r.s.own.host.Members(); m[0].Level == wire.LevelKeyboard {
		t.Fatal("the display name granted the keyboard")
	}
	h.Press("ctrl+u")
	h.Type("jun")
	h.Press("enter")
	m := r.s.own.host.Members()
	if m[0].Level != wire.LevelKeyboard || m[0].KeyboardUntil.Sub(h.Clock().Now()) != share.DefaultKeyboard {
		t.Fatalf("member = %+v", m[0])
	}
	if !strings.Contains(h.Screen(), "jun[키보드 30:00]") {
		t.Fatalf("band:\n%s", h.Screen())
	}
	h.Advance(10 * time.Minute)
	if !strings.Contains(h.Screen(), "jun[키보드 20:00]") {
		t.Fatalf("band after 10m:\n%s", h.Screen())
	}
}

func TestKeyboardWarningNeverTruncated(t *testing.T) {
	setup(t)
	sizes := append(append([]uitest.Size(nil), uitest.Required...), uitest.Tiny)
	uitest.ForEachSize(t, sizes, func(t *testing.T, h *uitest.Harness) {
		s := openFolder(t, h, "workspace:kbd", copyFixture(t))
		s.dlg = newKeyboardDialog(s, h.Context(), peerInfo{seat: 1, userID: junUser.ID, handle: "jun", name: "Jun"})
		want := flat(h.Context().T.Workspace.KeyboardWarning("Jun", "sam"))
		if body := flat(strings.Join(s.dlg.bodyText(h.Context(), s.size.W), "")); !strings.Contains(body, want) {
			t.Fatalf("warning cut in the dialog body at %dx%d", s.size.W, s.size.H)
		}
		// Every part of it can be scrolled into view.
		seen := ""
		for range 40 {
			seen += flat(h.Screen())
			h.Press("pgdown")
		}
		lines := s.dlg.bodyText(h.Context(), s.size.W)
		for _, l := range lines {
			if f := flat(l); f != "" && !strings.Contains(seen, f) {
				t.Fatalf("line never shown: %q", l)
			}
		}
	})
}

func TestOwnerActivityFeedsHost(t *testing.T) {
	fake := setup(t)
	h := uitest.New(t)
	r := newShareRig(t, h, fake, nil)
	r.shareFriends()
	var got []time.Time
	t.Cleanup(SetOwnerActivityForTest(func(_ *share.Host, now time.Time) { got = append(got, now) }))
	h.Press("ctrl+\\", "1", "j")
	if len(got) == 0 || !got[len(got)-1].Equal(h.Clock().Now()) {
		t.Fatalf("owner activity = %v", got)
	}
}

// launchMsgForTest starts argv in s's terminal cell (as the new-terminal list would).
func launchMsgForTest(s *Surface, argv []string) launchTermMsg {
	return launchTermMsg{surface: s.id, argv: argv, mode: termhost.ModePane}
}
