package leet_test

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/patrache/masil/internal/termhost"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/leet"
	"github.com/patrache/masil/internal/ui/uitest"
)

// The terminal in the LeetCode tool (v0.1.3-1): t (or the palette's
// leet.terminal) opens the user's $SHELL in the solutions folder, in a
// panel under the editor; Windows and the tiny mode switch screens instead.

const termID = "leet:today#term"

func withShell(t *testing.T) {
	t.Helper()
	t.Setenv("SHELL", "/bin/zsh")
}

func TestTerminalKeyOpensShell(t *testing.T) {
	e := setup(t)
	e.lc.SetDailyDate("2026-11-05")
	withShell(t)
	h := uitest.New(t)
	signIn(t, e, h) // r runs without the login dialog
	openToday(t, h)
	h.Press("t")
	st := e.pane.Started()
	if len(st) != 1 {
		t.Fatalf("started = %+v", st)
	}
	dir := filepath.Dir(solutionPath(e))
	if st[0].Program != "/bin/zsh" || len(st[0].Args) != 0 || st[0].Dir != dir || st[0].ToolID != "leet" {
		t.Fatalf("request = %+v, want /bin/zsh in %s", st[0], dir)
	}
	s := h.Screen()
	if !strings.Contains(s, "zsh pane") || !strings.Contains(s, "터미널 · zsh") {
		t.Fatalf("terminal not shown:\n%s", s)
	}
	h.Check()
	// While the shell has the keys the hint says how to run from inside it.
	if !strings.Contains(s, "ctrl+\\ r 실행") || strings.Contains(s, "e 편집기 · t 터미널") {
		t.Fatalf("no shell hint while the shell has the keys:\n%s", s)
	}
	if !strings.Contains(s, "l 언어: Go") {
		t.Fatalf("no language tail in the shell hint:\n%s", s)
	}
	// Keys go to the shell, r included.
	h.Type("ls")
	h.Press("r")
	if keys := e.pane.Keys(termID); !slices.Equal(keys, []string{"l", "s", "r"}) {
		t.Fatalf("keys to the shell = %q", keys)
	}
	if run, _ := leet.RequestsForTest(todayInstance(t, h)); run != 0 {
		t.Fatalf("r ran the solution from inside the shell")
	}
	// ctrl+\ r is the tool's run, ctrl+\ esc leaves the shell: r is the tool's again.
	h.Press("ctrl+\\", "r")
	h.Press("ctrl+\\", "esc", "r")
	if run, _ := leet.RequestsForTest(todayInstance(t, h)); run != 2 {
		t.Fatalf("run = %d, want 2", run)
	}
	// t again goes back into the same shell.
	h.Press("t")
	h.Type("x")
	if n := len(e.pane.Started()); n != 1 {
		t.Fatalf("started %d shells", n)
	}
	if keys := e.pane.Keys(termID); keys[len(keys)-1] != "x" {
		t.Fatalf("t did not go back into the shell: %q", keys)
	}
	// Left again, the hint is the tool's (t goes back in).
	h.Press("ctrl+\\", "esc")
	if s := h.Screen(); !strings.Contains(s, "t 터미널") || strings.Contains(s, "ctrl+\\ esc 나가기") {
		t.Fatalf("no t hint after leaving the shell:\n%s", s)
	}
}

func TestTerminalUnderEditor(t *testing.T) {
	e := setup(t)
	e.lc.SetDailyDate("2026-11-05")
	withNvim(t, true)
	withShell(t)
	h := uitest.New(t)
	openToday(t, h)
	h.Press("e")
	h.Press("ctrl+\\", "esc", "t")
	s := h.Screen()
	if !strings.Contains(s, "nvim pane") || !strings.Contains(s, "zsh pane") {
		t.Fatalf("editor and terminal not both shown:\n%s", s)
	}
	if strings.Index(s, "nvim pane") > strings.Index(s, "zsh pane") {
		t.Fatalf("the terminal is not under the editor:\n%s", s)
	}
	h.Check()
	h.Type("a")
	if keys := e.pane.Keys(termID); !slices.Equal(keys, []string{"a"}) {
		t.Fatalf("shell keys = %q", keys)
	}
	if keys := e.pane.Keys("leet:today"); slices.Contains(keys, "a") {
		t.Fatalf("nvim got the shell's key: %q", keys)
	}
	// Back to the editor with e: keys go to nvim.
	h.Press("ctrl+\\", "esc", "e")
	h.Type("b")
	if keys := e.pane.Keys("leet:today"); !slices.Contains(keys, "b") {
		t.Fatalf("e did not go back to nvim: %q", keys)
	}
	if keys := e.pane.Keys(termID); slices.Contains(keys, "b") {
		t.Fatalf("the shell got nvim's key: %q", keys)
	}
	for _, sz := range []uitest.Size{{W: 200, H: 60}, {W: 80, H: 24}, {W: 120, H: 40}} {
		h.Resize(sz.W, sz.H)
		h.Check()
	}
}

func TestTerminalPaneLimit(t *testing.T) {
	e := setup(t)
	e.lc.SetDailyDate("2026-11-05")
	withShell(t)
	e.pane.SetLimit(0)
	h := uitest.New(t)
	openToday(t, h)
	h.Press("t")
	s := h.Screen()
	if !strings.Contains(s, "패널은 동시에") {
		t.Fatalf("no pane-limit toast:\n%s", s)
	}
	if strings.Contains(s, "터미널 · zsh") {
		t.Fatalf("a terminal shown at the limit:\n%s", s)
	}
	h.Check()
}

func TestTerminalExitsAndCloses(t *testing.T) {
	e := setup(t)
	e.lc.SetDailyDate("2026-11-05")
	withShell(t)
	e.pane.SetLimit(1)
	h := uitest.New(t)
	openToday(t, h)
	h.Press("t")
	h.Send(e.pane.Exit(termID, 0))
	if s := h.Screen(); strings.Contains(s, "zsh pane") {
		t.Fatalf("the ended shell is still shown:\n%s", s)
	}
	h.Press("t")
	if n := len(e.pane.Started()); n != 2 {
		t.Fatalf("t after the end: started %d", n)
	}
	// Closing the tool ends the shell: the one panel is free again.
	h.Send(ui.CloseToolMsg{InstanceID: "leet:today"})
	h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteTools}})
	h.Press("1", "t")
	if s := h.Screen(); !strings.Contains(s, "zsh pane") {
		t.Fatalf("the closed tool's shell still holds the panel:\n%s", s)
	}
}

func TestTerminalPaletteCommand(t *testing.T) {
	e := setup(t)
	e.lc.SetDailyDate("2026-11-05")
	withShell(t)
	h := uitest.New(t)
	openToday(t, h)
	h.Send(ui.RunCommandMsg{ID: "leet.terminal"})
	if st := e.pane.Started(); len(st) != 1 || st[0].Program != "/bin/zsh" {
		t.Fatalf("started = %+v", st)
	}
	if s := h.Screen(); !strings.Contains(s, "zsh pane") {
		t.Fatalf("no terminal:\n%s", s)
	}
}

func TestTerminalScreenSwitch(t *testing.T) {
	for _, tc := range []struct {
		name string
		size uitest.Size
		env  *termhost.Env
	}{
		{"windows", uitest.Size{W: 120, H: 40}, &termhost.Env{GOOS: "windows", TERM: "xterm-256color", Embedded: true}},
		{"tiny", uitest.Size{W: 40, H: 14}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := setup(t)
			e.lc.SetDailyDate("2026-11-05")
			withShell(t)
			if tc.env != nil {
				e.pane.SetEnv(*tc.env)
			}
			h := uitest.New(t)
			h.Resize(tc.size.W, tc.size.H)
			openToday(t, h)
			h.Press("t")
			if st := e.pane.Started(); len(st) != 1 || st[0].Program != "/bin/zsh" {
				t.Fatalf("started = %+v", st)
			}
			if s := h.Screen(); strings.Contains(s, "zsh pane") {
				t.Fatalf("a pane opened:\n%s", s)
			}
			h.Check()
			h.Send(e.pane.Exit(termID, 0)) // back from the screen switch
			h.Check()
		})
	}
}

func TestUserShell(t *testing.T) {
	for _, tc := range []struct {
		goos string
		env  map[string]string
		want string
	}{
		{"darwin", map[string]string{"SHELL": "/bin/zsh"}, "/bin/zsh"},
		{"linux", nil, "/bin/sh"},
		{"windows", map[string]string{"COMSPEC": `C:\Windows\system32\cmd.exe`}, `C:\Windows\system32\cmd.exe`},
		{"windows", nil, "cmd.exe"},
		{"windows", map[string]string{"SHELL": "/usr/bin/bash", "COMSPEC": "cmd.exe"}, "/usr/bin/bash"},
	} {
		if got := leet.UserShellForTest(tc.goos, func(k string) string { return tc.env[k] }); got != tc.want {
			t.Errorf("%s %v: shell = %q, want %q", tc.goos, tc.env, got, tc.want)
		}
	}
}
