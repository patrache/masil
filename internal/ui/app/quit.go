package app

import (
	"log/slog"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/termhost"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/widgets"
)

// Quitting (v0.1.3 A7.4, P0 Task 20): every way out — ctrl+c twice, the
// palette's 종료 — goes through requestQuit. While embedded panes run, masil
// asks first ("lazygit 1개, nvim 1개가 실행 중이에요. 함께 끌까요?", 아니오 by
// default); yes ends them (SIGHUP, SIGKILL after the grace) and quits.

// paneCloseGrace is how long the panes get after SIGHUP before SIGKILL.
const paneCloseGrace = 2 * time.Second

// The termhost seams quitting goes through (tests swap them).
var (
	runningPanes  = termhost.Running
	closeAllPanes = termhost.CloseAll
)

// quitRequestMsg asks to quit; confirm = the palette's 종료, which always asks
// (a stray enter on the empty palette must not skip the ctrl+c-twice guard).
type quitRequestMsg struct{ confirm bool }

// requestQuit quits now, or asks first when panes run (or confirm is set).
func (m *Model) requestQuit(confirm bool) tea.Cmd {
	m.quitArmed = false
	if m.quitAsk != nil && slices.Contains(m.overlays, m.quitAsk) {
		return nil // already asking
	}
	if names := runningPanes(); len(names) > 0 {
		return m.openPaneQuit(names)
	}
	if confirm {
		return ui.OpenOverlay(widgets.Confirm("종료할까요?", "masil을 닫아요.", "종료", "취소", tea.Quit))
	}
	return tea.Quit
}

// openPaneQuit asks whether to end the running panes with masil.
func (m *Model) openPaneQuit(names []string) tea.Cmd {
	t := m.ctx.T.Termpane
	d := widgets.Confirm(t.QuitTitle(), t.QuitConfirm(paneList(m, names)), t.QuitYes(), t.QuitNo(), quitWithPanes)
	if kh, ok := d.(ui.KeyHandler); ok { // 아니오 is the default button (enter keeps masil)
		kh.HandleKey(m.ctx, tea.KeyPressMsg{Code: tea.KeyRight})
	}
	m.quitAsk = d
	return ui.OpenOverlay(d)
}

// quitWithPanes ends every pane (on the Cmd goroutine: CloseAll waits) and quits.
func quitWithPanes() tea.Msg {
	slog.Info("quit: closing live panes")
	closeAllPanes(paneCloseGrace)
	return tea.QuitMsg{}
}

// paneList is "lazygit 1개, nvim 1개": each program once, in first-start
// order, with its count.
func paneList(m *Model, names []string) string {
	var order []string
	count := map[string]int{}
	for _, n := range names {
		if count[n] == 0 {
			order = append(order, n)
		}
		count[n]++
	}
	items := make([]string, len(order))
	for i, n := range order {
		items[i] = m.ctx.T.Termpane.CountItem(n, count[n])
	}
	return strings.Join(items, ", ")
}
