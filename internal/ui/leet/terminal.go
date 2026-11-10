package leet

import (
	"os"
	"path/filepath"
	"runtime"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/termhost"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/termpane"
)

// The terminal (v0.1.3-1): t, or the palette's leet.terminal, opens the
// user's shell in the solutions folder — a panel under the editor in the
// code area, the same termpane machinery the workspace's terminals use.
// Where panels cannot run (Windows, the tiny mode, [tools.leet] open =
// "exec") the shell takes the screen until it ends. Its end (exit) empties
// the panel; closing the tool ends it, as it ends nvim in the editor slot.

// termSuffix makes the terminal's termpane instance id ("leet:today#term").
const termSuffix = "#term"

// terminalRequestedMsg asks instance id to open (or go back into) its
// terminal (the palette's leet.terminal).
type terminalRequestedMsg struct{ id string }

// userShell is the shell t starts: $SHELL, else %COMSPEC% (cmd.exe) on
// Windows, else /bin/sh.
func userShell(goos string, getenv func(string) string) string {
	if s := getenv("SHELL"); s != "" {
		return s
	}
	if goos == "windows" {
		if s := getenv("COMSPEC"); s != "" {
			return s
		}
		return "cmd.exe"
	}
	return "/bin/sh"
}

// termOK: a problem is shown (its body or the premium lock), so the code
// area — where the terminal sits — is on screen.
func (in *instance) termOK() bool {
	return in.q != nil && (in.phase == phaseReady || in.phase == phasePremium)
}

// openTerminal is t: the terminal panel (started on first use) gets the keys.
func (in *instance) openTerminal(ctx *ui.Context) tea.Cmd {
	if !in.termOK() {
		return nil
	}
	in.tab = tabCode
	if in.term != nil {
		in.focusTerm(true)
		if !in.term.GrabsKeys() {
			in.term.HandleKey(ctx, tea.KeyPressMsg{Code: tea.KeyEnter}) // back into the shell
		}
		in.layoutEditor(ctx)
		return nil
	}
	dir := filepath.Clean(solutionDir(ctx))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ui.Toast(ctx.T.Leetcode.FileFailed(), ui.ToastError)
	}
	shell := userShell(runtime.GOOS, os.Getenv)
	id := in.id
	req := termhost.Request{Program: shell, Dir: dir, Title: filepath.Base(shell), ToolID: toolID}
	l, cmd, err := termpane.Launch(ctx, req, termpane.Options{ToolID: toolID, InstanceID: id + termSuffix,
		OnPrefix: func(_ *ui.Context, key string) tea.Cmd {
			switch key {
			case "r":
				return emit(runRequestedMsg{id: id})
			case "s":
				return emit(submitRequestedMsg{id: id})
			}
			return nil
		}})
	if err != nil {
		return cmd // the pane limit's toast (or the downgrade's)
	}
	if l.View != nil {
		in.term = l.View
		in.focusTerm(true)
		in.layoutEditor(ctx)
	}
	return cmd // exec / window: the shell has the screen until it ends
}

// focusTerm moves the tool's own focus to the terminal (on) or off it, and
// tells both panels where it is (their programs' focus reports).
func (in *instance) focusTerm(on bool) {
	in.termFocus = on
	if on {
		in.edFocus = false
	}
	in.syncCells()
}

// syncCells tells the editor's and the terminal's panels whether the
// tool's focus is on them.
func (in *instance) syncCells() {
	if p, ok := in.ed.(*paneEditor); ok {
		p.SetCellFocus(in.edFocus)
	}
	if in.term != nil {
		in.term.SetCellFocus(in.termFocus)
	}
}

// termEnded drops the ended shell's panel.
func (in *instance) termEnded(ctx *ui.Context, msg termpane.ExitedMsg) tea.Cmd {
	if in.term == nil {
		return nil // exec / window: nothing was kept
	}
	cmd := in.term.Update(ctx, msg)
	in.term, in.termFocus = nil, false
	in.layoutEditor(ctx)
	return cmd
}

// dropTerminal ends the shell (closing the tool).
func (in *instance) dropTerminal() tea.Cmd {
	p := in.term
	in.term, in.termFocus = nil, false
	if p == nil {
		return nil
	}
	h := p.Handle()
	return func() tea.Msg { _ = h.Close(); return nil }
}

// termActive: the terminal has the tool's focus and is on screen.
func (in *instance) termActive(ctx *ui.Context) bool {
	if in.term == nil || !in.termFocus {
		return false
	}
	_, _, _, _, ok := in.termRect(ctx)
	return ok
}

// routeKey is this instance's route (what ui.FocusChangedMsg names).
func (in *instance) routeKey() string {
	return ui.Route{Kind: ui.RouteTool, ID: in.id, Arg: toolID}.Key()
}

// termFocusMsg is m as the terminal's panel must see it: its own route
// when the app's focus is on this instance.
func (in *instance) termFocusMsg(m ui.FocusChangedMsg) ui.FocusChangedMsg {
	if m.RouteKey == in.routeKey() {
		m.RouteKey = ui.Route{Kind: ui.RouteTool, ID: in.id + termSuffix, Arg: toolID}.Key()
	}
	return m
}

// activeInstance is the leet instance in front (else the first open one).
func activeInstance(ctx *ui.Context) string {
	if ctx.OpenTools == nil {
		return ""
	}
	first := ""
	for _, o := range ctx.OpenTools() {
		if o.Route.Arg != toolID {
			continue
		}
		if o.Active {
			return o.Route.ID
		}
		if first == "" {
			first = o.Route.ID
		}
	}
	return first
}
