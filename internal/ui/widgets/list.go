package widgets

import (
	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/ui"
)

// List is a selectable, scrollable list: selection by item ID, anchored scroll.
type List[T any] struct { /* selection by item ID, anchored scroll */
}

// ScrollAnchor makes scroll position width-independent: survives re-wrap on mode/width change.
type ScrollAnchor struct {
	ItemID     string
	LineOffset int
	AtBottom   bool
}

// NewList creates a list.
func NewList[T any](idOf func(T) string, render func(ctx *ui.Context, item T, selected bool, width int) []string) *List[T] {
	// TODO(core): Task 22
	return nil
}

// SetItems replaces items, keeping selection & top anchor by ID.
func (l *List[T]) SetItems(items []T) {
	// TODO(core): Task 22
}

// Selected returns the selected item.
func (l *List[T]) Selected() (T, bool) {
	// TODO(core): Task 22
	var zero T
	return zero, false
}

// Select selects by ID.
func (l *List[T]) Select(id string) {
	// TODO(core): Task 22
}

// HandleKey handles list navigation keys.
func (l *List[T]) HandleKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	// TODO(core): Task 22
	return false, nil
}

// HandleMouse handles clicks and wheel.
func (l *List[T]) HandleMouse(ctx *ui.Context, msg tea.MouseMsg, local ui.Point) tea.Cmd {
	// TODO(core): Task 22
	return nil
}

// View renders exactly size.
func (l *List[T]) View(ctx *ui.Context, size ui.Size) string {
	// TODO(core): Task 22
	return ""
}

// Anchor returns the current scroll anchor.
func (l *List[T]) Anchor() ScrollAnchor {
	// TODO(core): Task 22
	return ScrollAnchor{}
}

// Restore applies a scroll anchor.
func (l *List[T]) Restore(a ScrollAnchor) {
	// TODO(core): Task 22
}

// AtBottom reports whether the view is at the bottom.
func (l *List[T]) AtBottom() bool {
	// TODO(core): Task 22
	return false
}

// Follow sticks to bottom on new items (chat).
func (l *List[T]) Follow(bottom bool) {
	// TODO(core): Task 22
}
