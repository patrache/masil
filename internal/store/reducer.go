package store

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/realtime"
)

// Reducer is a domain store registered at init().
type Reducer interface {
	Name() string
	Apply(s *Store, ev realtime.Event)
	Reset() // logout / user switch
}

// Resyncer is optionally implemented by a Reducer.
type Resyncer interface {
	// Resync is called after `ready` when resync is required or on first connect, AFTER core step-1 data landed.
	// It returns commands that refetch open windows (events.md 5.3 steps 2-4).
	Resync(s *Store, c *api.Client) tea.Cmd
}

// reducerFactories is filled by RegisterReducer during init().
var reducerFactories []func(s *Store) Reducer

// RegisterReducer adds a reducer factory (init()-time); the reducer may also implement Resyncer.
// Every Store built by New calls each factory once.
func RegisterReducer(factory func(s *Store) Reducer) {
	reducerFactories = append(reducerFactories, factory)
}

// Reducer returns the registered reducer with that name, or nil (e.g. chatstore.Get(s) uses this).
func (s *Store) Reducer(name string) Reducer {
	return s.reducers[name]
}

func (s *Store) buildReducers() {
	s.reducers = make(map[string]Reducer, len(reducerFactories))
	s.reducerOrder = make([]Reducer, 0, len(reducerFactories))
	for _, f := range reducerFactories {
		r := f(s)
		name := r.Name()
		if _, dup := s.reducers[name]; dup {
			panic(fmt.Sprintf("store: duplicate reducer name %q", name))
		}
		s.reducers[name] = r
		s.reducerOrder = append(s.reducerOrder, r)
	}
}
