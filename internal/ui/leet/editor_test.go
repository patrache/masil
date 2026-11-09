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
	signIn(t, e, h)
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
	signIn(t, e, h)
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

// Task 20 / A10.4: after esc (normal mode) r and s run and submit, in the
// editor — not vim's replace and substitute.
func TestGoeditorNormalModeRunSubmit(t *testing.T) {
	e := setup(t)
	e.lc.SetDailyDate("2026-11-05")
	withNvim(t, false)
	h := uitest.New(t)
	signIn(t, e, h)
	openToday(t, h)
	h.Press("e", "i")
	h.Type("// mine")
	h.Press("esc", ":", "w", "enter") // normal mode, saved
	before := readFile(t, solutionPath(e))
	h.Press("r")
	if run, _ := leet.RequestsForTest(todayInstance(t, h)); run != 1 {
		t.Fatalf("run = %d after esc r in the editor", run)
	}
	h.Press("s")
	if _, submit := leet.RequestsForTest(todayInstance(t, h)); submit != 1 {
		t.Fatalf("submit = %d after s in the editor", submit)
	}
	h.Press("0", "f", "r") // r as an argument stays vim's
	if run, _ := leet.RequestsForTest(todayInstance(t, h)); run != 1 {
		t.Fatalf("run = %d after f r", run)
	}
	h.Press("x", ":", "w", "enter") // the editor still has the keys
	if got := readFile(t, solutionPath(e)); got == before {
		t.Fatal("the editor lost the keys after r / s")
	}
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
	// l is the device's memory (tools-state), not config.toml
	var st struct {
		Lang string `json:"lang"`
	}
	h.Context().ToolState.Section("leet", &st)
	if st.Lang != "python3" {
		t.Fatalf("tools-state lang = %q", st.Lang)
	}
	if b, err := os.ReadFile(h.Context().Paths.Config); err == nil && strings.Contains(string(b), "lang = ") {
		t.Fatalf("l wrote config.toml:\n%s", b)
	}
}

// With [tools.leet] lang set by hand, l still switches: it takes the pin
// out of config.toml (keeping the rest) and remembers the pick on the device.
func TestLanguageSwitchOverConfigPin(t *testing.T) {
	e := setup(t)
	e.lc.SetDailyDate("2026-11-05")
	withNvim(t, false)
	h := uitest.New(t)
	if err := os.WriteFile(h.Context().Paths.Config, []byte("[tools.leet]\nlang = \"golang\"\nauto_status = false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := leet.ReloadConfigForTest(h.Context()); err != nil {
		t.Fatal(err)
	}
	openToday(t, h)
	h.Press("l", "e")
	py := strings.TrimSuffix(solutionPath(e), ".go") + ".py"
	if _, err := os.Stat(py); err != nil {
		t.Fatalf("no Python file after l: %v", err)
	}
	b, err := os.ReadFile(h.Context().Paths.Config)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "lang = ") || !strings.Contains(string(b), "auto_status = false") {
		t.Fatalf("config.toml after l:\n%s", b)
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

// The hub's open list and the widget's context line tell problems apart:
// the title carries the problem's number, the status how it is edited.
func TestInstanceTitleAndStatus(t *testing.T) {
	e := setup(t)
	e.lc.SetDailyDate("2026-11-05")
	withNvim(t, false)
	h := uitest.New(t)
	signIn(t, e, h)
	openToday(t, h)
	in := todayInstance(t, h)
	if title, _ := in.(ui.Titled).Title(h.Context()); title != "오늘의 문제 · 1606" {
		t.Fatalf("title = %q", title)
	}
	st, ok := in.(ui.ToolStatuser)
	if !ok {
		t.Fatal("no ToolStatus")
	}
	if s := st.ToolStatus(h.Context()); s != "" {
		t.Fatalf("status before editing = %q", s)
	}
	h.Press("e")
	if s := st.ToolStatus(h.Context()); s != "masil 편집기" {
		t.Fatalf("status with goeditor = %q", s)
	}
	h.Press("esc", "r")
	if s := st.ToolStatus(h.Context()); s != "실행 중…" {
		t.Fatalf("status while running = %q", s)
	}
}

func TestInstanceStatusPane(t *testing.T) {
	e := setup(t)
	e.lc.SetDailyDate("2026-11-05")
	withNvim(t, true)
	h := uitest.New(t)
	openToday(t, h)
	h.Press("e")
	if s := todayInstance(t, h).(ui.ToolStatuser).ToolStatus(h.Context()); s != "패널 nvim" {
		t.Fatalf("status with nvim in a pane = %q", s)
	}
}

// Task 20: the solution file is made when the problem opens (an outside
// editor can open it before e is pressed).
func TestSolutionFileOnOpen(t *testing.T) {
	e := setup(t)
	e.lc.SetDailyDate("2026-11-05")
	h := uitest.New(t)
	openToday(t, h)
	if got := readFile(t, solutionPath(e)); got != snippet1606 {
		t.Fatalf("solution file at open = %q", got)
	}
}
