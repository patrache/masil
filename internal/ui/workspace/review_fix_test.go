package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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
