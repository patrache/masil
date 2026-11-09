package arch

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/archdoc"
	"github.com/patrache/masil/internal/ui"
)

// readOnlyReason says why a design cannot be edited.
type readOnlyReason int

const (
	roNone   readOnlyReason = iota
	roCopy                  // a card copy: only "save as a copy"
	roBroken                // the file could not be opened
)

// saveState is the header's save state.
type saveState int

const (
	saveSaved   saveState = iota // everything written
	savePending                  // edits not written yet ("편집 ●")
	saveUnsaved                  // a new design without a file name
	saveFailed                   // the last write failed
	saveCopy                     // a read-only copy
)

// docSink is where a canvas reads its design and sends its edits: a personal
// file (localSink), a card copy (copySink) or a room document (sharedSink,
// P3 Task 22). Every edit goes through Apply.
type docSink interface {
	Objects() archdoc.Objects
	Apply(ch []archdoc.Change) tea.Cmd
	ReadOnly() (bool, readOnlyReason)
	Status() saveState
}

// saveDelay is how long a personal design waits after the last edit before it
// is written (one write per burst of edits).
const saveDelay = time.Second

// saveDueMsg asks instance to write its design if gen is still its latest edit.
type saveDueMsg struct {
	instance string
	gen      int
}

// savedMsg reports a finished write of edit gen.
type savedMsg struct {
	instance string
	gen      int
	err      error
}

// localSink is a personal design file (~/.config/masil/arch/<name>.json):
// edits apply at once and are written 1s after the last one. name "" is a new
// design that has no file yet (nothing is written).
type localSink struct {
	instance   string
	store      *archdoc.Store
	name       string
	objs       archdoc.Objects
	timers     ui.Timers
	gen, saved int
	failed     bool
}

func (s *localSink) Objects() archdoc.Objects { return s.objs }

func (s *localSink) ReadOnly() (bool, readOnlyReason) { return false, roNone }

func (s *localSink) Status() saveState {
	switch {
	case s.name == "" || s.store == nil:
		return saveUnsaved
	case s.failed:
		return saveFailed
	case s.gen != s.saved:
		return savePending
	}
	return saveSaved
}

func (s *localSink) Apply(ch []archdoc.Change) tea.Cmd {
	if len(ch) == 0 {
		return nil
	}
	s.objs = s.objs.Apply(ch)
	s.gen++
	if s.name == "" || s.store == nil || s.timers == nil {
		return nil
	}
	return s.timers.After(saveDelay, saveDueMsg{instance: s.instance, gen: s.gen})
}

// due writes the design when gen is still the latest edit.
func (s *localSink) due(gen int) tea.Cmd {
	if gen != s.gen || s.store == nil || s.name == "" {
		return nil
	}
	d, _ := archdoc.FromObjects(s.objs, nil)
	st, name, inst := s.store, s.name, s.instance
	return func() tea.Msg {
		return savedMsg{instance: inst, gen: gen, err: st.Save(name, d, true)}
	}
}

// done records a finished write.
func (s *localSink) done(m savedMsg) {
	if m.err != nil {
		s.failed = true
		return
	}
	s.failed = false
	s.saved = max(s.saved, m.gen)
}

// copySink is a read-only design: a chat card's copy (save it as a copy to
// edit) or a file that could not be opened.
type copySink struct {
	objs   archdoc.Objects
	reason readOnlyReason
}

func (s *copySink) Objects() archdoc.Objects { return s.objs }

func (s *copySink) Apply([]archdoc.Change) tea.Cmd { return nil }

func (s *copySink) ReadOnly() (bool, readOnlyReason) {
	if s.reason == roNone {
		return true, roCopy
	}
	return true, s.reason
}

func (s *copySink) Status() saveState { return saveCopy }
