package store

// Ring is a fixed-capacity ring buffer.
type Ring[T any] struct { /* fixed-capacity */
	buf  []T
	next int // index the next Push writes
	n    int // items held
}

// NewRing creates a ring of capacity n (n < 1 is treated as 1).
func NewRing[T any](n int) *Ring[T] {
	if n < 1 {
		n = 1
	}
	return &Ring[T]{buf: make([]T, n)}
}

// Push adds v, dropping the oldest when full.
func (r *Ring[T]) Push(v T) {
	r.buf[r.next] = v
	r.next = (r.next + 1) % len(r.buf)
	if r.n < len(r.buf) {
		r.n++
	}
}

// Items returns the items newest first (a copy).
func (r *Ring[T]) Items() []T {
	out := make([]T, r.n)
	for i := 0; i < r.n; i++ {
		out[i] = r.buf[(r.next-1-i+2*len(r.buf))%len(r.buf)]
	}
	return out
}

// idSet is a bounded set of IDs: once full, the oldest ID is forgotten.
type idSet struct {
	order []string
	next  int
	set   map[string]struct{}
}

func newIDSet(n int) *idSet {
	return &idSet{order: make([]string, 0, n), set: make(map[string]struct{}, n)}
}

func (s *idSet) Has(id string) bool {
	_, ok := s.set[id]
	return ok
}

// Add inserts id; it reports false if id was already present.
func (s *idSet) Add(id string) bool {
	if s.Has(id) {
		return false
	}
	if len(s.order) < cap(s.order) {
		s.order = append(s.order, id)
	} else {
		delete(s.set, s.order[s.next])
		s.order[s.next] = id
		s.next = (s.next + 1) % len(s.order)
	}
	s.set[id] = struct{}{}
	return true
}
