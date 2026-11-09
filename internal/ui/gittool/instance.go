package gittool

import (
	"context"
	"errors"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/config"
	"github.com/patrache/masil/internal/lazygit"
	"github.com/patrache/masil/internal/termhost"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/termpane"
)

// One lazygit instance (A11.4): instance id "git:" + the absolute repository.
// It prepares the launch off the UI goroutine (find lazygit, the user's
// config files and the color overlay — the overlay only for a pane), then
// starts it with termpane.Launch: a pane shown here, or a screen switch / tmux
// popup. When lazygit ends the instance stays as "종료됨 · enter 다시 열기"
// (x closes it). If it cannot start (pane limit, lazygit gone), it shows the
// picker again. A working folder report (OSC 7) that lands in another
// repository puts that repository in the recent list.

// termEnv is the environment the instance predicts the open mode in (the
// same as termpane.Launch's; tests swap both).
var termEnv = termhost.CurrentEnv

// instState is where an instance is.
type instState int

const (
	statePicking instState = iota
	stateStarting
	stateRunning
	stateExited
)

// preparedMsg carries a prepared launch (or why it cannot start).
type preparedMsg struct {
	id  string
	seq int
	req termhost.Request
	err error
}

// cwdRepoMsg is the repository a working-folder report landed in ("" = none).
type cwdRepoMsg struct {
	id, repo string
}

type instance struct {
	id     string
	repo   string
	query  map[string]string
	state  instState
	seq    int // the current launch; older preparedMsgs are dropped
	mode   termhost.Mode
	view   *termpane.Pane // pane mode, while running
	picker *picker
	size   ui.Size
}

var (
	_ ui.Component     = (*instance)(nil)
	_ ui.KeyHandler    = (*instance)(nil)
	_ ui.MouseHandler  = (*instance)(nil)
	_ ui.DragHandler   = (*instance)(nil)
	_ ui.KeyGrabber    = (*instance)(nil)
	_ ui.StatusTexter  = (*instance)(nil)
	_ ui.Cursorer      = (*instance)(nil)
	_ ui.Titled        = (*instance)(nil)
	_ ui.Helper        = (*instance)(nil)
	_ ui.ToolStatuser  = (*instance)(nil)
	_ ui.Closer        = (*instance)(nil)
	_ ui.InputCapturer = (*instance)(nil)
	_ ui.Completer     = (*instance)(nil)
)

func newInstance(_ *ui.Context, r ui.Route) ui.Component {
	return &instance{id: r.ID, repo: strings.TrimPrefix(r.ID, instancePrefix), query: r.Query, state: stateStarting}
}

func (in *instance) Init(ctx *ui.Context) tea.Cmd { return in.start(ctx) }

// wanted is the open mode asked for: the hub's one-off menu, else the
// [tools.lazygit] open setting.
func (in *instance) wanted(ctx *ui.Context) termhost.Mode {
	if m := termhost.Mode(in.query[queryMode]); m != "" && m != termhost.ModeAuto {
		return m
	}
	var cfg *config.Config
	if ctx != nil {
		cfg = ctx.Config
	}
	return termhost.Mode(cfg.ToolOpen(toolID))
}

// start prepares a launch in a Cmd (lookups, the overlay file).
func (in *instance) start(ctx *ui.Context) tea.Cmd {
	in.seq++
	in.state, in.view, in.picker = stateStarting, nil, nil
	cfg := toolConfig(ctx)
	env := curEnv()
	tiny := ctx.Mode == layout.ModeTiny
	req := termhost.Request{Dir: in.repo, Title: "lazygit", ToolID: toolID, Mode: in.wanted(ctx)}
	mode, _ := termhost.Choose(req, termEnv(ctx.Config, toolID, tiny))
	th, cache := ctx.Theme, ctx.Paths.Cache
	id, seq, repo := in.id, in.seq, in.repo
	return func() tea.Msg {
		bin, err := lazygit.Locate(env, cfg.Path)
		if err != nil {
			return preparedMsg{id: id, seq: seq, err: err}
		}
		c, cancel := context.WithTimeout(context.Background(), resolveTimeout)
		defer cancel()
		req.Program = bin
		req.Args = lazygit.LaunchArgs(c, env, bin, repo, mode, cfg.OverlayOn(), th, cache)
		return preparedMsg{id: id, seq: seq, req: req}
	}
}

// launch starts a prepared request.
func (in *instance) launch(ctx *ui.Context, req termhost.Request) tea.Cmd {
	l, cmd, err := termpane.Launch(ctx, req, termpane.Options{ToolID: toolID, InstanceID: in.id, SingleCell: true})
	if err != nil {
		in.pick(ctx)
		if cmd == nil || !errors.Is(err, termhost.ErrPaneLimit) {
			cmd = tea.Batch(cmd, ui.Toast(ctx.T.Lazygit.LaunchFailed(), ui.ToastError))
		}
		return cmd
	}
	in.state, in.mode, in.view = stateRunning, l.Mode, l.View
	if in.view != nil && in.size.W > 0 && in.size.H > 0 {
		in.view.Resize(ctx, in.size)
	}
	pushRecent(ctx, in.repo)
	return cmd
}

// pick shows the picker in place: choosing this repository again retries,
// another one opens its own instance.
func (in *instance) pick(ctx *ui.Context) {
	in.state, in.view = statePicking, nil
	in.picker = newPickerFor(in.id, func(ctx *ui.Context, repo string) tea.Cmd {
		if repo == in.repo {
			return in.start(ctx)
		}
		return tea.Batch(ui.CloseTool(in.id), ui.OpenTool(toolID, instancePrefix+repo, in.query))
	})
	if i := indexOf(loadState(ctx).Recent, in.repo); i >= 0 {
		in.picker.sel = i
	}
	in.picker.Resize(ctx, in.size)
}

func indexOf(list []string, s string) int {
	for i, v := range list {
		if v == s {
			return i
		}
	}
	return -1
}

func (in *instance) Update(ctx *ui.Context, msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case preparedMsg:
		if m.id != in.id || m.seq != in.seq || in.state != stateStarting {
			return nil
		}
		if m.err != nil {
			in.pick(ctx)
			return ui.OpenRegisteredOverlay(OverlayInstall, "")
		}
		return in.launch(ctx, m.req)
	case termpane.ExitedMsg:
		if m.InstanceID != in.id || in.state != stateRunning {
			return nil
		}
		var cmd tea.Cmd
		if in.view != nil {
			cmd = in.view.Update(ctx, msg)
		}
		in.state, in.view = stateExited, nil
		return cmd
	case termpane.CwdMsg:
		if m.InstanceID != in.id {
			return nil
		}
		env, dir, id := curEnv(), m.Dir, in.id
		return func() tea.Msg {
			c, cancel := context.WithTimeout(context.Background(), resolveTimeout)
			defer cancel()
			repo, err := lazygit.RepoRoot(c, env, dir)
			if err != nil {
				return nil
			}
			return cwdRepoMsg{id: id, repo: repo}
		}
	case cwdRepoMsg:
		if m.id == in.id && m.repo != "" && m.repo != in.repo {
			pushRecent(ctx, m.repo)
		}
		return nil
	}
	switch {
	case in.view != nil:
		return in.view.Update(ctx, msg)
	case in.picker != nil:
		return in.picker.Update(ctx, msg)
	}
	return nil
}

func (in *instance) Resize(ctx *ui.Context, size ui.Size) {
	in.size = size
	if in.view != nil {
		in.view.Resize(ctx, size)
	}
	if in.picker != nil {
		in.picker.Resize(ctx, size)
	}
}

func (in *instance) HandleKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	switch in.state {
	case stateRunning:
		if in.view != nil {
			return in.view.HandleKey(ctx, msg)
		}
	case statePicking:
		if in.picker != nil {
			return in.picker.HandleKey(ctx, msg)
		}
	case stateExited:
		switch msg.String() {
		case "enter":
			return true, in.start(ctx)
		case "x":
			return true, ui.CloseTool(in.id)
		}
	}
	return false, nil
}

func (in *instance) HandleMouse(ctx *ui.Context, msg tea.MouseMsg, local ui.Point) tea.Cmd {
	if in.view != nil {
		return in.view.HandleMouse(ctx, msg, local)
	}
	return nil
}

func (in *instance) HandleDrag(ctx *ui.Context, msg tea.MouseMsg, local ui.Point) tea.Cmd {
	if in.view != nil {
		return in.view.HandleDrag(ctx, msg, local)
	}
	return nil
}

func (in *instance) ClearDrag() {
	if in.view != nil {
		in.view.ClearDrag()
	}
}

func (in *instance) GrabsKeys() bool { return in.view != nil && in.view.GrabsKeys() }

func (in *instance) StatusText(ctx *ui.Context) (string, bool) {
	if in.view != nil {
		return in.view.StatusText(ctx)
	}
	return "", false
}

func (in *instance) Cursor() *tea.Cursor {
	switch {
	case in.view != nil:
		return in.view.Cursor()
	case in.picker != nil:
		return in.picker.Cursor()
	}
	return nil
}

func (in *instance) CapturesInput() bool { return in.picker != nil && in.picker.CapturesInput() }
func (in *instance) Completing() bool    { return in.picker != nil && in.picker.Completing() }

func (in *instance) Help(ctx *ui.Context) []key.Binding {
	switch {
	case in.view != nil:
		return in.view.Help(ctx)
	case in.state == stateExited:
		return []key.Binding{
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", ctx.T.Lazygit.Exited())),
			key.NewBinding(key.WithKeys("x"), key.WithHelp("x", ctx.T.Lazygit.ExitedClose())),
		}
	}
	return nil
}

// Title is "lazygit · <folder>" (A11.2: the folder name only).
func (in *instance) Title(ctx *ui.Context) (string, ui.Badge) {
	return ctx.T.Lazygit.Name() + " · " + folderName(in.repo), ui.Badge{}
}

func (in *instance) ToolStatus(ctx *ui.Context) string {
	t := ctx.T
	switch in.state {
	case stateRunning:
		return t.Lazygit.StatusRunning(modeName(ctx, in.mode))
	case stateExited:
		return t.Lazygit.StatusExited()
	case statePicking:
		return t.Lazygit.PickTitle()
	}
	return t.Lazygit.Starting()
}

func modeName(ctx *ui.Context, m termhost.Mode) string {
	switch m {
	case termhost.ModePane:
		return ctx.T.Tools.ModePane()
	case termhost.ModeWindow:
		return ctx.T.Tools.ModeWindow()
	}
	return ctx.T.Tools.ModeExec()
}

// CloseTool ends a pane still running (a screen switch or popup ends with
// lazygit itself).
func (in *instance) CloseTool(*ui.Context) tea.Cmd {
	if in.view != nil && in.state == stateRunning {
		h := in.view.Handle()
		in.view = nil
		return func() tea.Msg { _ = h.Close(); return nil }
	}
	return nil
}

func (in *instance) View(ctx *ui.Context, focused bool) string {
	w, h := in.size.W, in.size.H
	if w <= 0 || h <= 0 {
		return ""
	}
	switch {
	case in.view != nil:
		return in.view.View(ctx, focused)
	case in.picker != nil:
		return in.picker.View(ctx, focused)
	}
	th, t := themeOf(ctx), ctx.T
	var lines []string
	switch in.state {
	case stateExited:
		lines = []string{
			th.S.Base.Render(" " + t.Lazygit.Exited()),
			th.S.Dim.Render(" " + t.Lazygit.ExitedClose()),
		}
	case stateRunning:
		lines = []string{th.S.Dim.Render(" " + t.Lazygit.RunningElsewhere(modeName(ctx, in.mode)))}
	default:
		lines = []string{th.S.Dim.Render(" " + t.Lazygit.Starting())}
	}
	if ctx.Mode == layout.ModeTiny { // no border carries the title there
		title, _ := in.Title(ctx)
		lines = append([]string{th.S.Title.Render(" " + hubIcon + " " + textwidth.Sanitize(title))}, lines...)
	}
	out := make([]string, 0, h)
	for _, l := range lines {
		if len(out) == h {
			break
		}
		out = append(out, padStyled(th.S.Base.Render, textwidth.Truncate(l, w, "…"), w))
	}
	for len(out) < h {
		out = append(out, th.S.Base.Render(strings.Repeat(" ", w)))
	}
	return strings.Join(out, "\n")
}

// padStyled pads a rendered line to w cells with base-styled spaces.
func padStyled(base func(...string) string, s string, w int) string {
	if n := w - textwidth.Width(s); n > 0 {
		return s + base(strings.Repeat(" ", n))
	}
	return s
}
