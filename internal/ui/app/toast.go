package app

import (
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/widgets"
)

// Toast decisions (plan 03 Task 26, CA 5.3.4).
const (
	toastTTL  = 4 * time.Second // each toast expires through ui.After
	toastMax  = 3               // newest toasts kept (older ones are dropped)
	toastMaxW = 44              // box width cap (cells, border included)
	toastMinW = 24              // narrower body => the status line shows the newest toast
	toastZ    = 100             // compositor layer above panes (0) and overlays (10+i)
)

// toastExpireMsg removes the toast with this id (armed for toastTTL when it is shown).
type toastExpireMsg struct{ id int }

// toastItem is one queued toast.
type toastItem struct {
	id  int
	msg ui.ToastMsg
	// alert is set on an alert toast whose target core does not know: opening it
	// asks ui.AlertTargetCmd first (Model.openToastTarget).
	alert *api.Alert
}

// toastHit maps screen rows [y0, y1) of the toast box to the toast drawn there.
type toastHit struct {
	y0, y1 int
	id     int
}

// toastState is the toast queue, oldest first.
type toastState struct {
	items []toastItem
	next  int // last issued id
}

// push queues t (keeping the newest toastMax) and returns its expiry timer.
func (s *toastState) push(ctx *ui.Context, t ui.ToastMsg) tea.Cmd {
	s.next++
	s.items = append(s.items, toastItem{id: s.next, msg: t})
	if n := len(s.items) - toastMax; n > 0 {
		s.items = slices.Delete(s.items, 0, n)
	}
	return ui.After(ctx, toastTTL, toastExpireMsg{id: s.next})
}

// pushAlert is push for an alert toast whose target opens through ui.AlertTargetCmd.
func (s *toastState) pushAlert(ctx *ui.Context, t ui.ToastMsg, a api.Alert) tea.Cmd {
	cmd := s.push(ctx, t)
	s.items[len(s.items)-1].alert = &a
	return cmd
}

// remove drops the toast with id (no-op when it is already gone).
func (s *toastState) remove(id int) {
	s.items = slices.DeleteFunc(s.items, func(it toastItem) bool { return it.id == id })
}

// newestFirst returns the queued toasts, newest first.
func (s *toastState) newestFirst() []toastItem {
	out := slices.Clone(s.items)
	slices.Reverse(out)
	return out
}

// errorToast turns an ErrorMsg into its toast text: "<Context>: <Describe>" (Describe
// alone when Context is empty).
func errorToast(e ui.ErrorMsg) ui.ToastMsg {
	text := api.Describe(e.Err)
	if text == "" {
		text = api.Describe(errUnknown)
	}
	if e.Context != "" {
		text = e.Context + ": " + text
	}
	return ui.ToastMsg{Text: text, Kind: ui.ToastError}
}

// errUnknown stands in for an ErrorMsg without an error ("알 수 없는 오류예요").
var errUnknown = &api.Error{}

// body is the screen minus the status line.
func (m *Model) body() layout.Rect {
	return layout.Rect{X: 0, Y: 0, W: m.cols, H: max(0, m.rows-1)}
}

// toastInStatus reports whether the newest toast goes to the status line instead of
// a box: tiny mode, or a body too small for a bordered box.
func (m *Model) toastInStatus() bool {
	b := m.body()
	return m.lay.Mode == layout.ModeTiny || b.W < toastMinW || b.H < 3
}

// statusToast is the newest toast's "<icon> <text>" for the status line, or "".
func (m *Model) statusToast() string {
	if len(m.toasts.items) == 0 || !m.toastInStatus() {
		return ""
	}
	t := m.toasts.items[len(m.toasts.items)-1].msg
	return toastSymbol(t.Kind) + " " + t.Text
}

// toastSymbol matches widgets.Toasts' icons (status-line form).
func toastSymbol(k ui.ToastKind) string {
	switch k {
	case ui.ToastSuccess:
		return "●"
	case ui.ToastError:
		return "✕"
	case ui.ToastAlert:
		return "★"
	}
	return "○"
}

// toastBox is the toast box geometry for the current state: its rect, inner lines
// (newest first, at most toastMax toasts, cut to the body height) and hit rows. ok is
// false when no box is drawn (no toasts, TooSmall, or the status-line fallback).
func (m *Model) toastBox() (r layout.Rect, lines []string, hits []toastHit, ok bool) {
	if !m.sized || len(m.toasts.items) == 0 || m.lay.Mode == layout.ModeTooSmall || m.toastInStatus() {
		return layout.Rect{}, nil, nil, false
	}
	b := m.body()
	w := min(toastMaxW, b.W)
	maxLines := b.H - 2
	for _, it := range m.toasts.newestFirst() {
		if len(lines) >= maxLines {
			break
		}
		ls := widgets.Toasts(m.ctx.Theme, []ui.ToastMsg{it.msg}, w-2)
		ls = ls[:min(len(ls), maxLines-len(lines))]
		y0 := b.Y + 1 + len(lines)
		hits = append(hits, toastHit{y0: y0, y1: y0 + len(ls), id: it.id})
		lines = append(lines, ls...)
	}
	return layout.Rect{X: b.X + b.W - w, Y: b.Y, W: w, H: len(lines) + 2}, lines, hits, true
}

// toastLayer draws the toast box (bordered with the theme Border + OverlayBorder) at
// the body's top-right, records it in m.frame.Toast, and returns its layer; nil when
// no box is drawn.
func (m *Model) toastLayer() *lipgloss.Layer {
	r, lines, _, ok := m.toastBox()
	if !ok {
		return nil
	}
	th := m.ctx.Theme
	box := widgets.Box(th, th.S.OverlayBorder, ui.Size{W: r.W, H: r.H}, strings.Join(lines, "\n"))
	m.frame.Toast = &Box{Rect: r, Bordered: true}
	return lipgloss.NewLayer(box).X(r.X).Y(r.Y).Z(toastZ)
}

// clickToast handles a left click inside the toast box: the toast under the pointer
// (border rows count for the nearest one) is dismissed, and a toast with a Route
// navigates there. ok is false when the click missed the box.
func (m *Model) clickToast(msg tea.MouseClickMsg) (tea.Cmd, bool) {
	r, _, hits, ok := m.toastBox()
	if !ok || msg.Button != tea.MouseLeft || !r.Contains(msg.X, msg.Y) || len(hits) == 0 {
		return nil, false
	}
	hit := hits[0]
	for _, h := range hits {
		if msg.Y >= h.y0 {
			hit = h
		}
	}
	var open *toastItem
	for _, it := range m.toasts.items {
		if it.id == hit.id && it.msg.Route != nil {
			open = &it
		}
	}
	m.toasts.remove(hit.id)
	if open == nil {
		return nil, true
	}
	if open.alert != nil {
		if cmd, ok := ui.AlertTargetCmd(m.ctx, *open.alert); ok && cmd != nil {
			return cmd, true
		}
	}
	return ui.Navigate(*open.msg.Route), true
}
