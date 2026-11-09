package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/palette"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/workspace/share"
)

// A file delete request in the tiny summary is named as one and needs its
// own key: s and i (type a proposal) never delete a file.
func TestTinyFsRequestNeedsApproveKey(t *testing.T) {
	fake := setup(t)
	h := uitest.New(t)
	r, _, g := sharedTermRig(t, h, fake, "편집", true)
	if err := g.RequestFs("delete", "README.md", ""); err != nil {
		t.Fatal(err)
	}
	r.settle()
	h.Resize(uitest.Tiny.W, uitest.Tiny.H)
	scr := h.Screen()
	if !strings.Contains(scr, "지우기") || !strings.Contains(scr, "README.md") {
		t.Fatalf("the delete request is not named as one:\n%s", scr)
	}
	if !strings.Contains(flat(scr), flat("a 허용")) {
		t.Fatalf("no approve key in the hint:\n%s", scr)
	}
	path := filepath.Join(r.s.dir, "README.md")
	for _, k := range []string{"s", "i", "enter"} {
		h.Press(k)
		r.settle()
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("%q deleted the file: %v", k, err)
		}
	}
	h.Press("a")
	r.settle()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("a did not carry out the request: %v", err)
	}
}

// enter in the proposal queue never runs a proposal: typing needs i or s.
func TestProposalQueueEnterDoesNotRun(t *testing.T) {
	fake := setup(t)
	h := uitest.New(t)
	r, ft, g := sharedTermRig(t, h, fake, "제안", true)
	r.propose(g, "rm -rf build")
	h.Press("ctrl+\\", "enter")
	if r.s.dlg == nil || !r.s.dlg.queue {
		t.Fatalf("no proposal queue:\n%s", h.Screen())
	}
	h.Press("enter")
	r.settle()
	if got := ft.typed(); got != "" {
		t.Fatalf("enter injected %q", got)
	}
	if n := len(r.s.own.host.Proposals()); n != 1 {
		t.Fatalf("waiting proposals = %d", n)
	}
}

// A join request never takes the owner's keys: it waits (toast, band) until
// the owner opens the check-number dialog on purpose.
func TestJoinRequestWaitsForOwner(t *testing.T) {
	fake := setup(t)
	h := uitest.New(t)
	r := newShareRig(t, h, fake, map[uint8]api.UserSummary{1: minjiUser})
	r.shareFriends("minji")
	h.Press("ctrl+\\", "1")
	g := r.addGuest(1)
	if r.s.dlg != nil {
		t.Fatalf("the join request opened a dialog by itself:\n%s", h.Screen())
	}
	if !strings.Contains(h.Screen(), "참가 요청 1건") {
		t.Fatalf("no waiting join request in the band:\n%s", h.Screen())
	}
	h.Press("j", "j", "j", "j", "enter", "space", "enter")
	r.settle()
	if n := len(r.s.own.host.Members()); n != 0 {
		t.Fatalf("tree keys let %d people in", n)
	}
	h.Press("esc")
	h.Press("ctrl+\\", "enter")
	if r.s.dlg == nil || !strings.Contains(h.Screen(), g.SAS()) {
		t.Fatalf("ctrl+\\ enter did not open the check-number dialog:\n%s", h.Screen())
	}
}

// The band names the key that really opens the queue.
func TestProposalBandNamesRealKey(t *testing.T) {
	fake := setup(t)
	h := uitest.New(t)
	h.Resize(200, 60)
	r, _, g := sharedTermRig(t, h, fake, "제안", true)
	r.propose(g, "npm test")
	if !strings.Contains(h.Screen(), "제안 1건 대기(ctrl+\\ enter 보기)") {
		t.Fatalf("band:\n%s", h.Screen())
	}
}

// A join request rings the terminal bell with its toast (B6.3 #2).
func TestJoinRequestRingsBell(t *testing.T) {
	fake := setup(t)
	h := uitest.New(t)
	bells := 0
	t.Cleanup(CountBellsForTest(&bells))
	r := newShareRig(t, h, fake, map[uint8]api.UserSummary{1: minjiUser})
	r.shareFriends("minji")
	r.addGuest(1)
	if bells != 1 {
		t.Fatalf("bells = %d", bells)
	}
}

// The audit log lives under the config dir (B8.5: ~/.config/masil/workspace-audit),
// not the OS cache that cleanup tools empty.
func TestAuditLogInConfigDir(t *testing.T) {
	fake := setup(t)
	h := uitest.New(t)
	r := newShareRig(t, h, fake, nil)
	r.shareFriends()
	paths := h.Context().Paths
	if _, err := os.Stat(filepath.Join(paths.Dir, "workspace-audit", rigShare+".jsonl")); err != nil {
		t.Fatalf("no audit log in the config dir: %v", err)
	}
	if paths.Cache != "" {
		if _, err := os.Stat(filepath.Join(paths.Cache, "workspace-audit")); err == nil {
			t.Fatal("an audit log went to the cache dir")
		}
	}
}

// A share without files leaves the documents to the UI's pump: outside
// changes still merge while it runs.
func TestFilesOffShareStillTicksDocs(t *testing.T) {
	fake := setup(t)
	h := uitest.New(t)
	r := newShareRig(t, h, fake, nil)
	h.Press("j", "enter", "j", "enter", "j", "enter")
	d := r.s.code.current()
	if d == nil || d.rel != hubTS {
		t.Fatalf("hub.ts not open:\n%s", h.Screen())
	}
	h.Press("ctrl+\\", "S")
	r.pickRow("파일")
	h.Press("space")
	h.Press("enter")
	r.settle()
	if !r.s.sharing() || r.s.own.files {
		t.Fatalf("no files-off share: sharing %v", r.s.sharing())
	}
	path := filepath.Join(r.s.dir, hubTS)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append([]byte("// outside\n"), b...), 0o644); err != nil {
		t.Fatal(err)
	}
	for range 60 {
		h.Advance(pumpEvery)
		h.Flush()
		r.relay.Pump()
	}
	if !strings.HasPrefix(d.ed.Text(), "// outside\n") {
		t.Fatalf("the outside change did not merge: %.40q", d.ed.Text())
	}
}

// recTerm records the taps and the fixed sizes a share sets on a terminal.
type recTerm struct {
	fakeTerm
	taps, stops int
	sizes       [][2]int
}

func (r *recTerm) Tap(func([]byte)) func() {
	r.taps++
	return func() { r.stops++ }
}
func (r *recTerm) SetFixedSize(c, rows int) { r.sizes = append(r.sizes, [2]int{c, rows}) }

// Ending a share hands every shared terminal back: the tap is stopped and
// the fixed size released.
func TestShareEndReleasesTerminals(t *testing.T) {
	fake := setup(t)
	h := uitest.New(t)
	rt := &recTerm{}
	t.Cleanup(SetTerminalForTest(func(string) share.Terminal { return rt }))
	r := newShareRig(t, h, fake, nil)
	h.Send(launchMsgForTest(r.s, []string{"sh"}))
	h.Press("ctrl+\\", "1", "ctrl+\\", "S")
	r.pickRow("80x24")
	h.Press("space", "enter")
	r.settle()
	if rt.taps != 1 || len(rt.sizes) == 0 || rt.sizes[len(rt.sizes)-1] != [2]int{80, 24} {
		t.Fatalf("share: taps %d, sizes %v", rt.taps, rt.sizes)
	}
	h.Send(wsCommandMsg{surface: r.s.id, action: "share.end"})
	r.pickRow("공유 끝내기")
	h.Press("enter")
	r.settle()
	if r.s.sharing() {
		t.Fatal("still sharing")
	}
	if rt.stops != rt.taps || len(rt.sizes) == 0 || rt.sizes[len(rt.sizes)-1] != [2]int{0, 0} {
		t.Fatalf("after the end: taps %d, stops %d, sizes %v", rt.taps, rt.stops, rt.sizes)
	}
}

// Closing the workspace while a share is starting ends that share, whether
// the start finishes before or after the close.
func TestCloseDuringShareStartEndsIt(t *testing.T) {
	for _, closeFirst := range []bool{true, false} {
		t.Run(map[bool]string{true: "close-then-start", false: "start-then-close"}[closeFirst], func(t *testing.T) {
			fake := setup(t)
			h := uitest.New(t)
			r := newShareRig(t, h, fake, nil)
			ctx := h.Context()
			start := r.s.startShare(ctx, shareOpts{title: "t", files: true, terms: map[string]bool{}})
			if start == nil {
				t.Fatal("no start")
			}
			var msg tea.Msg
			if !closeFirst {
				msg = start()
			}
			h.Send(ui.CloseTool(r.s.id)()) // the app drops the instance
			if h.Instance(openRoute(r.s.id, r.s.dir).Key()) != nil {
				t.Fatal("the instance is still open")
			}
			if closeFirst {
				msg = start()
			}
			r.relay.Pump()
			if msg != nil {
				h.Send(msg)
			}
			r.relay.Pump()
			if m, ok := msg.(shareStartedMsg); ok && (m.host == nil || !m.host.Killed()) {
				t.Fatalf("the share outlived the closed workspace: %+v", m)
			}
			if !r.relay.Ended() {
				t.Fatal("the relay never saw the share end")
			}
			if r.s.own != nil {
				t.Fatal("a closed surface installed the share")
			}
		})
	}
}

// The band names the share (its server-visible title, sanitized).
func TestBandShowsShareTitle(t *testing.T) {
	fake := setup(t)
	h := uitest.New(t)
	r := newShareRig(t, h, fake, nil)
	r.shareFriends()
	if !strings.Contains(h.Screen(), "🔴 공유 중 · 0명 · sam의 작업공간") {
		t.Fatalf("band:\n%s", h.Screen())
	}
	r.s.own.title = "evil\x1b[31m‮title"
	if scr := h.Screen(); strings.Contains(scr, "‮") || !strings.Contains(scr, "title") {
		t.Fatalf("title not sanitized:\n%q", scr)
	}
}

// ws.share.end and ws.proposals are offered only while the workspace in
// front shares; ws.share.start only while it does not.
func TestPaletteCommandsFollowSharing(t *testing.T) {
	fake := setup(t)
	h := uitest.New(t)
	r := newShareRig(t, h, fake, nil)
	avail := func(id string) bool {
		c, ok := palette.Lookup(id)
		if !ok {
			t.Fatalf("no %s", id)
		}
		return c.Available(h.Context())
	}
	if !avail("ws.share.start") || avail("ws.share.end") || avail("ws.proposals") {
		t.Fatalf("solo: start %v end %v proposals %v", avail("ws.share.start"), avail("ws.share.end"), avail("ws.proposals"))
	}
	r.shareFriends()
	if avail("ws.share.start") || !avail("ws.share.end") || !avail("ws.proposals") {
		t.Fatalf("sharing: start %v end %v proposals %v", avail("ws.share.start"), avail("ws.share.end"), avail("ws.proposals"))
	}
}
