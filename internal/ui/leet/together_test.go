package leet_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/uitest"

	// The git tool next to leet, as cmd/masil/register_leetgit.go links both.
	// Only this test binary imports both: the production packages still do
	// not import each other (CA 2.2).
	_ "github.com/patrache/masil/internal/ui/gittool"
)

// fakeLazygit puts a stand-in lazygit first on PATH (it answers --version
// and -cd like lazygit; the fake pane never runs it) and returns a folder
// the git instance can open. gittool's own seams live in its test files, so
// this test uses its real lookup.
func fakeLazygit(t *testing.T) (repo string) {
	t.Helper()
	// A short folder: the fake pane draws "<program path> pane WxH", which
	// must fit the 40-column tiny layout.
	bin, err := os.MkdirTemp("/tmp", "lg")
	if err != nil {
		t.Skipf("no short temp folder: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(bin) })
	cfg := t.TempDir()
	script := "#!/bin/sh\ncase \"$1\" in\n" +
		"--version) echo 'commit=abc, build date=2026-11-07T00:00:00Z, build source=homebrew, version=0.66.3, os=darwin, arch=arm64, git version=2.47.1' ;;\n" +
		"-cd) echo '" + cfg + "' ;;\nesac\n"
	if err := os.WriteFile(filepath.Join(bin, "lazygit"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("LG_CONFIG_FILE", "")
	return t.TempDir()
}

// TestModeSwitchKeepsBothTools (P1 Task 24's "lazygit·leet 를 연 채"): today's
// problem with nvim in a pane and lazygit on one repository, both open,
// through every layout mode — no program restarts, both instances stay, each
// comes back, and the frame (the tab row's tool cell at 80x24 with both
// tools in it) holds at every size.
func TestModeSwitchKeepsBothTools(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no embedded panes on Windows (P0 Choose rule 1)")
	}
	e := setup(t)
	t.Setenv("MASIL_FEATURES", "leet,git")
	t.Setenv("TERM", "xterm-256color") // gittool predicts its mode from the real environment
	t.Setenv("TMUX", "")
	repo := fakeLazygit(t)
	gitID := "git:" + repo
	e.lc.SetDailyDate("2026-11-05")
	withNvim(t, true)
	h := uitest.New(t)
	h.Resize(200, 60)
	leetMe(h, api.LeetMe{Current: 3, Best: 5})
	dailyReport(h, "2026-11-05", slug1606)
	openToday(t, h)
	h.Press("e")
	h.Send(ui.OpenTool(ui.ToolGit, gitID, nil)())

	started := e.pane.Started()
	if len(started) != 2 || !strings.HasSuffix(started[0].Program, "nvim") || !strings.HasSuffix(started[1].Program, "lazygit") {
		t.Fatalf("Started = %+v, want nvim then lazygit:\n%s", started, h.Screen())
	}
	leetInst := todayInstance(t, h)
	gitKey := ui.Route{Kind: ui.RouteTool, ID: gitID, Arg: ui.ToolGit}.Key()
	gitInst := h.Instance(gitKey)
	if gitInst == nil {
		t.Fatalf("%s is not open:\n%s", gitID, h.Screen())
	}
	showLeet := ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteTool, ID: "leet:today", Arg: ui.ToolLeet}}
	showGit := ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteTool, ID: gitID, Arg: ui.ToolGit}}
	for _, sz := range modeWalk {
		h.Resize(sz.W, sz.H)
		for _, step := range []struct {
			show ui.NavigateMsg
			pane string
		}{{showGit, "lazygit pane"}, {showLeet, "nvim pane"}} {
			h.Send(step.show)
			h.Check()
			if n := len(e.pane.Started()); n != 2 {
				t.Fatalf("%dx%d: Started = %d, want 2 (a resize restarted a program)", sz.W, sz.H, n)
			}
			if todayInstance(t, h) != leetInst || h.Instance(gitKey) != gitInst {
				t.Fatalf("%dx%d: an instance was replaced", sz.W, sz.H)
			}
			if s := h.Screen(); !strings.Contains(s, step.pane) {
				t.Fatalf("%dx%d (%v): %q is not shown:\n%s", sz.W, sz.H, h.Context().Mode, step.pane, s)
			}
		}
	}
}
