package share_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/patrache/masil/internal/workspace/audit"
	"github.com/patrache/masil/internal/workspace/share"
	"github.com/patrache/masil/internal/workspace/wire"
)

func treeNames(evs []share.Event, dir string) ([]string, wire.TreeList) {
	for _, e := range evs {
		if tl, ok := e.(share.EventTree); ok && tl.Dir == dir {
			var names []string
			for _, en := range tl.Entries {
				names = append(names, en.Name)
			}
			return names, tl.TreeList
		}
	}
	return nil, wire.TreeList{Error: "no answer"}
}

func TestTreeForPeer(t *testing.T) {
	d := newDocRig(t, wire.LevelViewer, 1)
	g := d.guests[1]
	must(t, g.ListDir("."))
	must(t, g.ListDir("src"))
	must(t, g.ListDir(".git"))
	must(t, g.ListDir("../"))
	d.settle()
	evs := d.takeGuest(1)

	top, tl := treeNames(evs, ".")
	if tl.Error != "" {
		t.Fatalf("list . = %#v", tl)
	}
	for _, hidden := range []string{".env", ".git", "node_modules", ".gitignore", "id_rsa", "link_env"} {
		if slices.Contains(top, hidden) {
			t.Errorf("%s listed for a peer: %v", hidden, top)
		}
	}
	for _, shown := range []string{"README.md", "src"} {
		if !slices.Contains(top, shown) {
			t.Errorf("%s missing: %v", shown, top)
		}
	}
	if src, tl := treeNames(evs, "src"); tl.Error != "" || !slices.Contains(src, "server") || !slices.Contains(src, "index.ts") {
		t.Fatalf("list src = %v %#v", src, tl)
	}
	if _, tl := treeNames(evs, ".git"); tl.Error != share.DocErrDenied || len(tl.Entries) != 0 {
		t.Fatalf("list .git = %#v", tl)
	}
	if _, tl := treeNames(evs, "../"); tl.Error != share.DocErrEscape {
		t.Fatalf("list ../ = %#v", tl)
	}
	if !d.auditHas(audit.EvPathViolation, dataIs("rel", "../")) {
		t.Fatalf("escape not recorded: %#v", d.auditEntries())
	}
}

func TestViewerCannotRequestFs(t *testing.T) {
	d := newDocRig(t, wire.LevelViewer, 1)
	if err := d.guests[1].RequestFs(wire.FsCreate, "x.md", ""); !errors.Is(err, share.ErrLevel) {
		t.Fatalf("viewer RequestFs = %v", err)
	}
	if err := d.guests[1].Save(hubTS); !errors.Is(err, share.ErrLevel) {
		t.Fatalf("viewer Save = %v", err)
	}
}
