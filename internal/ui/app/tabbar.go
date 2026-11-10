package app

import (
	"strings"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/widgets"
)

// tabItems returns the SlotTabs entries for the current mode and the active index
// (CA 5.4 "탭"):
//   - compact: every NavState tab root (home "홈", channel label + its My badge), then
//     the fixed DM (Store.DMUnread), 피드 and 알림 (Store.InboxBadge: notifications +
//     unread conversations);
//   - widget: 채팅 (Store.ChannelUnread, Mention when any) / DM / 피드 / 알림;
//   - tiny: the same four as 챗 / DM / 피 / 알.
//
// The active entry is Tabs[Active] (FrontTab), DM (FrontTab on the DM surface), or
// the feed/notifications entry.
func (m *Model) tabItems() ([]widgets.Tab, int) {
	st := m.ctx.Store
	notif := widgets.Tab{Label: "알림", Badge: ui.Badge{Count: st.InboxBadge()}}
	feed := widgets.Tab{Label: "피드"}
	du, dm := st.DMUnread()
	dms := widgets.Tab{Label: "DM", Badge: ui.Badge{Count: du, Mention: dm > 0}}
	if m.lay.Mode == layout.ModeCompact {
		var items []widgets.Tab
		for _, t := range m.nav.Tabs {
			items = append(items, m.rootTab(t.Stack[0]))
		}
		items = append(items, dms, feed, notif)
		if m.nav.toolTabs() > 0 {
			items = append(items, widgets.Tab{Label: m.ctx.T.Tools.TabLabel()})
		}
		return items, m.nav.clone().barPos()
	}
	unread, mentions := st.ChannelUnread()
	chat := widgets.Tab{Label: "채팅", Badge: ui.Badge{Count: unread, Mention: mentions > 0}}
	tools := widgets.Tab{Label: m.ctx.T.Tools.TabLabel()}
	if m.lay.Mode == layout.ModeTiny {
		chat.Label, feed.Label, notif.Label = "챗", "피", "알"
		tools.Label = m.ctx.T.Tools.TabLabelShort()
		tools.Badge = m.toolsTabBadge()
	}
	items := []widgets.Tab{chat, dms, feed, notif}
	if m.nav.toolTabs() > 0 { // the tool tab exists while a tool is open (A8.3)
		items = append(items, tools)
	}
	return items, m.nav.widgetPos()
}

// toolsTabBadge is the tiny tool cell's mark: a dot while an open tool
// instance's Titled badge has one — a shared pane (A7.7: tiny has no border,
// so the share mark is on the tab line).
func (m *Model) toolsTabBadge() ui.Badge {
	for _, r := range m.nav.Tools {
		if r.Kind != ui.RouteTool {
			continue
		}
		if in := m.cache.lookup(r.Key()); in != nil {
			if t, ok := in.comp.(ui.Titled); ok {
				if _, b := t.Title(m.ctx); b.Dot {
					return ui.Badge{Dot: true}
				}
			}
		}
	}
	return ui.Badge{}
}

// rootTab is the compact tab of a tab root.
func (m *Model) rootTab(r ui.Route) widgets.Tab {
	if r.Kind != ui.RouteChannel {
		return widgets.Tab{Label: m.routeTitle(r)}
	}
	t := widgets.Tab{Label: m.routeTitle(r)}
	if ch := m.ctx.Store.Channels[r.ID]; ch != nil {
		t.Badge = ui.Badge{Count: ch.My.UnreadCount, Mention: ch.My.MentionCount > 0}
	}
	return t
}

// tabBar renders SlotTabs at exactly w cells (widgets.Tabs scrolls to keep the active
// tab visible and marks hidden ones with ‹ ›).
func (m *Model) tabBar(w int) string {
	items, active := m.tabItems()
	m.tabs.Set(items, active)
	return m.tabs.View(m.ctx.Theme, w)
}

// contextLine renders the widget SlotContext: the chat surface's title — the active
// tab root's title, " · " and the space name for a space channel; on the DM surface
// the open DM's label, else "DM" — then " ▾" (CA 5.4).
func (m *Model) contextLine(w int) string {
	th := m.ctx.Theme
	if w <= 0 {
		return ""
	}
	text := ""
	if m.nav.Front == FrontTools { // the tool surface: the active tool's title (A8.3)
		text = m.toolContext()
	} else if t, ok := m.nav.activeTab(); ok {
		root := t.Stack[0]
		if root.Kind == ui.RouteDMs && len(t.Stack) >= 2 && t.Stack[1].Kind == ui.RouteChannel {
			root = t.Stack[1]
		}
		text = m.routeTitle(root)
		if root.Kind == ui.RouteChannel {
			if name := m.spaceName(root.ID); name != "" {
				text += " · " + name
			}
		}
	}
	text = textwidth.Sanitize(text) + " ▾"
	text = textwidth.Truncate(" "+text, w, "…")
	line := th.S.Dim.Render(text)
	if pad := w - textwidth.Width(text); pad > 0 {
		line += th.S.Base.Render(strings.Repeat(" ", pad))
	}
	return line
}

// spaceName is the name of the space a channel belongs to ("" for DMs/unknown).
func (m *Model) spaceName(channelID string) string {
	st := m.ctx.Store
	ch := st.Channels[channelID]
	if ch == nil || ch.SpaceID == nil || ch.Kind != api.ChannelText {
		return ""
	}
	if sp := st.Spaces[*ch.SpaceID]; sp != nil {
		return sp.Name
	}
	return ""
}
