package termpanetest_test

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/config"
	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/termhost"
	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/keymap"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/termpane"
	"github.com/patrache/masil/internal/ui/termpane/termpanetest"
)

func ctxFor(mode layout.Mode) *ui.Context {
	return &ui.Context{Theme: theme.Get(theme.Default), Mode: mode, Keys: keymap.Default(), Config: config.Defaults(),
		T: i18n.KoT(), Post: ui.DiscardPoster{}}
}

func TestFakeRecordsRequests(t *testing.T) {
	f := termpanetest.Install(t)
	ctx := ctxFor(layout.ModeStandard)
	req := termhost.Request{Program: "lazygit", Args: []string{"-p", "/repo"}, Env: []string{"LG_CONFIG_FILE=/x"}, ToolID: ui.ToolGit}
	l, _, err := termpane.Launch(ctx, req, termpane.Options{ToolID: ui.ToolGit, InstanceID: "git:r"})
	if err != nil || l.Mode != termhost.ModePane || l.View == nil || l.Pane != nil {
		t.Fatalf("Launch = %+v, %v", l, err)
	}
	got := f.Started()
	if len(got) != 1 || got[0].Program != "lazygit" || got[0].Env[0] != "LG_CONFIG_FILE=/x" || got[0].Args[1] != "/repo" {
		t.Fatalf("Started = %+v", got)
	}
	got[0].Env[0] = "changed"
	if f.Started()[0].Env[0] != "LG_CONFIG_FILE=/x" {
		t.Fatal("Started shares the caller's slices")
	}
	l.View.Resize(ctx, ui.Size{W: 30, H: 5})
	if v := l.View.View(ctx, true); !strings.Contains(v, "lazygit pane 30x5") {
		t.Errorf("view = %q", v)
	}
	if ok, _ := l.View.HandleKey(ctx, tea.KeyPressMsg{Code: 'a', Text: "a"}); !ok {
		t.Error("the fake pane did not take a key")
	}
	if keys := f.Keys("git:r"); len(keys) != 1 || keys[0] != "a" {
		t.Errorf("Keys = %q", keys)
	}

	f.SetEnv(termhost.Env{GOOS: "windows", Embedded: true})
	if l, _, _ := termpane.Launch(ctx, req, termpane.Options{InstanceID: "git:w"}); l.Mode != termhost.ModeExec || l.View != nil {
		t.Errorf("windows env = %+v", l)
	}
	if l, _, _ := termpane.Launch(ctxFor(layout.ModeStandard), termhost.Request{Program: "lazygit", Mode: termhost.ModeWindow}, termpane.Options{InstanceID: "git:x"}); l.Mode != termhost.ModeExec {
		t.Errorf("window outside tmux = %+v", l)
	}
}

func TestFakeExitAndCwd(t *testing.T) {
	f := termpanetest.Install(t)
	ctx := ctxFor(layout.ModeStandard)
	if _, _, err := termpane.Launch(ctx, termhost.Request{Program: "nvim"}, termpane.Options{InstanceID: "leet:1"}); err != nil {
		t.Fatal(err)
	}
	if m := f.Exit("leet:1", 2); m != (termpane.ExitedMsg{InstanceID: "leet:1", Info: termhost.ExitInfo{Mode: termhost.ModePane, Code: 2}}) {
		t.Errorf("Exit = %#v", m)
	}
	if m := f.Cwd("leet:1", "/tmp"); m != (termpane.CwdMsg{InstanceID: "leet:1", Dir: "/tmp"}) {
		t.Errorf("Cwd = %#v", m)
	}
	if m := f.Notify("leet:1", "bel"); m != (termpane.NotifyMsg{InstanceID: "leet:1", Kind: "bel"}) {
		t.Errorf("Notify = %#v", m)
	}
}

func TestFakeLimit(t *testing.T) {
	f := termpanetest.Install(t)
	f.SetLimit(2)
	ctx := ctxFor(layout.ModeStandard)
	for _, id := range []string{"git:1", "git:2"} {
		if _, _, err := termpane.Launch(ctx, termhost.Request{Program: "lazygit"}, termpane.Options{InstanceID: id}); err != nil {
			t.Fatal(err)
		}
	}
	_, cmd, err := termpane.Launch(ctx, termhost.Request{Program: "lazygit"}, termpane.Options{InstanceID: "git:3"})
	if !errors.Is(err, termhost.ErrPaneLimit) || cmd == nil {
		t.Fatalf("third = %v, cmd %v", err, cmd)
	}
	if m, ok := cmd().(ui.ToastMsg); !ok || !strings.Contains(m.Text, "2개까지") {
		t.Errorf("toast = %#v", m)
	}
	f.Exit("git:1", 0)
	if _, _, err := termpane.Launch(ctx, termhost.Request{Program: "lazygit"}, termpane.Options{InstanceID: "git:4"}); err != nil {
		t.Fatalf("after an exit: %v", err)
	}
	if n := len(f.Started()); n != 4 {
		t.Errorf("Started = %d", n)
	}
}
