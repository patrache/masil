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
