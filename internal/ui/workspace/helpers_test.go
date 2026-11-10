package workspace

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

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
// before uitest.New. Embedded panes are on whatever the OS: the panes are
// termpanetest fakes (no PTY or ConPTY), so the terminal and sharing logic
// and the shared goldens are the same on every OS. What Windows does instead
// (no pane, the notice, no terminal sharing) is tested on every OS by the
// tests that call SetPaneSupportedForTest(false) after setup
// (TestWindowsTerminalNotice, TestWindowsShareDialogNoTerminals,
// TestWindowsGuestCanWatch, TestGoldenSolo/windows).
func setup(t *testing.T) *termpanetest.Fake {
	t.Helper()
	t.Cleanup(SetPaneSupportedForTest(true))
	t.Setenv("MASIL_FEATURES", "workspace")
	forgetSurfaces()
	t.Cleanup(forgetSurfaces)
	return termpanetest.Install(t)
}

// forgetSurfaces drops the surfaces and shares a test left open (one
// process runs many harnesses; the real app has one).
func forgetSurfaces() {
	liveMu.Lock()
	live = nil
	liveMu.Unlock()
	guard.mu.Lock()
	clear(guard.hosts)
	guard.mu.Unlock()
}

func openRoute(id, dir string) ui.Route {
	return ui.Route{Kind: ui.RouteTool, ID: id, Arg: ui.ToolWorkspace, Query: map[string]string{"open": dir}}
}

// closeLiveAtEnd closes every open surface when the test ends, the way the
// app closes a tool (ui.CloseTool): CloseTool and the commands it returns,
// which end a share and close its audit log. Windows refuses to delete a
// file or folder that is still open (the os.Root holds the folder, the audit
// log its ws_*.jsonl), so a surface left open fails t.TempDir's RemoveAll
// there. Call it after uitest.New: the harness's t.TempDir comes first and
// Cleanup is LIFO, so this runs before the temp dirs are removed. Calling it
// more than once is harmless.
func closeLiveAtEnd(t *testing.T, h *uitest.Harness) {
	t.Helper()
	t.Cleanup(func() {
		liveMu.Lock()
		open := append([]*Surface(nil), live...)
		liveMu.Unlock()
		for _, s := range open {
			h.Send(ui.CloseTool(s.id)())
			if !s.closed { // not (or no longer) an app tool entry
				runCmds(s.CloseTool(h.Context()))
			}
		}
	})
}

// runCmds runs cmd and the commands of any batch it returns, dropping the
// other messages (test cleanup only).
func runCmds(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	if b, ok := cmd().(tea.BatchMsg); ok {
		for _, c := range b {
			runCmds(c)
		}
	}
}

// openFolder opens dir as instance id and returns its surface (closed when
// the test ends, closeLiveAtEnd).
func openFolder(t *testing.T, h *uitest.Harness, id, dir string) *Surface {
	t.Helper()
	closeLiveAtEnd(t, h)
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
