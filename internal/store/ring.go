package store

// Ring is a fixed-capacity ring buffer.
type Ring[T any] struct { /* fixed-capacity */
}

// NewRing creates a ring of capacity n.
func NewRing[T any](n int) *Ring[T] {
	// TODO(core): Task 14
	return nil
}

// Push adds v, dropping the oldest when full.
func (r *Ring[T]) Push(v T) {
	// TODO(core): Task 14
}

// Items returns the items newest first.
func (r *Ring[T]) Items() []T {
	// TODO(core): Task 14
	return nil
}
