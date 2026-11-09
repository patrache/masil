package workspace

import (
	"errors"
	"log/slog"
	"runtime"
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/patrache/masil/internal/termhost"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/termpane"
	"github.com/patrache/masil/internal/workspace/hooks"
	"github.com/patrache/masil/internal/workspace/launch"
)

// The terminal cell (spec B6.4): programs started in the folder, each its
// own termpane instance ("<surface id>#<n>"), one shown.

const termhostExec = termhost.ModeExec

// paneSupported reports whether embedded panes can run here (not on
// Windows; export_test.go swaps it).
var paneSupported = func() bool { return runtime.GOOS != "windows" }

type term struct {
	id      string
	n       int
	name    string
	req     termhost.Request // as started (Task 21 checks Argv)
	mode    termhost.Mode
	view    *termpane.Pane
	pane    *termhost.Pane // pane mode with the real launcher (nil in tests)
	cleanup func()
	// attention: the program asked for the user (hooks, BEL / OSC 9) — Task 22
	attention bool
}

// Argv is the program and its arguments as started.
func (t *term) Argv() []string { return append([]string{t.req.Program}, t.req.Args...) }

func (t *term) routeKey() string {
	return ui.Route{Kind: ui.RouteTool, ID: t.id, Arg: ui.ToolWorkspace}.Key()
}

// close ends the program and removes its settings file.
func (t *term) close() {
	if t.view != nil {
		_ = t.view.Handle().Close()
	}
	t.cleanup()
}

type termCell struct {
	terms []*term          // pane-mode terminals, shown in the cell
	execs map[string]*term // exec/window-mode programs until they end
	cur   int
	next  int
}

func (c *termCell) current() *term {
	if c.cur < 0 || c.cur >= len(c.terms) {
		return nil
	}
	return c.terms[c.cur]
}

func (c *termCell) count() int { return len(c.terms) }

func (c *termCell) byID(id string) (*term, int) {
	for i, t := range c.terms {
		if t.id == id {
			return t, i
		}
	}
	return nil, -1
}

// termMode is how a new terminal opens: a pane, or a screen switch where
// panes cannot run.
func (s *Surface) termMode() termhost.Mode {
	if paneSupported() {
		return termhost.ModePane
	}
	return termhost.ModeExec
}

// launchTerm starts argv in the folder.
func (s *Surface) launchTerm(ctx *ui.Context, argv []string, mode termhost.Mode) tea.Cmd {
	if len(argv) == 0 || argv[0] == "" {
		return nil
	}
	s.term.next++
	n := s.term.next
	id := s.id + "#" + strconv.Itoa(n)
	name := launch.ProgramName(argv[0])
	out, cleanup := argv, func() {}
	if launch.IsClaude(argv) {
		url := ""
		if r := s.hookReceiver(ctx); r != nil {
			url = r.URL(strconv.Itoa(n))
		}
		o, c, err := launch.Prepare(argv, url, ctx.Paths.Cache)
		if err != nil {
			slog.Warn("workspace: claude settings", "err", err)
			return ui.Toast(ctx.T.Workspace.OpenFailed(), ui.ToastError)
		}
		out, cleanup = o, c
	}
	req := termhost.Request{Program: out[0], Args: out[1:], Dir: s.dir, Title: name, ToolID: ui.ToolWorkspace, Mode: mode}
	opts := termpane.Options{ToolID: ui.ToolWorkspace, InstanceID: id, OnPrefix: s.prefixKey}
	l, cmd, err := termpane.Launch(ctx, req, opts)
	if err != nil {
		cleanup()
		if errors.Is(err, termhost.ErrPaneLimit) {
			return cmd
		}
		return tea.Batch(cmd, ui.Toast(ctx.T.Workspace.OpenFailed(), ui.ToastError))
	}
	t := &term{id: id, n: n, name: name, req: req, mode: l.Mode, view: l.View, pane: l.Pane, cleanup: cleanup}
	if l.Mode == termhost.ModePane && l.View != nil {
		s.term.terms = append(s.term.terms, t)
		s.term.cur = len(s.term.terms) - 1
		s.focus = PaneTerm
		s.relayout(ctx)
		s.onTermStarted(ctx, t)
	} else {
		if s.term.execs == nil {
			s.term.execs = map[string]*term{}
		}
		s.term.execs[id] = t
	}
	return cmd
}

// termExited drops an ended program.
func (s *Surface) termExited(ctx *ui.Context, m termpane.ExitedMsg) tea.Cmd {
	if t, ok := s.term.execs[m.InstanceID]; ok {
		t.cleanup()
		delete(s.term.execs, m.InstanceID)
		return nil
	}
	t, i := s.term.byID(m.InstanceID)
	if t == nil {
		return nil
	}
	cmd := t.view.Update(ctx, m)
	t.cleanup()
	s.onTermEnded(ctx, t)
	s.term.terms = slices.Delete(s.term.terms, i, i+1)
	if s.term.cur >= len(s.term.terms) {
		s.term.cur = len(s.term.terms) - 1
	}
	if s.term.cur < 0 {
		s.term.cur = 0
	}
	s.relayout(ctx)
	return cmd
}

// hookReceiver starts the workspace's hook endpoint on first use.
func (s *Surface) hookReceiver(ctx *ui.Context) *hooks.Receiver {
	if s.hooks != nil {
		return s.hooks
	}
	r, err := hooks.Listen()
	if err != nil {
		slog.Warn("workspace: hook receiver", "err", err)
		return nil
	}
	s.hooks = r
	post := ctx.Post
	id := s.id
	if post != nil {
		go func() {
			for ev := range r.Events() {
				post.Post(hookMsg{surface: id, ev: ev})
			}
		}()
	}
	return r
}

// hookMsg is a hook call of one of this surface's programs.
type hookMsg struct {
	surface string
	ev      hooks.Event
}

// termKey handles a key in the terminal cell while no program has the keyboard.
func (s *Surface) termKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	t := s.term.current()
	if t != nil && t.view != nil {
		if ok, cmd := t.view.HandleKey(ctx, msg); ok {
			return true, cmd
		}
	}
	switch msg.String() {
	case "n":
		s.openNewTerm(ctx, s.termMode())
		return true, nil
	case "e":
		s.openNewTerm(ctx, termhost.ModeExec)
		return true, nil
	case "[":
		if len(s.term.terms) > 1 {
			s.term.cur = (s.term.cur + len(s.term.terms) - 1) % len(s.term.terms)
			s.relayout(ctx)
		}
		return true, nil
	case "]":
		if len(s.term.terms) > 1 {
			s.term.cur = (s.term.cur + 1) % len(s.term.terms)
			s.relayout(ctx)
		}
		return true, nil
	}
	return false, nil
}

// view draws the terminal cell: a header line with the terminals, then the
// shown program.
func (c *termCell) view(ctx *ui.Context, s *Surface, w, h int, focused bool) []string {
	th := ctx.Theme
	t := ctx.T.Workspace
	dim := lipgloss.NewStyle().Foreground(th.P.Dim)
	line := lipgloss.NewStyle().Foreground(th.P.Line)
	var tabs []string
	for i, x := range c.terms {
		label := strconv.Itoa(x.n) + " " + textwidth.Sanitize(x.name)
		if x.attention {
			label += " ●"
		}
		switch {
		case i == c.cur && focused:
			tabs = append(tabs, th.S.TabActive.Render(" "+label+" "))
		case i == c.cur:
			tabs = append(tabs, th.S.Title.Render(" "+label+" "))
		default:
			tabs = append(tabs, dim.Render(" "+label+" "))
		}
	}
	head := line.Render("─") + strings.Join(tabs, line.Render("─"))
	if n := len(s.docs.Dirty()); n > 0 && len(c.terms) > 0 {
		tail := " " + t.TermTitleUnsaved(n) + " "
		head = textwidth.Truncate(head, max(w-textwidth.Width(tail)-1, 1), "…")
		fill := max(w-textwidth.Width(head)-textwidth.Width(tail), 0)
		head += line.Render(strings.Repeat("─", fill)) + th.S.Warning.Render(tail)
	} else {
		head = textwidth.Truncate(head, w, "…")
		head += line.Render(strings.Repeat("─", max(w-textwidth.Width(head), 0)))
	}
	lines := []string{head}
	cur := c.current()
	switch {
	case !paneSupported():
		lines = append(lines, "")
		for _, l := range textwidth.Wrap(t.WindowsNoTerminal("e"), max(w-2, 1)) {
			lines = append(lines, " "+th.S.Warning.Render(l))
		}
	case cur == nil || cur.view == nil:
		lines = append(lines, "", " "+dim.Render(textwidth.Truncate(t.TermEmptyHint(), max(w-2, 1), "…")))
	default:
		lines = append(lines, strings.Split(cur.view.View(ctx, focused), "\n")...)
	}
	return lines
}
