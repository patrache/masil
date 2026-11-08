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
//     the fixed 피드 and 알림 (NotifUnread);
//   - widget: 채팅 (TotalUnread, Mention when any) / 피드 / 알림;
//   - tiny: the same three as 챗 / 피 / 알.
//
// The active entry is Tabs[Active] (Front==FrontTab) or the feed/notifications entry.
func (m *Model) tabItems() ([]widgets.Tab, int) {
	st := m.ctx.Store
	notif := widgets.Tab{Label: "알림", Badge: ui.Badge{Count: st.NotifUnread}}
	feed := widgets.Tab{Label: "피드"}
	if m.lay.Mode == layout.ModeCompact {
		var items []widgets.Tab
		for _, t := range m.nav.Tabs {
			items = append(items, m.rootTab(t.Stack[0]))
		}
		active := m.nav.Active
		switch m.nav.Front {
		case FrontFeed:
			active = len(items)
		case FrontNotifications:
			active = len(items) + 1
		}
		return append(items, feed, notif), active
	}
	unread, mentions := st.TotalUnread()
	chat := widgets.Tab{Label: "채팅", Badge: ui.Badge{Count: unread, Mention: mentions > 0}}
	if m.lay.Mode == layout.ModeTiny {
		chat.Label, feed.Label, notif.Label = "챗", "피", "알"
	}
	return []widgets.Tab{chat, feed, notif}, int(m.nav.Front)
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

// contextLine renders the widget SlotContext: the active tab root's title, " · " and
// the space name for a space channel, then " ▾" (CA 5.4).
func (m *Model) contextLine(w int) string {
	th := m.ctx.Theme
	if w <= 0 {
		return ""
	}
	text := ""
	if t, ok := m.nav.activeTab(); ok {
		root := t.Stack[0]
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
