package widgets

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui"
)

// placeholder is the component drawn for unregistered slots/routes.
type placeholder struct {
	text string
	size ui.Size
}

// Placeholder is drawn for unregistered slots/routes ("준비 중").
func Placeholder(text string) ui.Component {
	return &placeholder{text: text}
}

func (p *placeholder) Init(*ui.Context) tea.Cmd            { return nil }
func (p *placeholder) Update(*ui.Context, tea.Msg) tea.Cmd { return nil }
func (p *placeholder) Resize(_ *ui.Context, size ui.Size)  { p.size = size }
func (p *placeholder) View(ctx *ui.Context, _ bool) string {
	return Empty(themeOf(ctx), p.size, "…", p.text, "")
}

// themeOf is ctx.Theme, or the default theme without a context.
func themeOf(ctx *ui.Context) *theme.Theme {
	if ctx != nil && ctx.Theme != nil {
		return ctx.Theme
	}
	return theme.Get(theme.Default)
}

// Empty draws an empty-state box: icon, a blank row, title (Title) and hint (Dim),
// centered both ways in exactly size.H lines x size.W cells. When rows run short the
// icon (and its blank) go first, then the hint; the title always stays.
func Empty(t *theme.Theme, size ui.Size, icon, title, hint string) string {
	w, h := size.W, size.H
	if w <= 0 || h <= 0 {
		return ""
	}
	type row struct {
		text  string
		style func(string) string
	}
	icon, title, hint = textwidth.Sanitize(icon), textwidth.Sanitize(title), textwidth.Sanitize(hint)
	titleRow := row{title, func(s string) string { return t.S.Title.Render(s) }}
	hintRow := row{hint, func(s string) string { return t.S.Dim.Render(s) }}
	iconRow := row{icon, func(s string) string { return t.S.Base.Render(s) }}
	blank := row{"", nil}

	var rows []row
	build := func(withIcon, withHint bool) []row {
		var r []row
		if withIcon && icon != "" {
			r = append(r, iconRow, blank)
		}
		r = append(r, titleRow)
		if withHint && hint != "" {
			r = append(r, hintRow)
		}
		return r
	}
	for _, opt := range [][2]bool{{true, true}, {false, true}, {false, false}} {
		if rows = build(opt[0], opt[1]); len(rows) <= h {
			break
		}
	}
	top := (h - len(rows)) / 2
	out := make([]string, h)
	for i := range out {
		j := i - top
		if j < 0 || j >= len(rows) || rows[j].text == "" {
			out[i] = pad(t, w)
			continue
		}
		s := textwidth.Truncate(rows[j].text, w, "…")
		gap := w - textwidth.Width(s)
		left := gap / 2
		out[i] = pad(t, left) + rows[j].style(s) + pad(t, gap-left)
	}
	return strings.Join(out, "\n")
}
