package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/workspace/ot"
)

func TestEndWithUnsavedPeerEdits(t *testing.T) {
	fake := setup(t)
	h := uitest.New(t)
	r, ft, g := sharedTermRig(t, h, fake, "편집", true)
	if err := g.OpenDoc("README.md"); err != nil {
		t.Fatal(err)
	}
	r.settle()
	text, ok := g.DocText("README.md")
	if !ok {
		t.Fatal("guest has no README.md")
	}
	if err := g.Edit("README.md", ot.Insert(0, "Z", len([]rune(text)))); err != nil {
		t.Fatal(err)
	}
	r.settle()
	h.Send(wsCommandMsg{surface: r.s.id, action: "share.end"})
	scr := h.Screen()
	if r.s.dlg == nil || !strings.Contains(scr, "README.md") || !strings.Contains(flat(scr), flat("저장 / 버리기 / 열어 두기(혼자 계속)")) {
		t.Fatalf("no end dialog with the file:\n%s", scr)
	}
	r.pickRow("저장")
	h.Press("enter")
	r.settle()
	b, err := os.ReadFile(filepath.Join(r.s.dir, "README.md"))
	if err != nil || !strings.HasPrefix(string(b), "Z") {
		t.Fatalf("README.md = %.20q, %v", b, err)
	}
	if r.s.sharing() || !strings.Contains(h.Screen(), "🔒 나만 보기") {
		t.Fatalf("still sharing:\n%s", h.Screen())
	}
	if !r.relay.Ended() {
		t.Error("the relay did not see the end")
	}
	if ft.typed() != "" {
		t.Errorf("the program got %q", ft.typed())
	}
	if r.s.term.count() != 1 {
		t.Error("the terminal went away")
	}
}

func TestEndStopsProgramsWhenAsked(t *testing.T) {
	fake := setup(t)
	h := uitest.New(t)
	r, ft, _ := sharedTermRig(t, h, fake, "보기", true)
	h.Send(wsCommandMsg{surface: r.s.id, action: "share.end"})
	r.pickRow("중단 신호")
	h.Press("space")
	r.pickRow("공유 끝내기")
	h.Press("enter")
	r.settle()
	if ft.typed() != "\x03" || r.s.sharing() {
		t.Fatalf("typed %q, sharing %v", ft.typed(), r.s.sharing())
	}
}

func TestTinyOwnerSummary(t *testing.T) {
	fake := setup(t)
	h := uitest.New(t)
	r, ft, g := sharedTermRig(t, h, fake, "제안", true)
	h.Resize(uitest.Tiny.W, uitest.Tiny.H)
	r.propose(g, "npm test")
	scr := h.Screen()
	for _, want := range []string{"🔴 공유 중 1명", "제안 1", "npm test", "x 공유 끝"} {
		if !strings.Contains(scr, want) {
			t.Fatalf("tiny lacks %q:\n%s", want, scr)
		}
	}
	h.Press("s")
	r.settle()
	if got := ft.typed(); got != "\x1b[200~npm test\x1b[201~\r" {
		t.Fatalf("injected %q", got)
	}
	h.Press("x")
	if r.s.dlg == nil {
		t.Fatalf("x did not ask to end:\n%s", h.Screen())
	}
}
