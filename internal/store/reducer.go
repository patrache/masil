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

// Backfiller is optionally implemented by a Reducer.
type Backfiller interface {
	// Backfill is called after a resumed `ready` (no resync): the server replayed
	// the logged events, but transient (휘발) ones sent while the socket was down
	// are lost (events.md 4.8). It returns commands that refetch only what those
	// would have brought to the open windows; nil when nothing is open.
	Backfill(s *Store, c *api.Client) tea.Cmd
}

// Backfill batches every Backfiller's commands (see Backfiller).
func (s *Store) Backfill(c *api.Client) tea.Cmd {
	var cmds []tea.Cmd
	for _, r := range s.reducerOrder {
		if b, ok := r.(Backfiller); ok {
			cmds = append(cmds, b.Backfill(s, c))
		}
	}
	return tea.Batch(cmds...)
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
