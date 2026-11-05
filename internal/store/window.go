package store

import (
	"sort"
	"time"
)

// Window is a generic ordered window (ascending by ID) used by chatstore/feedstore.
// IDs are compared as strings (events.md 5.3: sort comparison only, never parsed).
type Window[T any] struct { /* unexported */
	idOf          func(T) string
	timeOf        func(T) time.Time
	items         []T
	hasMoreBefore bool
	hasMoreAfter  bool
}

// NewWindow creates an empty window.
func NewWindow[T any](idOf func(T) string, timeOf func(T) time.Time) *Window[T] {
	return &Window[T]{idOf: idOf, timeOf: timeOf}
}

// search returns the index of id (or where it would be inserted) and whether it is present.
func (w *Window[T]) search(id string) (int, bool) {
	i := sort.Search(len(w.items), func(i int) bool { return w.idOf(w.items[i]) >= id })
	return i, i < len(w.items) && w.idOf(w.items[i]) == id
}

// Items returns the items ascending (a copy).
func (w *Window[T]) Items() []T {
	out := make([]T, len(w.items))
	copy(out, w.items)
	return out
}

// Len returns the item count.
func (w *Window[T]) Len() int { return len(w.items) }

// Get returns the item with that ID.
func (w *Window[T]) Get(id string) (T, bool) {
	if i, ok := w.search(id); ok {
		return w.items[i], true
	}
	var zero T
	return zero, false
}

// Upsert inserts by ID order, replacing the same ID.
func (w *Window[T]) Upsert(items ...T) {
	for _, it := range items {
		i, ok := w.search(w.idOf(it))
		if ok {
			w.items[i] = it
			continue
		}
		var zero T
		w.items = append(w.items, zero)
		copy(w.items[i+1:], w.items[i:])
		w.items[i] = it
	}
}

// Update applies fn to the item with that ID. fn must not change the ID.
func (w *Window[T]) Update(id string, fn func(*T)) bool {
	i, ok := w.search(id)
	if !ok {
		return false
	}
	fn(&w.items[i])
	return true
}

// Remove deletes the item with that ID.
func (w *Window[T]) Remove(id string) bool {
	i, ok := w.search(id)
	if !ok {
		return false
	}
	w.items = append(w.items[:i], w.items[i+1:]...)
	return true
}

// Replace replaces every item (sorted by ID, deduplicated: later items win).
func (w *Window[T]) Replace(items []T, hasMoreBefore, hasMoreAfter bool) {
	w.items = nil
	w.Upsert(items...)
	w.hasMoreBefore, w.hasMoreAfter = hasMoreBefore, hasMoreAfter
}

// PrependOlder adds an older page (merged by ID).
func (w *Window[T]) PrependOlder(items []T, hasMoreBefore bool) {
	w.Upsert(items...)
	w.hasMoreBefore = hasMoreBefore
}

// AppendNewer adds a newer page (merged by ID).
func (w *Window[T]) AppendNewer(items []T, hasMoreAfter bool) {
	w.Upsert(items...)
	w.hasMoreAfter = hasMoreAfter
}

// HasMoreBefore reports older items on the server.
func (w *Window[T]) HasMoreBefore() bool { return w.hasMoreBefore }

// HasMoreAfter reports newer items on the server.
func (w *Window[T]) HasMoreAfter() bool { return w.hasMoreAfter }

// Covers reports first.ID <= id <= last.ID (or hasMoreAfter==false and id > last).
// An empty window covers nothing.
func (w *Window[T]) Covers(id string) bool {
	if len(w.items) == 0 {
		return false
	}
	first, last := w.idOf(w.items[0]), w.idOf(w.items[len(w.items)-1])
	if id < first {
		return false
	}
	return id <= last || !w.hasMoreAfter
}

// OverlapAnchor (events.md 5.3): newest item with time <= last.time-overlap; "" if none.
func (w *Window[T]) OverlapAnchor(overlap time.Duration) string {
	if len(w.items) == 0 {
		return ""
	}
	cutoff := w.timeOf(w.items[len(w.items)-1]).Add(-overlap)
	for i := len(w.items) - 1; i >= 0; i-- {
		if !w.timeOf(w.items[i]).After(cutoff) {
			return w.idOf(w.items[i])
		}
	}
	return ""
}

// Clear empties the window and resets both has-more flags.
func (w *Window[T]) Clear() {
	w.items = nil
	w.hasMoreBefore, w.hasMoreAfter = false, false
}
