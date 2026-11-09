package workspace

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/ui"
)

// pumpEvery is how often the documents (disk polling, autosave, the journal)
// and the share engine are driven while there is something to drive.
const pumpEvery = 33 * time.Millisecond

// pumpMsg is one beat of instance id's pump.
type pumpMsg struct{ id string }

// needsPump: documents are open or a share engine runs.
func (s *Surface) needsPump() bool {
	return !s.closed && s.docs != nil && (len(s.code.docs) > 0 || s.ownerActive())
}

// armPump schedules the next beat (once at a time, only while needed).
func (s *Surface) armPump(ctx *ui.Context) tea.Cmd {
	if s.pumping || !s.needsPump() {
		return nil
	}
	s.pumping = true
	return ui.After(ctx, pumpEvery, pumpMsg{id: s.id})
}

// onPump runs one beat: the share engine's events when sharing (it ticks the
// documents itself), else the documents' own Tick.
func (s *Surface) onPump(ctx *ui.Context) tea.Cmd {
	s.pumping = false
	if s.closed || s.docs == nil {
		return nil
	}
	var cmd tea.Cmd
	if s.ownerActive() {
		cmd = s.ownerPump(ctx)
	} else {
		cmd = s.docEvents(ctx, s.docs.Tick(nowClock(ctx).Now()))
	}
	return tea.Batch(cmd, s.armPump(ctx))
}
