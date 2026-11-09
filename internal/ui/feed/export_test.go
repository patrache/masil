package feed

import "github.com/patrache/masil/internal/ui"

// SelectedID is the selected card's item ID; "" while the new-posts banner is
// selected (or c is not a timeline).
func SelectedID(c ui.Component) string {
	t, ok := c.(*timeline)
	if !ok || t.bannerSel {
		return ""
	}
	it, ok := t.list.Selected()
	if !ok {
		return ""
	}
	return it.ID
}

// AnchorID is the card List's scroll anchor item ID.
func AnchorID(c ui.Component) string {
	t, ok := c.(*timeline)
	if !ok {
		return ""
	}
	return t.list.Anchor().ItemID
}

// TrendsQuery is the trends search query (the input's normalized value).
func TrendsQuery(c ui.Component) string {
	if t, ok := c.(*trends); ok {
		return t.query
	}
	return ""
}

// TrendsSearchOpen reports whether the trends search line is open.
func TrendsSearchOpen(c ui.Component) bool {
	t, ok := c.(*trends)
	return ok && t.searchOpen
}

// TrendsInputFocused reports whether the trends search input has the focus.
func TrendsInputFocused(c ui.Component) bool {
	t, ok := c.(*trends)
	return ok && t.searchOpen && t.inputFocus
}

// TrendsSelectedID is the selected row of the shown trends list: a tag (Top 10
// or tag result), "open:<tag>" for the open row, or a post result's item ID.
func TrendsSelectedID(c ui.Component) string {
	if t, ok := c.(*trends); ok {
		return t.selectedID()
	}
	return ""
}

// NotifIDs is the shown notification IDs, top row first.
func NotifIDs(c ui.Component) []string {
	n, ok := c.(*notifications)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(n.items))
	for _, it := range n.items {
		out = append(out, it.ID)
	}
	return out
}

// ImagesRetained is how many decoded images a card screen holds (timeline,
// hashtag, trends, post, profile); -1 for any other component.
func ImagesRetained(c ui.Component) int {
	switch v := c.(type) {
	case *timeline:
		return v.imgs.retained()
	case *hashtag:
		return v.imgs.retained()
	case *trends:
		return v.imgs.retained()
	case *postView:
		return v.imgs.retained()
	case *profile:
		return v.imgs.retained()
	}
	return -1
}

// NotifRowIDs is every 알림함 row ID, top first: "conv:<channel>" and
// "conv:thread:<root>" for the 안 읽은 대화 section, then notification IDs.
func NotifRowIDs(c ui.Component) []string {
	n, ok := c.(*notifications)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(n.rows))
	for _, r := range n.rows {
		out = append(out, r.id())
	}
	return out
}

// SelectNotifRow selects the 알림함 row with this ID (false when absent).
func SelectNotifRow(c ui.Component, id string) bool {
	n, ok := c.(*notifications)
	if !ok {
		return false
	}
	for _, r := range n.rows {
		if r.id() == id {
			n.list.Select(id)
			return true
		}
	}
	return false
}

// NotifConvPreview is the 안 읽은 대화 row's preview body ("" = the 새 메시지
// fallback) and whether the row exists.
func NotifConvPreview(c ui.Component, id string) (string, bool) {
	n, ok := c.(*notifications)
	if !ok {
		return "", false
	}
	for _, r := range n.convs {
		if r.key == id {
			return r.body, true
		}
	}
	return "", false
}

// NotifSelectedRow is the selected 알림함 row's ID ("" = none).
func NotifSelectedRow(c ui.Component) string {
	n, ok := c.(*notifications)
	if !ok {
		return ""
	}
	if r, ok := n.list.Selected(); ok {
		return r.id()
	}
	return ""
}
