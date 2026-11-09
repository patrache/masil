package leet_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/patrache/masil/internal/leetcode"
	"github.com/patrache/masil/internal/termhost"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/leet"
	"github.com/patrache/masil/internal/ui/uitest"
)

const snippet1606 = "func busiestServers(k int, arrival []int, load []int) []int {\n    \n}"

// solutionPath is today's Go solution file in the default folder.
func solutionPath(e *env) string {
	return filepath.Join(leetcode.DefaultDir(e.home), "2026-11-05-1606-"+slug1606+".go")
}

// withNvim makes nvim findable (path "/opt/bin/nvim") or not.
func withNvim(t *testing.T, on bool) {
	t.Helper()
	t.Cleanup(leet.SetLookPathForTest(func(name string) (string, error) {
		if on && name == "nvim" {
			return "/opt/bin/nvim", nil
		}
		return "", os.ErrNotExist
	}))
}

// todayInstance is the open "leet:today" instance.
func todayInstance(t *testing.T, h *uitest.Harness) ui.Component {
	t.Helper()
	c := h.Instance(ui.Route{Kind: ui.RouteTool, ID: "leet:today", Arg: "leet"}.Key())
	if c == nil {
		t.Fatal("leet:today is not open")
	}
	return c
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestPaneNvimSpec(t *testing.T) {
	e := setup(t)
	e.lc.SetDailyDate("2026-11-05")
	withNvim(t, true)
	h := uitest.New(t)
	openToday(t, h)
	h.Press("e")
	st := e.pane.Started()
	if len(st) != 1 {
		t.Fatalf("started = %+v", st)
	}
	p := solutionPath(e)
	if st[0].Program != "/opt/bin/nvim" || !slices.Equal(st[0].Args, []string{p}) || st[0].Mode != termhost.ModePane || st[0].Dir != filepath.Dir(p) || st[0].ToolID != "leet" {
		t.Fatalf("request = %+v, want nvim %s in a pane", st[0], p)
	}
	if got := readFile(t, p); got != snippet1606 {
		t.Fatalf("solution file = %q", got)
	}
	if s := h.Screen(); !strings.Contains(s, "nvim pane") {
		t.Fatalf("pane not shown:\n%s", s)
	}
	// e again does not start a second nvim.
	h.Press("ctrl+\\", "esc", "e")
	if n := len(e.pane.Started()); n != 1 {
		t.Fatalf("started %d editors", n)
	}
}

func TestPrefixRunFromPane(t *testing.T) {
	e := setup(t)
	e.lc.SetDailyDate("2026-11-05")
	withNvim(t, true)
	h := uitest.New(t)
	openToday(t, h)
	h.Press("e")
	h.Press("ctrl+\\", "r")
	h.Press("ctrl+\\", "s")
	run, submit := leet.RequestsForTest(todayInstance(t, h))
	if run != 1 || submit != 1 {
		t.Fatalf("run %d submit %d, want 1 1 (keys to nvim: %q)", run, submit, e.pane.Keys("leet:today"))
	}
	if keys := e.pane.Keys("leet:today"); slices.Contains(keys, "r") {
		t.Fatalf("r went to nvim: %q", keys)
	}
}

func TestGoeditorFallback(t *testing.T) {
	e := setup(t)
	e.lc.SetDailyDate("2026-11-05")
	withNvim(t, false)
	h := uitest.New(t)
	openToday(t, h)
	h.Press("e")
	if n := len(e.pane.Started()); n != 0 {
		t.Fatalf("started %+v without nvim", e.pane.Started())
	}
	if s := h.Screen(); !strings.Contains(s, "busiestServers") {
		t.Fatalf("goeditor does not show the snippet:\n%s", s)
	}
	h.Press("i")
	h.Type("// mine")
	h.Press("esc")
	h.Type(":w")
	h.Press("enter")
	p := solutionPath(e)
	if got := readFile(t, p); !strings.HasPrefix(got, "// mine") {
		t.Fatalf(":w did not save: %q", got)
	}
	h.Type(":Run")
	h.Press("enter")
	h.Type(":Submit")
	h.Press("enter")
	if run, submit := leet.RequestsForTest(todayInstance(t, h)); run != 1 || submit != 1 {
		t.Fatalf("run %d submit %d after :Run :Submit", run, submit)
	}
	// esc in normal mode leaves the editor: r is the tool's run again.
	h.Press("esc", "r")
	if run, _ := leet.RequestsForTest(todayInstance(t, h)); run != 2 {
		t.Fatalf("run = %d after esc r", run)
	}
	if got := readFile(t, p); strings.Contains(got, "Run") {
		t.Fatalf("the command reached the file: %q", got)
	}
	h.Check()
}

func TestExecReload(t *testing.T) {
	e := setup(t)
	e.lc.SetDailyDate("2026-11-05")
	withNvim(t, false)
	t.Setenv("EDITOR", "myed --wait")
	h := uitest.New(t)
	openToday(t, h)
	h.Press("e", "esc", "E")
	st := e.pane.Started()
	p := solutionPath(e)
	if len(st) != 1 || st[0].Mode != termhost.ModeExec || st[0].Program != "myed" || !slices.Equal(st[0].Args, []string{"--wait", p}) {
		t.Fatalf("started = %+v", st)
	}
	if err := os.WriteFile(p, []byte("func changedOnDisk() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.Send(e.pane.Exit("leet:today#exec", 0))
	if s := h.Screen(); !strings.Contains(s, "changedOnDisk") {
		t.Fatalf("editor not reloaded:\n%s", s)
	}
}

func TestNoExternalEditor(t *testing.T) {
	e := setup(t)
	e.lc.SetDailyDate("2026-11-05")
	withNvim(t, false)
	h := uitest.New(t)
	openToday(t, h)
	h.Press("E")
	if n := len(e.pane.Started()); n != 0 {
		t.Fatalf("started %+v", e.pane.Started())
	}
	if s := h.Screen(); !strings.Contains(s, "nvim") {
		t.Fatalf("no toast:\n%s", s)
	}
}

func TestExistingSolutionKept(t *testing.T) {
	e := setup(t)
	e.lc.SetDailyDate("2026-11-05")
	withNvim(t, false)
	p := solutionPath(e)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("func alreadyMine() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := uitest.New(t)
	openToday(t, h)
	h.Press("e")
	if got := readFile(t, p); got != "func alreadyMine() {}\n" {
		t.Fatalf("overwritten: %q", got)
	}
	if s := h.Screen(); !strings.Contains(s, "alreadyMine") {
		t.Fatalf("not shown:\n%s", s)
	}
}

func TestWindowsNoPane(t *testing.T) {
	e := setup(t)
	e.lc.SetDailyDate("2026-11-05")
	withNvim(t, true)
	win := termhost.Env{GOOS: "windows", TERM: "xterm-256color", Embedded: true}
	e.pane.SetEnv(win)
	t.Cleanup(leet.SetTermEnvForTest(win))
	h := uitest.New(t)
	openToday(t, h)
	h.Press("e")
	if n := len(e.pane.Started()); n != 0 {
		t.Fatalf("a pane on Windows: %+v", e.pane.Started())
	}
	if s := h.Screen(); !strings.Contains(s, "busiestServers") {
		t.Fatalf("no goeditor:\n%s", s)
	}
	h.Press("esc", "E")
	if st := e.pane.Started(); len(st) != 1 || st[0].Mode != termhost.ModeExec || st[0].Program != "/opt/bin/nvim" {
		t.Fatalf("E = %+v", st)
	}
}

func TestLanguageSwitchFile(t *testing.T) {
	e := setup(t)
	e.lc.SetDailyDate("2026-11-05")
	withNvim(t, false)
	h := uitest.New(t)
	openToday(t, h)
	h.Press("l", "e")
	py := strings.TrimSuffix(solutionPath(e), ".go") + ".py"
	if _, err := os.Stat(py); err != nil {
		t.Fatalf("no Python file after l: %v", err)
	}
}

func TestEditorGoeditorGolden(t *testing.T) {
	e := setup(t)
	e.lc.SetDailyDate("2026-11-05")
	withNvim(t, false)
	uitest.ForEachSize(t, leetSizes, func(t *testing.T, h *uitest.Harness) {
		openToday(t, h)
		h.Press("e")
		h.Check()
		h.Golden("leet_editor_goeditor")
	})
}
