package app

import (
	"log/slog"
	"path/filepath"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/notify"
	"github.com/patrache/masil/internal/realtime"
	"github.com/patrache/masil/internal/termhost"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
)

// Exec mode (design A6.3, A7): while termhost.Exec hands the terminal to a
// program, Update does not run. The bridge goroutine still sees every event; it
// pops OS notifications itself (notify.Decide + Notifier.Show — the shared ledger
// keeps the later UI pass from showing them twice) and counts what arrived. The
// first Update after Exclusive() turned false shows one summary toast. The Store
// is never touched off the Update goroutine.

// ExecReturnedMsg may be returned by an exec onExit (termhost.Exec); the app
// needs nothing from it — the toast follows the Exclusive() edge — so tools may
// return their own msg instead.
type ExecReturnedMsg struct{ Info termhost.ExitInfo }

// execWatch is the state the bridge goroutine shares with Update.
type execWatch struct {
	desktop  atomic.Bool // config notify.desktop, refreshed at every Update
	active   atomic.Bool // events were seen while exclusive (a toast may be due)
	messages atomic.Int64
	mentions atomic.Int64
}

// noteConfig refreshes what the bridge goroutine reads from the config.
func (w *execWatch) noteConfig(ctx *ui.Context) {
	w.desktop.Store(ctx.Config != nil && ctx.Config.Notify.Desktop)
}

// observe runs on the realtime goroutine for every event while Exclusive():
// other people's messages and mentions are counted, an alert that Decide pops
// is shown now (off the goroutine, so the socket never waits on the OS).
func (w *execWatch) observe(ev realtime.Event, me string, n *notify.Notifier, now func() time.Time) {
	switch ev.Type {
	case "message.created":
		v, err := ev.Decode()
		if err != nil {
			return
		}
		if d, ok := v.(realtime.MessageEvent); ok && d.Message.Author.ID != me {
			w.messages.Add(1)
			w.active.Store(true)
		}
	case "alert":
		a, ok := alertOf(ev)
		if !ok {
			return
		}
		if a.Kind == api.AlertMention {
			w.mentions.Add(1)
		}
		w.active.Store(true)
		in := notify.Input{
			Role:           notify.RoleTUI,
			Now:            now(),
			Replayed:       ev.Replayed,
			DesktopEnabled: w.desktop.Load(),
			// TerminalFocusKnown false: the program, not masil, is on screen.
		}
		if n != nil && notify.Decide(a, in) == notify.Popup {
			go func() { // ledger + OS notifier off the realtime goroutine
				if err := n.Show(a); err != nil {
					slog.Debug("os notification failed", "alert_id", a.ID, "kind", string(a.Kind), "err", err)
				}
			}()
		}
	}
}

// takeReturn is the first Update after an exec program gave the terminal back:
// the counts become one toast ("<program> 쓰는 동안 · 새 메시지 n · 멘션 m"),
// none when nothing was counted.
func (m *Model) takeReturn() tea.Cmd {
	w := &m.excl
	w.noteConfig(m.ctx)
	if !w.active.Load() || termhost.Exclusive() {
		return nil
	}
	w.active.Store(false)
	msgs, ments := w.messages.Swap(0), w.mentions.Swap(0)
	if msgs == 0 && ments == 0 {
		return nil
	}
	program, title := termhost.ExclusiveProgram()
	name := textwidth.Sanitize(title)
	if name == "" {
		name = textwidth.Sanitize(filepath.Base(program))
	}
	text := m.ctx.T.Termpane.ExecReturn(name, int(msgs), int(ments))
	return m.toasts.push(m.ctx, ui.ToastMsg{Text: text, Kind: ui.ToastInfo})
}
