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
