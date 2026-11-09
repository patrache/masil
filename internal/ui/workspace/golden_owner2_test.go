package workspace

import (
	"fmt"
	"testing"

	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/workspace/ot"
)

// TestGoldenOwner2 builds each scene at 120x40 (a pane needs a layout that
// is not tiny) and draws it at the size under test.
func TestGoldenOwner2(t *testing.T) {
	fake := setup(t)
	for _, sz := range uitest.Required {
		t.Run(fmt.Sprintf("%dx%d", sz.W, sz.H), func(t *testing.T) {
			h := uitest.New(t)
			r, _, g := sharedTermRig(t, h, fake, "편집", true)
			h.Resize(sz.W, sz.H)
			r.propose(g, "npm test")
			r.propose(g, "git status\ngit diff --stat")
			h.Press("ctrl+\\", "enter")
			h.Golden("proposals_overlay")
			h.Press("esc")
			if err := g.OpenDoc("README.md"); err != nil {
				t.Fatal(err)
			}
			r.settle()
			text, _ := g.DocText("README.md")
			if err := g.Edit("README.md", ot.Insert(0, "Z", len([]rune(text)))); err != nil {
				t.Fatal(err)
			}
			r.settle()
			h.Send(wsCommandMsg{surface: r.s.id, action: "share.end"})
			h.Golden("end_unsaved")
		})
	}
	for _, sz := range []uitest.Size{{W: 50, H: 20}, uitest.Tiny} {
		t.Run(fmt.Sprintf("tiny/%dx%d", sz.W, sz.H), func(t *testing.T) {
			h := uitest.New(t)
			r, _, g := sharedTermRig(t, h, fake, "제안", true)
			h.Resize(sz.W, sz.H)
			r.propose(g, "npm test")
			h.Golden("tiny_owner")
		})
	}
}
