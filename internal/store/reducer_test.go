package store

import (
	"testing"

	"github.com/patrache/masil/internal/realtime"
)

type namedReducer struct {
	name  string
	store *Store
}

func (r *namedReducer) Name() string                 { return r.name }
func (r *namedReducer) Apply(*Store, realtime.Event) {}
func (r *namedReducer) Reset()                       {}

func withFactories(t *testing.T) {
	t.Helper()
	saved := append([]func(*Store) Reducer(nil), reducerFactories...)
	reducerFactories = nil
	t.Cleanup(func() { reducerFactories = saved })
}

func TestRegisterReducerBuildsPerStore(t *testing.T) {
	withFactories(t)
	RegisterReducer(func(s *Store) Reducer { return &namedReducer{name: "a", store: s} })
	RegisterReducer(func(s *Store) Reducer { return &namedReducer{name: "b", store: s} })
	s := New()
	a, ok := s.Reducer("a").(*namedReducer)
	if !ok || a.name != "a" || a.store != s {
		t.Fatalf("Reducer(a) = %#v", s.Reducer("a"))
	}
	if b, ok := s.Reducer("b").(*namedReducer); !ok || b.store != s {
		t.Fatalf("Reducer(b) = %#v", s.Reducer("b"))
	}
	if s.Reducer("nope") != nil {
		t.Fatal("unknown reducer must be nil")
	}
	s2 := New()
	if s2.Reducer("a") == s.Reducer("a") {
		t.Fatal("each Store must get its own reducer instances")
	}
}

func TestDuplicateReducerNamePanics(t *testing.T) {
	withFactories(t)
	RegisterReducer(func(s *Store) Reducer { return &namedReducer{name: "dup"} })
	RegisterReducer(func(s *Store) Reducer { return &namedReducer{name: "dup"} })
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on duplicate reducer name")
		}
	}()
	New()
}
