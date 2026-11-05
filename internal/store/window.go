package store

import "time"

// Window is a generic ordered window (ascending by ID) used by chatstore/feedstore.
type Window[T any] struct { /* unexported */
}

// NewWindow creates an empty window.
func NewWindow[T any](idOf func(T) string, timeOf func(T) time.Time) *Window[T] {
	// TODO(core): Task 14
	return nil
}

// Items returns the items ascending.
func (w *Window[T]) Items() []T {
	// TODO(core): Task 14
	return nil
}

// Len returns the item count.
func (w *Window[T]) Len() int {
	// TODO(core): Task 14
	return 0
}

// Get returns the item with that ID.
func (w *Window[T]) Get(id string) (T, bool) {
	// TODO(core): Task 14
	var zero T
	return zero, false
}

// Upsert inserts by ID order, replacing the same ID.
func (w *Window[T]) Upsert(items ...T) {
	// TODO(core): Task 14
}

// Update applies fn to the item with that ID.
func (w *Window[T]) Update(id string, fn func(*T)) bool {
	// TODO(core): Task 14
	return false
}

// Remove deletes the item with that ID.
func (w *Window[T]) Remove(id string) bool {
	// TODO(core): Task 14
	return false
}

// Replace replaces every item.
func (w *Window[T]) Replace(items []T, hasMoreBefore, hasMoreAfter bool) {
	// TODO(core): Task 14
}

// PrependOlder adds an older page.
func (w *Window[T]) PrependOlder(items []T, hasMoreBefore bool) {
	// TODO(core): Task 14
}

// AppendNewer adds a newer page.
func (w *Window[T]) AppendNewer(items []T, hasMoreAfter bool) {
	// TODO(core): Task 14
}

// HasMoreBefore reports older items on the server.
func (w *Window[T]) HasMoreBefore() bool {
	// TODO(core): Task 14
	return false
}

// HasMoreAfter reports newer items on the server.
func (w *Window[T]) HasMoreAfter() bool {
	// TODO(core): Task 14
	return false
}

// Covers reports first.ID <= id <= last.ID (or hasMoreAfter==false and id > last).
func (w *Window[T]) Covers(id string) bool {
	// TODO(core): Task 14
	return false
}

// OverlapAnchor (events.md 5.3): newest item with time <= last.time-overlap; "" if none.
func (w *Window[T]) OverlapAnchor(overlap time.Duration) string {
	// TODO(core): Task 14
	return ""
}

// Clear empties the window.
func (w *Window[T]) Clear() {
	// TODO(core): Task 14
}
