package workspace

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/termpane/termpanetest"
	_ "github.com/patrache/masil/internal/ui/tools" // the RouteTool factory
	"github.com/patrache/masil/internal/ui/uitest"
)

const hubTS = "src/server/hub.ts"

// copyFixture copies testdata/proj to <temp>/proj and adds node_modules
// (git-ignored in this repository).
func copyFixture(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "proj")
	err := filepath.WalkDir("testdata/proj", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel("testdata/proj", p)
		dst := filepath.Join(dir, rel)
		if d.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "node_modules", "x.js"), []byte("module.exports = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// setup turns the workspace flag on and fakes the terminal launcher; call it
// before uitest.New.
func setup(t *testing.T) *termpanetest.Fake {
	t.Helper()
	t.Setenv("MASIL_FEATURES", "workspace")
	return termpanetest.Install(t)
}

func openRoute(id, dir string) ui.Route {
	return ui.Route{Kind: ui.RouteTool, ID: id, Arg: ui.ToolWorkspace, Query: map[string]string{"open": dir}}
}

// openFolder opens dir as instance id and returns its surface.
func openFolder(t *testing.T, h *uitest.Harness, id, dir string) *Surface {
	t.Helper()
	r := openRoute(id, dir)
	h.Send(ui.NavigateMsg{Route: r})
	s, ok := h.Instance(r.Key()).(*Surface)
	if !ok {
		t.Fatalf("no workspace surface for %s (got %T)", r.Key(), h.Instance(r.Key()))
	}
	return s
}

// flat is the screen without line breaks and spaces (wrapping-proof search).
func flat(s string) string {
	return strings.NewReplacer("\n", "", " ", "", "│", "").Replace(s)
}
