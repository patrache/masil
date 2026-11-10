package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/workspace/fsroot"
	"github.com/patrache/masil/internal/workspace/journal"
)

func TestOpenFolderSolo(t *testing.T) {
	setup(t)
	h := uitest.New(t)
	dir := copyFixture(t)
	openFolder(t, h, "workspace:test", dir)
	scr := h.Screen()
	for _, want := range []string{"🔒 나만 보기", "src/", "README.md", "🔒 .env (공유 안 됨)", "node_modules/"} {
		if !strings.Contains(scr, want) {
			t.Errorf("screen lacks %q:\n%s", want, scr)
		}
	}
	if strings.Contains(scr, "x.js") {
		t.Error("node_modules was expanded")
	}
	// Spec B16 #1: the solo workspace never talks to the server about the folder.
	for _, r := range h.API.Requests() {
		all := r.Path + "?" + r.Query + string(r.Body)
		for _, bad := range []string{dir, "hub.ts", "README", "workspace-shares"} {
			if strings.Contains(all, bad) {
				t.Errorf("request %s %s carries %q", r.Method, r.Path, bad)
			}
		}
	}
	h.Check()
}

func TestOpenEditSaveSolo(t *testing.T) {
	setup(t)
	h := uitest.New(t)
	dir := copyFixture(t)
	s := openFolder(t, h, "workspace:test", dir)
	// tree: node_modules/, src/ … → src/ ▸ server/ ▸ hub.ts
	h.Press("j", "enter", "j", "enter", "j", "enter")
	if s.focus != PaneCode || s.code.current() == nil || s.code.current().rel != hubTS {
		t.Fatalf("focus %d, open %+v\n%s", s.focus, s.code.current(), h.Screen())
	}
	if !strings.Contains(h.Screen(), hubTS) {
		t.Fatalf("code header missing:\n%s", h.Screen())
	}
	h.Press("i", "X", "esc")
	if !strings.Contains(h.Screen(), "● 저장 안 됨") {
		t.Fatalf("no unsaved mark:\n%s", h.Screen())
	}
	h.Press(":", "w", "enter")
	b, err := os.ReadFile(filepath.Join(dir, hubTS))
	if err != nil || !strings.HasPrefix(string(b), "X// Test fixture") {
		t.Fatalf("disk = %.40q, %v", b, err)
	}
	if strings.Contains(h.Screen(), "● 저장 안 됨") {
		t.Fatalf("still unsaved after :w:\n%s", h.Screen())
	}
}

func TestRecentFoldersRemembered(t *testing.T) {
	setup(t)
	h := uitest.New(t)
	dir := copyFixture(t)
	openFolder(t, h, "workspace:test", dir)
	var st wsState
	if !h.Context().ToolState.Section(ui.ToolWorkspace, &st) || len(st.Recent) == 0 || st.Recent[0] != dir {
		t.Fatalf("recent = %+v", st)
	}
	h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteTool, ID: pickerID, Arg: ui.ToolWorkspace}})
	if scr := h.Screen(); !strings.Contains(scr, "최근 폴더") || !strings.Contains(scr, "proj") {
		t.Fatalf("picker:\n%s", scr)
	}
	// enter on the recent row opens it
	h.Press("enter")
	if r := h.Nav(); len(r.Tools) == 0 {
		t.Fatal("no tool open")
	}
	if !strings.Contains(h.Screen(), "🔒 나만 보기 · proj") {
		t.Fatalf("recent folder did not open:\n%s", h.Screen())
	}
}

func TestRecoveryPrompt(t *testing.T) {
	setup(t)
	h := uitest.New(t)
	dir := copyFixture(t)
	j, err := journal.Open(h.Context().Paths.Cache, dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := j.Put("README.md", "# proj\n\nrecovered text\n", fsroot.Fingerprint{}); err != nil {
		t.Fatal(err)
	}
	s := openFolder(t, h, "workspace:test", dir)
	if !strings.Contains(h.Screen(), "복구할 내용이 있어요") {
		t.Fatalf("no recovery dialog:\n%s", h.Screen())
	}
	h.Press("enter") // the first choice: recover
	if v, ok := s.docs.View("README.md"); !ok || !strings.Contains(v.Text, "recovered text") {
		t.Fatalf("not recovered: %+v %v", v, ok)
	}
	if !strings.Contains(h.Screen(), "● 저장 안 됨") {
		t.Fatalf("recovered doc not shown as unsaved:\n%s", h.Screen())
	}
}

func TestWindowsTerminalNotice(t *testing.T) {
	setup(t)
	t.Cleanup(SetPaneSupportedForTest(false))
	h := uitest.New(t)
	openFolder(t, h, "workspace:test", copyFixture(t))
	if !strings.Contains(flat(h.Screen()), flat("Windows에서는 masil 안 터미널이 아직 안 돼요.")) {
		t.Fatalf("no windows notice:\n%s", h.Screen())
	}
}

func TestPrefixMovesFocus(t *testing.T) {
	fake := setup(t)
	h := uitest.New(t)
	s := openFolder(t, h, "workspace:test", copyFixture(t))
	h.Press("ctrl+\\", "3")
	if s.focus != PaneTerm {
		t.Fatalf("focus = %d", s.focus)
	}
	h.Press("n") // the new-terminal list; the first row is the login shell
	h.Press("enter")
	if got := fake.Started(); len(got) != 1 || got[0].Dir != s.dir || got[0].ToolID != ui.ToolWorkspace {
		t.Fatalf("started = %+v", got)
	}
	if !s.GrabsKeys() {
		t.Fatal("the terminal does not hold the keyboard")
	}
	h.Press("ctrl+\\", "1")
	if s.focus != PaneTree || s.GrabsKeys() {
		t.Fatalf("focus = %d grabs %v", s.focus, s.GrabsKeys())
	}
	h.Press("ctrl+\\", "2")
	if s.focus != PaneCode {
		t.Fatalf("focus = %d", s.focus)
	}
}
