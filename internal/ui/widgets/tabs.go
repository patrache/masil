package widgets

import (
	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui"
)

// Tabs is a horizontal tab bar with badges and overflow arrows.
type Tabs struct { /* horizontal tab bar with badges and overflow arrows */
}

// Tab is one tab.
type Tab struct {
	Label string
	Badge ui.Badge
}

// NewTabs creates an empty tab bar.
func NewTabs() *Tabs {
	// TODO(core): Task 21
	return nil
}

// Set replaces the tabs.
func (t *Tabs) Set(items []Tab, active int) {
	// TODO(core): Task 21
}

// View renders the bar at width.
func (t *Tabs) View(th *theme.Theme, width int) string {
	// TODO(core): Task 21
	return ""
}

// Hit maps a column to a tab index.
func (t *Tabs) Hit(x int) (index int, ok bool) {
	// TODO(core): Task 21
	return 0, false
}
