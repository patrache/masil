package chat

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/widgets"
)

// awaitConfirm is a widgets.Confirm whose "yes" runs a doAction that must land
// wherever it was started from (a palette command runs from any route, where no
// conversation, pane or route that lands msgAction results may be alive). "yes"
// keeps the dialog open, showing busy instead of the body, until the
// ResultMsg[msgAction] tagged tag arrives; it lands that result (once-guarded:
// a live component may have landed it first) and closes. Input is ignored
// while it waits, so the result can never be dropped by an early esc.
type awaitConfirm struct {
	inner                      ui.Overlay
	title, body, yes, no, busy string
	tag                        string
	run                        tea.Cmd
	waiting                    bool
	size                       ui.Size
}

var (
	_ ui.Overlay      = (*awaitConfirm)(nil)
	_ ui.KeyHandler   = (*awaitConfirm)(nil)
	_ ui.MouseHandler = (*awaitConfirm)(nil)
	_ ui.Helper       = (*awaitConfirm)(nil)
)

// awaitYesMsg is the inner confirmation's "yes" (never leaves this overlay).
type awaitYesMsg struct{ owner *awaitConfirm }

// confirmAndAwait is awaitConfirm: run is the doAction tagged tag.
func confirmAndAwait(title, body, yes, no, busy, tag string, run tea.Cmd) ui.Overlay {
	d := &awaitConfirm{title: title, body: body, yes: yes, no: no, busy: busy, tag: tag, run: run}
	d.inner = widgets.Confirm(title, body, yes, no, func() tea.Msg { return awaitYesMsg{owner: d} })
	return d
}

func (d *awaitConfirm) Init(ctx *ui.Context) tea.Cmd { return d.inner.Init(ctx) }

func (d *awaitConfirm) Resize(ctx *ui.Context, size ui.Size) {
	d.size = size
	d.inner.Resize(ctx, size)
}

func (d *awaitConfirm) Bounds(body layout.Rect) layout.Rect { return d.inner.Bounds(body) }
func (d *awaitConfirm) Modal() bool                         { return true }

func (d *awaitConfirm) View(ctx *ui.Context, focused bool) string {
	return d.inner.View(ctx, focused)
}

func (d *awaitConfirm) Help(ctx *ui.Context) []key.Binding {
	if d.waiting {
		return nil
	}
	if h, ok := d.inner.(ui.Helper); ok {
		return h.Help(ctx)
	}
	return nil
}

func (d *awaitConfirm) HandleKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	if d.waiting {
		return true, nil
	}
	kh, ok := d.inner.(ui.KeyHandler)
	if !ok {
		return false, nil
	}
	handled, cmd := kh.HandleKey(ctx, msg)
	return handled, d.intercept(ctx, cmd)
}

func (d *awaitConfirm) HandleMouse(ctx *ui.Context, msg tea.MouseMsg, local ui.Point) tea.Cmd {
	if d.waiting {
		return nil
	}
	mh, ok := d.inner.(ui.MouseHandler)
	if !ok {
		return nil
	}
	return d.intercept(ctx, mh.HandleMouse(ctx, msg, local))
}

// intercept turns the inner confirmation's "yes" (close + awaitYesMsg) into
// waiting + run. The inner Cmds are closeOverlay and the awaitYesMsg func, both
// pure, so they are evaluated here; "no" / esc pass through as they are.
func (d *awaitConfirm) intercept(ctx *ui.Context, cmd tea.Cmd) tea.Cmd {
	if cmd == nil || !d.pressedYes(cmd) {
		return cmd
	}
	d.waiting = true
	d.inner = widgets.Confirm(d.title, d.busy, d.yes, d.no, nil)
	d.inner.Resize(ctx, d.size)
	return d.run
}

func (d *awaitConfirm) pressedYes(cmd tea.Cmd) bool {
	switch m := cmd().(type) {
	case awaitYesMsg:
		return m.owner == d
	case tea.BatchMsg:
		for _, c := range m {
			if c != nil && d.pressedYes(c) {
				return true
			}
		}
	}
	return false
}

func (d *awaitConfirm) Update(ctx *ui.Context, msg tea.Msg) tea.Cmd {
	m, ok := msg.(ui.ResultMsg[msgAction])
	if !ok || !d.waiting || m.Tag != d.tag {
		return nil
	}
	d.waiting = false
	return tea.Batch(closeOverlay, landAction(ctx, m))
}
