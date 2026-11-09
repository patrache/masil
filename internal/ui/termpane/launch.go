package termpane

import (
	"errors"
	"sync/atomic"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/termhost"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
)

// Launch is the one way a tool opens a program (P0 Task 16a): it picks the
// mode (termhost.Choose in termhost.CurrentEnv, tiny from the layout) and
// starts the program there, so P1·P2 never put Choose, StartPane, New, Exec and
// Popup together themselves.

// Launched is what Launch started.
type Launched struct {
	Mode termhost.Mode
	Pane *termhost.Pane // pane mode only
	View *Pane          // pane mode only: the tool shows it (a ui.Component; Init is not needed)
}

// CwdMsg reports the program's working folder (OSC 7) — pane mode only.
type CwdMsg struct{ InstanceID, Dir string }

// NotifyMsg reports that the program asked for attention: Kind "bel", "osc9"
// or "osc777" — pane mode only (P2 shows "승인 대기" with it).
type NotifyMsg struct{ InstanceID, Kind string }

// paneStartSize is the size a pane starts at; the first draw resizes it to
// the real inner size.
var paneStartSize = termhost.Size{W: 80, H: 24}

// The termhost seams Launch goes through (export_test.go swaps them).
var (
	currentEnv   = termhost.CurrentEnv
	startPane    = termhost.StartPane
	execProgram  = termhost.Exec
	popupProgram = termhost.Popup
)

// LaunchFunc is Launch's signature (termpanetest replaces it).
type LaunchFunc func(ctx *ui.Context, req termhost.Request, opts Options) (Launched, tea.Cmd, error)

var launchFn atomic.Pointer[LaunchFunc]

func init() {
	f := LaunchFunc(launch)
	launchFn.Store(&f)
}

// Launch starts req for instance opts.InstanceID in the mode Choose picks:
//
//   - pane: StartPane at 80x24 (the first draw resizes it) and New(pane,
//     opts). Its screen changes are posted coalesced as DirtyMsg, OSC 7 as
//     CwdMsg, bells and OSC 9/777 as NotifyMsg; ExitedMsg is posted once the
//     program ends (Launch watches it: the view needs no Init), and the view
//     calls opts.OnExit when that msg reaches its Update.
//   - exec: the returned Cmd is termhost.Exec; its end is ExitedMsg.
//   - window: termhost.Popup; a goroutine posts ExitedMsg when it closes.
//
// opts.OnExit is a pane-mode hook (see Options); ExitedMsg is the end in
// every mode. A mode that had to fall back to exec adds the toast
// termpane.downgraded.
// At the pane limit (all tools together) the error is termhost.ErrPaneLimit
// and the Cmd is the toast termpane.pane_limit. Every ExitedMsg, CwdMsg and
// NotifyMsg carries InstanceID: an instance takes only its own.
func Launch(ctx *ui.Context, req termhost.Request, opts Options) (Launched, tea.Cmd, error) {
	return (*launchFn.Load())(ctx, req, opts)
}

func launch(ctx *ui.Context, req termhost.Request, opts Options) (Launched, tea.Cmd, error) {
	if req.ToolID == "" {
		req.ToolID = opts.ToolID
	}
	env := currentEnv(ctx.Config, req.ToolID, ctx.Mode == layout.ModeTiny)
	mode, downgraded := termhost.Choose(req, env)
	var toast tea.Cmd
	if downgraded {
		toast = DowngradedToast(ctx.T, req, env)
	}
	post := ctx.Post
	if post == nil {
		post = ui.DiscardPoster{}
	}
	id := opts.InstanceID
	switch mode {
	case termhost.ModePane:
		po := termhost.PaneOptions{
			OnDirty:  func() { post.Coalesce("pane:"+id, DirtyMsg{InstanceID: id}) },
			OnCwd:    func(dir string) { post.Post(CwdMsg{InstanceID: id, Dir: dir}) },
			OnNotify: func(kind string) { post.Post(NotifyMsg{InstanceID: id, Kind: kind}) },
		}
		if ctx.Theme != nil {
			po.FG, po.BG = ctx.Theme.P.FG, ctx.Theme.P.BG
		}
		p, err := startPane(req, paneStartSize, po)
		if errors.Is(err, termhost.ErrPaneLimit) {
			return Launched{}, tea.Batch(toast, PaneLimitToast(ctx.T, termhost.MaxPanes)), err
		}
		if err != nil {
			return Launched{}, toast, err
		}
		view := New(p, opts)
		view.watchExit(post) // the tool need not Init the view for its end to arrive
		return Launched{Mode: termhost.ModePane, Pane: p, View: view}, toast, nil
	case termhost.ModeWindow:
		ch, err := popupProgram(req)
		if err != nil {
			return Launched{}, toast, err
		}
		go func() {
			if info, ok := <-ch; ok {
				post.Post(ExitedMsg{InstanceID: id, Info: info})
			}
		}()
		return Launched{Mode: termhost.ModeWindow}, toast, nil
	}
	cmd := execProgram(req, func(info termhost.ExitInfo) tea.Msg { return ExitedMsg{InstanceID: id, Info: info} })
	return Launched{Mode: termhost.ModeExec}, tea.Batch(toast, cmd), nil
}

// DowngradedToast is the toast for a wanted mode (req.Mode, else the tool's
// configured open) that fell back to exec.
func DowngradedToast(t *i18n.T, req termhost.Request, env termhost.Env) tea.Cmd {
	wanted := req.Mode
	if wanted == "" || wanted == termhost.ModeAuto {
		wanted = env.ToolOpen
	}
	label := t.Tools.ModeWindow()
	if wanted == termhost.ModePane {
		label = t.Tools.ModePane()
	}
	return ui.Toast(t.Termpane.Downgraded(label), ui.ToastInfo)
}

// PaneLimitToast is the toast for a pane refused at the limit of max panes.
func PaneLimitToast(t *i18n.T, max int) tea.Cmd {
	return ui.Toast(t.Termpane.PaneLimit(max), ui.ToastInfo)
}
