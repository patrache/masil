package workspace

import (
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/ui/termpane/termpanetest"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/workspace/share"
)

// fakeTerm is a shared terminal that records what was typed into it.
type fakeTerm struct {
	mu        sync.Mutex
	bracketed bool
	injected  []byte
}

func (f *fakeTerm) Tap(func([]byte)) func() { return func() {} }
func (f *fakeTerm) Snapshot() []byte        { return []byte("$ ") }
func (f *fakeTerm) Inject(p []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.injected = append(f.injected, p...)
	return nil
}
func (f *fakeTerm) Size() (int, int)      { return 80, 24 }
func (f *fakeTerm) SetFixedSize(int, int) {}
func (f *fakeTerm) BracketedPaste() bool  { return f.bracketed }
func (f *fakeTerm) Title() string         { return "" }
func (f *fakeTerm) typed() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return string(f.injected)
}

// sharedTermRig starts a share with one shared fake terminal and minji
// let in at level.
func sharedTermRig(t *testing.T, h *uitest.Harness, fake *termpanetest.Fake, level string, bracketed bool, argv ...string) (*shareRig, *fakeTerm, *share.Guest) {
	t.Helper()
	ft := &fakeTerm{bracketed: bracketed}
	t.Cleanup(SetTerminalForTest(func(string) share.Terminal { return ft }))
	r := newShareRig(t, h, fake, map[uint8]api.UserSummary{1: minjiUser})
	if len(argv) == 0 {
		argv = []string{"sh"}
	}
	h.Send(launchMsgForTest(r.s, argv))
	h.Press("ctrl+\\", "1")
	r.shareFriends("minji")
	if len(r.s.own.terms) != 1 {
		t.Fatalf("shared terminals = %d", len(r.s.own.terms))
	}
	r.join(1, level, level == "편집")
	return r, ft, r.guests[1]
}

func (r *shareRig) propose(g *share.Guest, text string) {
	r.t.Helper()
	if _, err := g.Propose(text); err != nil {
		r.t.Fatal(err)
	}
	r.settle()
}

func TestProposalInsertSend(t *testing.T) {
	fake := setup(t)
	h := uitest.New(t)
	r, ft, g := sharedTermRig(t, h, fake, "제안", true)
	r.propose(g, "npm test")
	if !strings.Contains(h.Screen(), "제안 1건 대기") {
		t.Fatalf("no waiting proposal in the band:\n%s", h.Screen())
	}
	h.Press("ctrl+\\", "enter")
	if r.s.dlg == nil || !strings.Contains(h.Screen(), "npm test") {
		t.Fatalf("no proposal queue:\n%s", h.Screen())
	}
	h.Press("s")
	r.settle()
	if got := ft.typed(); got != "\x1b[200~npm test\x1b[201~\r" {
		t.Fatalf("injected %q", got)
	}
	if n := len(r.s.own.host.Proposals()); n != 0 {
		t.Fatalf("still waiting: %d", n)
	}
}

func TestProposalMultilineConfirm(t *testing.T) {
	fake := setup(t)
	h := uitest.New(t)
	r, ft, g := sharedTermRig(t, h, fake, "제안", false)
	r.propose(g, "echo a\necho b")
	h.Press("ctrl+\\", "enter", "i")
	if ft.typed() != "" || !strings.Contains(flat(h.Screen()), flat("줄마다 따로 실행될 수 있어요")) {
		t.Fatalf("no multi-line confirmation (typed %q):\n%s", ft.typed(), h.Screen())
	}
	h.Press("enter")
	if got := ft.typed(); got != "echo a\necho b" {
		t.Fatalf("injected %q", got)
	}
}

func TestProposalEditAndReject(t *testing.T) {
	fake := setup(t)
	h := uitest.New(t)
	r, ft, g := sharedTermRig(t, h, fake, "제안", true)
	r.propose(g, "rm -rf /")
	r.propose(g, "ls")
	h.Press("ctrl+\\", "enter", "r")
	if n := len(r.s.own.host.Proposals()); n != 1 || ft.typed() != "" {
		t.Fatalf("after reject: %d waiting, typed %q", n, ft.typed())
	}
	h.Press("e", "ctrl+u")
	h.Type("ls -la")
	h.Press("enter")
	if got := ft.typed(); got != "\x1b[200~ls -la\x1b[201~" {
		t.Fatalf("injected %q", got)
	}
}

func TestAttentionFromHook(t *testing.T) {
	fake := setup(t)
	h := uitest.New(t)
	r, _, _ := sharedTermRig(t, h, fake, "보기", true, "claude")
	if r.s.hooks == nil {
		t.Fatal("no hook receiver for claude")
	}
	resp, err := http.Post(r.s.hooks.URL("1"), "application/json", strings.NewReader(`{"hook_event_name":"PermissionRequest"}`))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if len(body) != 0 {
		t.Errorf("the hook got an answer: %q", body)
	}
	h.WaitPosted(1, 2*time.Second)
	h.Flush()
	r.settle()
	if !strings.Contains(h.Screen(), "⏳ 승인 대기") {
		t.Fatalf("no attention mark:\n%s", h.Screen())
	}
	if n := r.relay.Attention(); n != 1 {
		t.Fatalf("ctl_attention sent %d times", n)
	}
	found := false
	for _, ev := range r.gEv[1] {
		if st, ok := ev.(share.EventTermStatus); ok && st.Attention {
			found = true
		}
	}
	if !found {
		t.Error("the participant did not see the attention")
	}
}

func TestAttentionFromBell(t *testing.T) {
	fake := setup(t)
	h := uitest.New(t)
	r, _, _ := sharedTermRig(t, h, fake, "보기", true)
	h.Send(fake.Notify(r.s.term.terms[0].id, "bel"))
	r.settle()
	if !strings.Contains(h.Screen(), "⏳ 승인 대기") || r.relay.Attention() != 1 {
		t.Fatalf("bell: attention %d\n%s", r.relay.Attention(), h.Screen())
	}
}
