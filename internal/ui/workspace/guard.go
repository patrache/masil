package workspace

import (
	"sync"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/workspace/share"
)

// The kill switch (spec B6.6, JX6): while any of my shares is live, ctrl+\ x
// cuts every one of them at once from any masil screen and any size, with no
// confirm. Without a live share the x is P0's own (the quit confirm).

// shareGuard is the process's list of live owner shares.
type shareGuard struct {
	mu    sync.Mutex
	hosts map[*share.Host]struct{}
}

var guard = &shareGuard{hosts: map[*share.Host]struct{}{}}

func init() {
	ui.RegisterKillSwitch(ui.KillSwitch{Active: guard.Active, Kill: guard.Kill})
}

// add puts a started share under the guard.
func (g *shareGuard) add(h *share.Host) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.hosts[h] = struct{}{}
}

// drop takes an ended share off the guard.
func (g *shareGuard) drop(h *share.Host) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.hosts, h)
}

// live is whether a share of mine is live (not killed or ended yet).
func (g *shareGuard) live() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	for h := range g.hosts {
		if !h.Killed() {
			return true
		}
	}
	return false
}

// Active is the kill switch's state for the shell: a share is live and no
// workspace surface in front reads ctrl+\ itself. While the shell's switch
// is active it takes ctrl+\ and drops every key after it but x, which
// would cost the surface its own ctrl+\ keys (enter, 4, S…); the surface
// cuts the shares on x itself (killNow). A running terminal in it grabs the
// keys, and its pane asks the shell's switch, so then it is active again.
func (g *shareGuard) Active() bool {
	return g.live() && !frontReadsPrefix()
}

// frontReadsPrefix: the workspace surface in front takes ctrl+\ through its
// own prefix (no dialog, no picker, no grabbing terminal over it).
func frontReadsPrefix() bool {
	liveMu.Lock()
	defer liveMu.Unlock()
	for _, s := range live {
		if s.appFocus && !s.closed && s.picker == nil && s.dlg == nil && (s.root != nil || s.guest != nil) && !s.GrabsKeys() {
			return true
		}
	}
	return false
}

// killNow is ctrl+\ x read by a workspace surface: every share of mine is
// cut at once (no confirm), then any other active kill switch.
func killNow(ctx *ui.Context) tea.Cmd {
	mine := guard.Kill(ctx)
	return tea.Batch(mine, ui.KillAll(ctx))
}

// Kill cuts every live share (Host.Kill tells the server at once, in the
// background) and asks the surfaces to drop their sharing state.
func (g *shareGuard) Kill(ctx *ui.Context) tea.Cmd {
	g.mu.Lock()
	n := 0
	for h := range g.hosts {
		if !h.Killed() {
			h.Kill()
			n++
		}
	}
	g.mu.Unlock()
	if n == 0 {
		return nil
	}
	cmds := []tea.Cmd{func() tea.Msg { return sharesKilledMsg{} }}
	if ctx != nil && ctx.T != nil {
		cmds = append(cmds, ui.Toast(ctx.T.Workspace.KilledToast(), ui.ToastAlert))
	}
	return tea.Batch(cmds...)
}

// shareGuardActive is the guard's state (tests and the prefix help).
func shareGuardActive() bool { return guard.live() }

// sharesKilledMsg tells every surface the kill switch was used.
type sharesKilledMsg struct{}

// onKilled drops the sharing state of a share the kill switch cut (the
// engine's own EventEnded may come later; shareEnded runs once).
func (s *Surface) onKilled(ctx *ui.Context) tea.Cmd {
	if s.own == nil || !s.own.host.Killed() {
		return nil
	}
	return s.shareEnded(ctx)
}
