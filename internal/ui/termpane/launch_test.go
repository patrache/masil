package termpane_test

import (
	"errors"
	"runtime"
	"slices"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/config"
	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/termhost"
	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/keymap"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/termpane"
)

// recPoster records ctx.Post from any goroutine.
type recPoster struct {
	mu   sync.Mutex
	msgs []tea.Msg
}

func (p *recPoster) Post(m tea.Msg)               { p.add(m) }
func (p *recPoster) Coalesce(_ string, m tea.Msg) { p.add(m) }
func (p *recPoster) add(m tea.Msg) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.msgs = append(p.msgs, m)
}

// wait returns the first posted msg keep accepts, waiting up to 5s.
func (p *recPoster) wait(t *testing.T, keep func(tea.Msg) bool) tea.Msg {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		p.mu.Lock()
		i := slices.IndexFunc(p.msgs, keep)
		var m tea.Msg
		if i >= 0 {
			m = p.msgs[i]
		}
		p.mu.Unlock()
		if i >= 0 {
			return m
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("the msg was never posted")
	return nil
}

func (p *recPoster) count(keep func(tea.Msg) bool) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := 0
	for _, m := range p.msgs {
		if keep(m) {
			n++
		}
	}
	return n
}

func launchCtx(mode layout.Mode) (*ui.Context, *recPoster) {
	post := &recPoster{}
	return &ui.Context{
		Theme: theme.Get(theme.Default), Mode: mode, Keys: keymap.Default(), Config: config.Defaults(),
		T: i18n.KoT(), Lang: i18n.Ko, Post: post, Now: time.Now,
	}, post
}

// msgsOf runs cmd and every batch inside it, returning the leaf msgs.
func msgsOf(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if b, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range b {
			out = append(out, msgsOf(c)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

func toasts(cmd tea.Cmd) []string {
	var out []string
	for _, m := range msgsOf(cmd) {
		if t, ok := m.(ui.ToastMsg); ok {
			out = append(out, t.Text)
		}
	}
	return out
}

// fakeEnv makes Launch decide in env (tiny follows the layout).
func fakeEnv(t *testing.T, env termhost.Env) {
	t.Helper()
	t.Cleanup(termpane.SetLaunchEnvForTest(func(_ *config.Config, _ string, tiny bool) termhost.Env {
		e := env
		e.Tiny = tiny
		return e
	}))
}

// fakeExec records exec launches; the returned func ends the last one.
func fakeExec(t *testing.T) (calls *int, end func(termhost.ExitInfo) tea.Msg) {
	t.Helper()
	n := 0
	var onExit func(termhost.ExitInfo) tea.Msg
	t.Cleanup(termpane.SetExecForTest(func(_ termhost.Request, f func(termhost.ExitInfo) tea.Msg) tea.Cmd {
		n++
		onExit = f
		return nil
	}))
	return &n, func(i termhost.ExitInfo) tea.Msg { return onExit(i) }
}

// fakePopup records window launches; each gets a channel the test closes.
func fakePopup(t *testing.T) *[]chan termhost.ExitInfo {
	t.Helper()
	var chans []chan termhost.ExitInfo
	t.Cleanup(termpane.SetPopupForTest(func(termhost.Request) (<-chan termhost.ExitInfo, error) {
		ch := make(chan termhost.ExitInfo, 1)
		chans = append(chans, ch)
		return ch, nil
	}))
	return &chans
}

func needPTY(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("embedded panes are macOS/Linux only")
	}
}

// closePane ends a real pane at cleanup and waits for it.
func closePane(t *testing.T, p *termhost.Pane) {
	t.Helper()
	t.Cleanup(func() {
		_ = p.Close()
		select {
		case <-p.Done():
		case <-time.After(5 * time.Second):
			t.Error("the pane did not end")
		}
	})
}

var darwin = termhost.Env{GOOS: "darwin", TERM: "xterm-256color", Embedded: true}

func TestLaunchChoosesMode(t *testing.T) {
	needPTY(t)
	execs, _ := fakeExec(t)
	pops := fakePopup(t)
	req := termhost.Request{Program: "/bin/cat", ToolID: ui.ToolGit, Title: "cat"}
	opts := termpane.Options{ToolID: ui.ToolGit, InstanceID: "git:a"}

	t.Run("tiny", func(t *testing.T) {
		fakeEnv(t, darwin)
		ctx, _ := launchCtx(layout.ModeTiny)
		l, cmd, err := termpane.Launch(ctx, req, opts)
		if err != nil || l.Mode != termhost.ModeExec || l.View != nil || *execs != 1 || len(toasts(cmd)) != 0 {
			t.Fatalf("tiny = %+v err %v execs %d toasts %q", l, err, *execs, toasts(cmd))
		}
	})
	t.Run("window", func(t *testing.T) {
		env := darwin
		env.TMUX, env.TmuxVersion, env.ToolOpen = "/tmp/tmux-501/default,1,0", "3.2", termhost.ModeWindow
		fakeEnv(t, env)
		ctx, _ := launchCtx(layout.ModeStandard)
		l, _, err := termpane.Launch(ctx, req, opts)
		if err != nil || l.Mode != termhost.ModeWindow || len(*pops) != 1 {
			t.Fatalf("window = %+v err %v pops %d", l, err, len(*pops))
		}
		close((*pops)[0])
	})
	t.Run("pane", func(t *testing.T) {
		fakeEnv(t, darwin)
		ctx, _ := launchCtx(layout.ModeStandard)
		l, _, err := termpane.Launch(ctx, req, opts)
		if err != nil || l.Mode != termhost.ModePane || l.Pane == nil || l.View == nil || l.View.Handle() != termpane.Handle(l.Pane) {
			t.Fatalf("pane = %+v err %v", l, err)
		}
		closePane(t, l.Pane)
	})
	t.Run("windows", func(t *testing.T) {
		env := darwin
		env.GOOS = "windows"
		fakeEnv(t, env)
		ctx, _ := launchCtx(layout.ModeStandard)
		before := *execs
		r := req
		r.Mode = termhost.ModePane // asked for a pane, Windows has none
		l, cmd, err := termpane.Launch(ctx, r, opts)
		want := i18n.KoT().Termpane.Downgraded(i18n.KoT().Tools.ModePane())
		if err != nil || l.Mode != termhost.ModeExec || *execs != before+1 || !slices.Equal(toasts(cmd), []string{want}) {
			t.Fatalf("windows = %+v err %v toasts %q, want %q", l, err, toasts(cmd), want)
		}
	})
}

func TestLaunchPaneLimitToast(t *testing.T) {
	fakeEnv(t, darwin)
	t.Cleanup(termpane.SetStartPaneForTest(func(termhost.Request, termhost.Size, termhost.PaneOptions) (*termhost.Pane, error) {
		return nil, termhost.ErrPaneLimit
	}))
	ctx, _ := launchCtx(layout.ModeStandard)
	l, cmd, err := termpane.Launch(ctx, termhost.Request{Program: "lazygit", ToolID: ui.ToolGit}, termpane.Options{InstanceID: "git:5"})
	if !errors.Is(err, termhost.ErrPaneLimit) || l.View != nil {
		t.Fatalf("Launch = %+v, %v; want ErrPaneLimit", l, err)
	}
	if got := toasts(cmd); !slices.Equal(got, []string{"패널은 동시에 4개까지예요 · 하나를 닫거나 화면 전환으로 여세요"}) {
		t.Errorf("toasts = %q", got)
	}
}

func isExited(id string) func(tea.Msg) bool {
	return func(m tea.Msg) bool { e, ok := m.(termpane.ExitedMsg); return ok && e.InstanceID == id }
}

func TestLaunchExitedMsgAllModes(t *testing.T) {
	needPTY(t)
	_, endExec := fakeExec(t)
	pops := fakePopup(t)
	ctx, post := launchCtx(layout.ModeStandard)

	// pane: the view's watcher posts it once the program ends by itself.
	fakeEnv(t, darwin)
	l, _, err := termpane.Launch(ctx, termhost.Request{Program: "/bin/sh", Args: []string{"-c", "exit 3"}, ToolID: ui.ToolGit},
		termpane.Options{ToolID: ui.ToolGit, InstanceID: "git:pane"})
	if err != nil || l.View == nil {
		t.Fatalf("pane launch: %+v %v", l, err)
	}
	l.View.Init(ctx)
	if m := post.wait(t, isExited("git:pane")).(termpane.ExitedMsg); m.Info.Code != 3 {
		t.Errorf("pane exit = %+v", m)
	}

	// exec: tea.ExecProcess's callback is the msg.
	env := darwin
	env.ToolOpen = termhost.ModeExec
	fakeEnv(t, env)
	if l, _, err = termpane.Launch(ctx, termhost.Request{Program: "lazygit", ToolID: ui.ToolGit}, termpane.Options{InstanceID: "git:exec"}); err != nil || l.Mode != termhost.ModeExec {
		t.Fatalf("exec launch: %+v %v", l, err)
	}
	if m, ok := endExec(termhost.ExitInfo{Mode: termhost.ModeExec, Code: 0}).(termpane.ExitedMsg); !ok || m.InstanceID != "git:exec" {
		t.Errorf("exec exit msg = %#v", m)
	}

	// window: the popup's channel is read on a goroutine and posted.
	env = darwin
	env.TMUX, env.TmuxVersion, env.ToolOpen = "/tmp/tmux", "3.4", termhost.ModeWindow
	fakeEnv(t, env)
	if l, _, err = termpane.Launch(ctx, termhost.Request{Program: "lazygit", ToolID: ui.ToolGit}, termpane.Options{InstanceID: "git:win"}); err != nil || l.Mode != termhost.ModeWindow {
		t.Fatalf("window launch: %+v %v", l, err)
	}
	(*pops)[0] <- termhost.ExitInfo{Mode: termhost.ModeWindow, Code: 1}
	if m := post.wait(t, isExited("git:win")).(termpane.ExitedMsg); m.Info.Code != 1 {
		t.Errorf("window exit = %+v", m)
	}
	time.Sleep(50 * time.Millisecond)
	for _, id := range []string{"git:pane", "git:win"} {
		if n := post.count(isExited(id)); n != 1 {
			t.Errorf("%s: %d ExitedMsg, want 1", id, n)
		}
	}
}

func TestLaunchCwdMsg(t *testing.T) {
	needPTY(t)
	fakeEnv(t, darwin)
	ctx, post := launchCtx(layout.ModeStandard)
	l, _, err := termpane.Launch(ctx, termhost.Request{Program: "/bin/sh", Args: []string{"-c", `printf '\033]7;file://host/tmp/masil-cwd\007'; sleep 5`}, ToolID: ui.ToolWorkspace},
		termpane.Options{ToolID: ui.ToolWorkspace, InstanceID: "workspace:t"})
	if err != nil {
		t.Fatal(err)
	}
	closePane(t, l.Pane)
	m := post.wait(t, func(m tea.Msg) bool { _, ok := m.(termpane.CwdMsg); return ok }).(termpane.CwdMsg)
	if m != (termpane.CwdMsg{InstanceID: "workspace:t", Dir: "/tmp/masil-cwd"}) {
		t.Errorf("CwdMsg = %+v", m)
	}
}

func TestLaunchNotifyMsg(t *testing.T) {
	needPTY(t)
	fakeEnv(t, darwin)
	ctx, post := launchCtx(layout.ModeStandard)
	l, _, err := termpane.Launch(ctx, termhost.Request{Program: "/bin/sh", Args: []string{"-c", `printf '\a'; sleep 5`}, ToolID: ui.ToolWorkspace},
		termpane.Options{ToolID: ui.ToolWorkspace, InstanceID: "workspace:n"})
	if err != nil {
		t.Fatal(err)
	}
	closePane(t, l.Pane)
	m := post.wait(t, func(m tea.Msg) bool { _, ok := m.(termpane.NotifyMsg); return ok }).(termpane.NotifyMsg)
	if m != (termpane.NotifyMsg{InstanceID: "workspace:n", Kind: "bel"}) {
		t.Errorf("NotifyMsg = %+v", m)
	}
}
