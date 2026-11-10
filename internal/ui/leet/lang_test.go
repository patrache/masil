package leet_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/patrache/masil/internal/leetcode"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/leet"
	"github.com/patrache/masil/internal/ui/uitest"
)

// The solve language is always on screen (v0.1.3-1 part C): the code
// area's head line ("Go · <file>") and the key hint's tail ("l 언어: Go"),
// and l says what it switched to in a toast.

func TestLanguageShownInEveryMode(t *testing.T) {
	e := setup(t)
	e.lc.SetDailyDate("2026-11-05")
	uitest.ForEachSize(t, leetSizes, func(t *testing.T, h *uitest.Harness) {
		openToday(t, h)
		if s := h.Screen(); !strings.Contains(s, "언어: Go") {
			t.Fatalf("no language before l:\n%s", s)
		}
		h.Press("l")
		s := h.Screen()
		if !strings.Contains(s, "언어: Go → Python3") {
			t.Fatalf("no toast:\n%s", s)
		}
		if !strings.Contains(s, "l 언어: Python3") {
			t.Fatalf("no language hint after l:\n%s", s)
		}
		if h.Context().Mode >= layout.ModeCompact && !strings.Contains(s, "Python3 · 2026-11-05-1606") {
			t.Fatalf("no code head line:\n%s", s)
		}
		h.Check()
	})
}

func TestCodeHeadLineInWidget(t *testing.T) {
	e := setup(t)
	e.lc.SetDailyDate("2026-11-05")
	h := uitest.New(t)
	h.Resize(80, 24)
	openToday(t, h)
	h.Press("2")
	if s := h.Screen(); !strings.Contains(s, "Go · 2026-11-05-1606") {
		t.Fatalf("no code head line on the code tab:\n%s", s)
	}
}

func TestLangSwitchReopensEditor(t *testing.T) {
	e := setup(t)
	e.lc.SetDailyDate("2026-11-05")
	withNvim(t, false)
	h := uitest.New(t)
	openToday(t, h)
	h.Press("e", "esc", "l")
	py := filepath.Join(leetcode.DefaultDir(e.home), "2026-11-05-1606-"+slug1606+".py")
	if got := leet.EditorPathForTest(todayInstance(t, h)); got != py {
		t.Fatalf("editor shows %q, want %q", got, py)
	}
	if s := h.Screen(); !strings.Contains(s, "class Solution") || !strings.Contains(s, "Python3 · ") {
		t.Fatalf("the Python file is not shown:\n%s", s)
	}
	if got := leet.EditorLangForTest(todayInstance(t, h)); got != "python" {
		t.Fatalf("highlighting language = %q, want python", got)
	}
	// Python3 -> Python keeps the .py file: the same editor, the language follows.
	h.Press("l")
	if got := leet.EditorPathForTest(todayInstance(t, h)); got != py {
		t.Fatalf("editor shows %q after Python", got)
	}
	if got := leet.EditorLangForTest(todayInstance(t, h)); got != "python2" {
		t.Fatalf("highlighting language after Python = %q, want python2", got)
	}
	h.Check()
}

// TestLangSwitchReopensNvim: nvim in the panel is ended and the new
// language's file opens in a new nvim once the old one is gone (its end
// never ends the new one).
func TestLangSwitchReopensNvim(t *testing.T) {
	e := setup(t)
	e.lc.SetDailyDate("2026-11-05")
	withNvim(t, true)
	h := uitest.New(t)
	openToday(t, h)
	h.Press("e", "ctrl+\\", "esc", "l")
	if n := len(e.pane.Started()); n != 1 {
		t.Fatalf("a new nvim before the old one ended: %+v", e.pane.Started())
	}
	h.Send(e.pane.Exit("leet:today", 0)) // the closed nvim's end
	st := e.pane.Started()
	py := filepath.Join(leetcode.DefaultDir(e.home), "2026-11-05-1606-"+slug1606+".py")
	if len(st) != 2 || st[1].Program != "/opt/bin/nvim" || len(st[1].Args) != 1 || st[1].Args[0] != py {
		t.Fatalf("started = %+v, want nvim %s", st, py)
	}
	if s := h.Screen(); !strings.Contains(s, "nvim pane") || !strings.Contains(s, "Python3 · ") {
		t.Fatalf("the new nvim is not shown:\n%s", s)
	}
	if got := leet.EditorPathForTest(todayInstance(t, h)); got != py {
		t.Fatalf("editor = %q", got)
	}
	h.Check()
}
