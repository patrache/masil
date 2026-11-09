package workspace

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/termpane"
	"github.com/patrache/masil/internal/workspace/hooks"
)

// The owner's share (Task 21–22) hooks into the solo surface here.

func (s *Surface) ownerSharing() bool                               { return false }
func (s *Surface) ownerPeople() int                                 { return 0 }
func (s *Surface) ownerActive() bool                                { return false }
func (s *Surface) ownerUpdate(*ui.Context, tea.Msg) tea.Cmd         { return nil }
func (s *Surface) ownerPump(*ui.Context) tea.Cmd                    { return nil }
func (s *Surface) ownerClose(*ui.Context) tea.Cmd                   { return nil }
func (s *Surface) ownerBadge() ui.Badge                             { return ui.Badge{} }
func (s *Surface) shareKey(*ui.Context) tea.Cmd                     { return nil }
func (s *Surface) onTermStarted(*ui.Context, *term)                 {}
func (s *Surface) onTermEnded(*ui.Context, *term)                   {}
func (s *Surface) onHook(*ui.Context, hooks.Event) tea.Cmd          { return nil }
func (s *Surface) onNotify(*ui.Context, termpane.NotifyMsg) tea.Cmd { return nil }
func (s *Surface) peopleKey(*ui.Context, tea.KeyPressMsg) (bool, tea.Cmd) {
	return false, nil
}
func (s *Surface) peopleView(*ui.Context, int, int, bool) []string { return nil }
func (s *Surface) ownerBand(*ui.Context) (string, lipgloss.Style, bool) {
	return "", lipgloss.Style{}, false
}
