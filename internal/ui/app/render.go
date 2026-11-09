package app

import (
	"maps"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/widgets"
)

// tooSmallText is the only thing drawn below 20x6 (CA 5.2.1).
const tooSmallText = "창을 조금만 키워 주세요"

// render composes the frame: every pane drawn at its rect (bordered panes through
// widgets.Frame), composited as lipgloss layers, then fit to exactly cols x rows. It
// records the frame geometry for Frame().
func (m *Model) render() string {
	m.frame = FrameInfo{Layout: m.lay, Routes: maps.Clone(m.routes)}
	w, h := m.cols, m.rows
	if !m.sized || w <= 0 || h <= 0 {
		return ""
	}
	if m.lay.Mode == layout.ModeTooSmall {
		return m.renderTooSmall()
	}
	layers := make([]*lipgloss.Layer, 0, len(m.lay.Panes))
	for _, p := range m.lay.Panes {
		s := m.renderPane(p)
		layers = append(layers, lipgloss.NewLayer(s).X(p.Rect.X).Y(p.Rect.Y).Z(0))
	}
	layers = append(layers, m.overlayLayers()...)
	if l := m.toastLayer(); l != nil {
		layers = append(layers, l)
	}
	out := lipgloss.NewCompositor(layers...).Render()
	return strings.Join(textwidth.Fit(strings.Split(out, "\n"), w, h), "\n")
}

// renderTooSmall draws Base blanks with the truncated notice on the middle row.
func (m *Model) renderTooSmall() string {
	w, h := m.cols, m.rows
	base := m.ctx.Theme.S.Base
	blank := base.Render(strings.Repeat(" ", w))
	lines := make([]string, h)
	for i := range lines {
		lines[i] = blank
	}
	lines[h/2] = base.Render(textwidth.Center(textwidth.Truncate(tooSmallText, w, "…"), w))
	return strings.Join(lines, "\n")
}

// renderPane draws one pane at exactly its rect size.
func (m *Model) renderPane(p layout.Pane) string {
	size := ui.Size{W: p.Rect.W, H: p.Rect.H}
	switch p.Slot {
	case layout.SlotStatus:
		return m.statusLine(p.Rect.W)
	case layout.SlotTabs:
		return m.tabBar(p.Rect.W)
	case layout.SlotContext:
		return m.contextLine(p.Rect.W)
	}
	in := m.slotInstance(p.Slot)
	if in == nil { // a route slot with nothing projected: keep the cell styled
		in = &instance{comp: widgets.Placeholder(placeholderText), size: innerSize(p)}
		in.comp.Resize(m.ctx, in.size)
	}
	focused := p.Slot == m.focus.Slot
	body := in.comp.View(m.ctx, focused)
	if !p.Bordered {
		return strings.Join(textwidth.Fit(strings.Split(body, "\n"), size.W, size.H), "\n")
	}
	title, badge := m.paneTitle(p.Slot, in)
	return widgets.Frame(m.ctx.Theme, title, badge, focused, size, body)
}

// paneTitle is the component's Titled result, else the route's default title
// (pane-factory placeholders have none).
func (m *Model) paneTitle(s layout.SlotID, in *instance) (string, ui.Badge) {
	if t, ok := in.comp.(ui.Titled); ok {
		return t.Title(m.ctx)
	}
	if r, ok := m.routes[s]; ok {
		return m.routeTitle(r), ui.Badge{}
	}
	return "", ui.Badge{}
}

// routeTitle is the default title of a route (CA 5.4 / plan 03 Task 25).
func (m *Model) routeTitle(r ui.Route) string {
	switch r.Kind {
	case ui.RouteHome:
		return "홈"
	case ui.RouteDMs:
		return "DM"
	case ui.RouteChannel:
		if l := m.ctx.Store.ChannelLabel(r.ID); l != "" {
			return l
		}
		return "채널"
	case ui.RouteThread:
		return "스레드"
	case ui.RouteFeed:
		return "피드"
	case ui.RouteNotifications:
		return "알림"
	case ui.RouteSettings:
		return "설정"
	case ui.RouteHelp:
		return "도움말"
	}
	return string(r.Kind)
}

// currentTitle is the title of the focused route (WindowTitle); without a projected
// route (TooSmall) it is the surface Main would show.
func (m *Model) currentTitle() string {
	r, ok := m.routes[m.focus.Slot]
	if !ok {
		r, ok = m.routes[layout.SlotMain]
	}
	if !ok {
		r = m.nav.clone().shownMain(m.lay.Mode)
	}
	title := m.routeTitle(r)
	if in := m.cache.lookup(r.Key()); in != nil {
		if t, ok := in.comp.(ui.Titled); ok {
			if s, _ := t.Title(m.ctx); s != "" {
				title = s
			}
		}
	}
	return textwidth.Sanitize(title)
}
