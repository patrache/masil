package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/ui/uitest"
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
