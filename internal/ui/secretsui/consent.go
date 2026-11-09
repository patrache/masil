// Package secretsui is the tool-secret dialogs (v0.1.3 spec A9.2·A9.4): the
// consent question shown when the OS store cannot be used, and the masked
// value prompt (a modal outside any panel). Both are registered overlays and
// answer with a broadcast msg the asking tool handles.
package secretsui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/secrets"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
)

// Registered overlay ids.
const (
	OverlayConsent = "secrets.consent" // args = tool id
	OverlayPrompt  = "secrets.prompt"  // args = JSON {"tool", "name", "label"}
)

// ConsentMsg is broadcast when the consent dialog closes. The dialog has
// already called ctx.Secrets.SetFallback(Choice), so the tool simply retries the
// same Put. Choice FallbackUnset = canceled (nothing was set).
type ConsentMsg struct {
	Tool   string
	Choice secrets.Fallback
}

func init() {
	ui.RegisterOverlay(OverlayConsent, newConsent)
	ui.RegisterOverlay(OverlayPrompt, newPrompt)
}

// BackendLabel is where a value is kept, as the settings and API screens show
// it (A9.2 "보관 위치를 숨기지 않는다"); "" for an unknown backend. nil t = ko.
func BackendLabel(t *i18n.T, b secrets.Backend) string {
	if t == nil {
		t = i18n.KoT()
	}
	switch b {
	case secrets.BackendKeychain:
		return t.Tools.BackendKeychain()
	case secrets.BackendCredMan:
		return t.Tools.BackendCredman()
	case secrets.BackendSecretService:
		return t.Tools.BackendSecretService()
	case secrets.BackendFile:
		return t.Tools.BackendFile()
	case secrets.BackendMemory:
		return t.Tools.BackendMemory()
	}
	return ""
}

// Dialog outer sizes (centered; the app clamps them to the body and drops the
// border when they do not fit).
const (
	consentW, consentH = 60, 11
	promptW, promptH   = 56, 8
)

func centered(body layout.Rect, w, h int) layout.Rect {
	return layout.Rect{X: body.X + max(0, (body.W-w)/2), Y: body.Y + max(0, (body.H-h)/2), W: w, H: h}
}

func closeOverlay() tea.Msg { return ui.CloseOverlayMsg{} }

func emit(msg tea.Msg) tea.Cmd { return func() tea.Msg { return msg } }

func themeOf(ctx *ui.Context) *theme.Theme {
	if ctx != nil && ctx.Theme != nil {
		return ctx.Theme
	}
	return theme.Get(theme.Default)
}

func catalog(ctx *ui.Context) *i18n.T {
	if ctx != nil && ctx.T != nil {
		return ctx.T
	}
	return i18n.KoT()
}

// toolTitle is the tool's display name ("" when it is not registered or off).
func toolTitle(ctx *ui.Context, tool string) string {
	if s, ok := ui.ToolByID(tool); ok && s.Name != nil {
		return s.Name(catalog(ctx))
	}
	return ""
}

// line is one dialog row: plain text and its style.
type line struct {
	text  string
	style func(...string) string
	keep  bool // never dropped to make room (the consent question, the choices)
}

// render lays rows into exactly size (one blank column each side when the
// dialog is wide enough). Rows that do not fit are dropped from the bottom,
// blanks and droppable rows first; focus (a row index) stays visible and its
// screen row is returned (-1 when nothing is drawn).
func render(t *theme.Theme, rows []line, size ui.Size, focus int) (string, int) {
	w, h := size.W, size.H
	if w <= 0 || h <= 0 {
		return "", -1
	}
	for len(rows) > h {
		i := lastDroppable(rows)
		if i < 0 {
			break
		}
		if i < focus {
			focus--
		}
		rows = append(rows[:i:i], rows[i+1:]...)
	}
	top := 0
	if len(rows) > h && focus >= h {
		top = focus - h + 1
	}
	p := 0
	if w >= 12 {
		p = 1
	}
	cw := max(1, w-2*p)
	blank := t.S.Base.Render(strings.Repeat(" ", w))
	out := make([]string, h)
	for y := range out {
		i := top + y
		if i >= len(rows) {
			out[y] = blank
			continue
		}
		r := rows[i]
		txt := textwidth.PadRight(textwidth.Truncate(r.text, cw, ""), cw)
		st := r.style
		if st == nil {
			st = t.S.Base.Render
		}
		out[y] = t.S.Base.Render(strings.Repeat(" ", p)) + st(txt) + t.S.Base.Render(strings.Repeat(" ", w-p-cw))
	}
	return strings.Join(out, "\n"), focus - top
}

// lastDroppable is the last blank row, else the last droppable row (-1: none).
func lastDroppable(rows []line) int {
	for i := len(rows) - 1; i >= 0; i-- {
		if rows[i].text == "" && !rows[i].keep {
			return i
		}
	}
	for i := len(rows) - 1; i >= 0; i-- {
		if !rows[i].keep {
			return i
		}
	}
	return -1
}

// wrapped is s sanitized and wrapped to w cells, one keep row per line.
func wrapped(s string, w int, style func(...string) string) []line {
	var out []line
	for _, l := range textwidth.Wrap(textwidth.Sanitize(s), max(1, w)) {
		out = append(out, line{text: l, style: style, keep: true})
	}
	return out
}

// contentWidth is the text width inside a dialog of width w.
func contentWidth(w int) int {
	if w >= 12 {
		return max(1, w-2)
	}
	return max(1, w)
}

// consent is the OverlayConsent dialog (A9.2): ① file ② this run (default).
type consent struct {
	tool string
	sel  secrets.Fallback
	size ui.Size
}

var (
	_ ui.Overlay    = (*consent)(nil)
	_ ui.KeyHandler = (*consent)(nil)
	_ ui.Titled     = (*consent)(nil)
)

func newConsent(_ *ui.Context, args string) ui.Overlay {
	return &consent{tool: strings.TrimSpace(args), sel: secrets.FallbackSession}
}

func (d *consent) Init(*ui.Context) tea.Cmd            { return nil }
func (d *consent) Update(*ui.Context, tea.Msg) tea.Cmd { return nil }
func (d *consent) Resize(_ *ui.Context, s ui.Size)     { d.size = s }
func (d *consent) Modal() bool                         { return true }
func (d *consent) Bounds(body layout.Rect) layout.Rect {
	return centered(body, consentW, consentH)
}
func (d *consent) Title(ctx *ui.Context) (string, ui.Badge) {
	return toolTitle(ctx, d.tool), ui.Badge{}
}

// choose sets the fallback first, then closes and broadcasts (the tool's retried
// Put then finds it set).
func (d *consent) choose(ctx *ui.Context, f secrets.Fallback) tea.Cmd {
	if f != secrets.FallbackUnset && ctx != nil && ctx.Secrets != nil {
		ctx.Secrets.SetFallback(f)
	}
	return tea.Batch(closeOverlay, emit(ConsentMsg{Tool: d.tool, Choice: f}))
}

func (d *consent) HandleKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	switch msg.String() {
	case "1", "up", "k", "shift+tab":
		d.sel = secrets.FallbackFile
	case "2", "down", "j", "tab":
		d.sel = secrets.FallbackSession
	case "enter", "space":
		return true, d.choose(ctx, d.sel)
	case "esc":
		return true, d.choose(ctx, secrets.FallbackUnset)
	case "ctrl+c":
		return false, nil
	}
	return true, nil // modal: every other key stops here
}

func (d *consent) View(ctx *ui.Context, _ bool) string {
	t, tr := themeOf(ctx), catalog(ctx)
	cw := contentWidth(d.size.W)
	rows := wrapped(tr.Tools.ConsentQuestion(), cw, t.S.Base.Render)
	rows = append(rows, line{})
	focus := 0
	for _, c := range []struct {
		f     secrets.Fallback
		label string
	}{{secrets.FallbackFile, tr.Tools.ConsentFile()}, {secrets.FallbackSession, tr.Tools.ConsentSession()}} {
		if c.f == d.sel {
			focus = len(rows)
			rows = append(rows, line{text: "› " + c.label, style: t.S.Selected.Render, keep: true})
		} else {
			rows = append(rows, line{text: "  " + c.label, keep: true})
		}
	}
	rows = append(rows, line{}, line{text: tr.Tools.ConsentFooter(), style: t.S.Dim.Render})
	s, _ := render(t, rows, d.size, focus)
	return s
}
