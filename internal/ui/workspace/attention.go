package workspace

import (
	"strconv"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/termpane"
	"github.com/patrache/masil/internal/workspace/hooks"
)

// A program waiting for the user (spec B8.6): claude's hooks and a bell or
// OSC 9/777 mark its terminal "⏳ 승인 대기", tell the owner and, when the
// terminal is shared, the participants (Host.SetAttention). masil answers
// nothing back to the program.

// onHook takes a hook call of terminal n (its hook URL's last element).
func (s *Surface) onHook(ctx *ui.Context, ev hooks.Event) tea.Cmd {
	n, err := strconv.Atoi(ev.Term)
	if err != nil {
		return nil
	}
	for _, t := range s.term.terms {
		if t.n == n {
			return s.setAttention(ctx, t, ev.Kind == hooks.EventAttention)
		}
	}
	return nil
}

// onNotify takes a bell or OSC 9/777 of one of this surface's terminals.
func (s *Surface) onNotify(ctx *ui.Context, m termpane.NotifyMsg) tea.Cmd {
	if t, _ := s.term.byID(m.InstanceID); t != nil {
		return s.setAttention(ctx, t, true)
	}
	return nil
}

// setAttention marks t (and tells the share); a new mark toasts.
func (s *Surface) setAttention(ctx *ui.Context, t *term, on bool) tea.Cmd {
	if t.attention == on {
		return nil
	}
	t.attention = on
	if s.own != nil {
		if id, ok := s.own.terms[t.id]; ok {
			s.own.host.SetAttention(id, on)
		}
	}
	if on {
		return ui.Toast(ctx.T.Workspace.AttentionToast(textwidth.Sanitize(t.name)), ui.ToastAlert)
	}
	return nil
}
