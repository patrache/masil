package app

import (
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/lipgloss/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/realtime"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
)

// hintItem is one status-bar hint ("ctrl+k 이동"): Key is matched against Helper
// bindings so a component never repeats a key the default hint already shows.
type hintItem struct{ Key, Desc string }

func (h hintItem) String() string {
	if h.Desc == "" {
		return h.Key
	}
	return h.Key + " " + h.Desc
}

// modeHints are the default hints per mode (CA 5.4 table, mockup layouts.html wording).
var modeHints = map[layout.Mode][]hintItem{
	layout.ModeCommand:  {{"ctrl+k", "이동"}, {"tab", "패널 전환"}, {"t", "스레드"}, {"?", "도움말"}},
	layout.ModeStandard: {{"f", "피드"}, {"t", "스레드"}, {"ctrl+k", ""}},
	layout.ModeCompact:  {{"[ ]", "탭 이동"}, {"enter", "스레드"}},
	layout.ModeWidget:   {{"tab", "탭"}},
	layout.ModeTiny:     nil, // pill and connection dot only
}

// inputHints replace the mode hints while the focused component captures input.
var inputHints = []hintItem{{"enter", "보내기"}, {"shift+enter", "줄바꿈"}, {"esc", "나가기"}}

// maxHints caps the hint items (defaults + Helper additions, CA 5.4).
const maxHints = 5

// statusItem is one right-side item ("● 온라인"). Essential items survive a narrow
// status line; the others are dropped first.
type statusItem struct {
	Text      string
	Style     lipgloss.Style
	Essential bool
}

// statusInput is what the status line shows this frame.
type statusInput struct {
	Mode  layout.Mode
	Hints []string     // rendered hint items, in order
	Right []statusItem // right side, in order
}

// statusLine gathers the status input for this frame and renders it.
func (m *Model) statusLine(w int) string {
	in := statusInput{Mode: m.lay.Mode, Right: []statusItem{m.connectionItem()}}
	if m.lay.Mode != layout.ModeTiny {
		in.Hints = m.hints()
	}
	return renderStatus(m.ctx, w, in)
}

// hints builds the hint items: the mode defaults (or the input hints while the focused
// component captures input), then the focused component's Helper bindings whose keys
// the defaults do not already show, at most maxHints in all.
func (m *Model) hints() []string {
	base := modeHints[m.lay.Mode]
	var comp ui.Component
	if in := m.focusedInstance(); in != nil {
		comp = in.comp
	}
	if c, ok := comp.(ui.InputCapturer); ok && c.CapturesInput() {
		base = inputHints
	}
	shown := map[string]bool{}
	var out []string
	for _, h := range base {
		for _, k := range strings.Fields(h.Key) {
			shown[k] = true
		}
		shown[h.Key] = true
		out = append(out, h.String())
	}
	if hp, ok := comp.(ui.Helper); ok {
		for _, b := range hp.Help(m.ctx) {
			if len(out) >= maxHints {
				break
			}
			if !b.Enabled() || overlaps(b, shown) {
				continue
			}
			hb := b.Help()
			item := hintItem{Key: hb.Key, Desc: hb.Desc}
			if item.Key == "" {
				continue
			}
			for _, k := range b.Keys() {
				shown[k] = true
			}
			shown[item.Key] = true
			out = append(out, item.String())
		}
	}
	return out
}

// overlaps reports whether any key of b (or its help label) is already shown.
func overlaps(b key.Binding, shown map[string]bool) bool {
	if shown[b.Help().Key] {
		return true
	}
	return slices.ContainsFunc(b.Keys(), func(k string) bool { return shown[k] })
}

// connectionItem is the realtime/presence item (CA 5.4): live => my presence
// (online when unknown), connecting/syncing => "◌ 연결 중…", else offline.
func (m *Model) connectionItem() statusItem {
	s := m.ctx.Theme.S
	bar := s.StatusBar
	tint := func(st lipgloss.Style) lipgloss.Style {
		if c := st.GetForeground(); c != nil {
			return bar.Foreground(c)
		}
		return bar
	}
	switch m.ctx.RT.State().Phase {
	case realtime.PhaseLive:
		switch m.ctx.Store.Self.Status {
		case api.PresenceAway:
			return statusItem{"◐ 자리비움", tint(s.Away), true}
		case api.PresenceDND:
			return statusItem{"⊘ 방해금지", tint(s.DND), true}
		}
		return statusItem{"● 온라인", tint(s.Online), true}
	case realtime.PhaseConnecting, realtime.PhaseSyncing:
		return statusItem{"◌ 연결 중…", s.StatusHint, true}
	}
	return statusItem{"✕ 오프라인", tint(s.Offline), true}
}

// renderStatus draws the 1-row status line at exactly w cells on the StatusBar
// background: mode pill, hints, then the right items flush right. In tiny mode only the
// pill and the connection symbol are drawn. When space runs short the non-essential
// right items go first, then the hints are cut with "…".
func renderStatus(ctx *ui.Context, w int, in statusInput) string {
	if w <= 0 {
		return ""
	}
	s := ctx.Theme.S
	bar := s.StatusBar
	pill := s.Pill.Render(" " + in.Mode.Label() + " ")
	pillW := textwidth.Width(in.Mode.Label()) + 2

	right := in.Right
	if in.Mode == layout.ModeTiny { // "[초소형] ●": the connection symbol only
		var kept []statusItem
		for _, it := range right {
			if it.Essential {
				sym, _, _ := strings.Cut(it.Text, " ")
				kept = append(kept, statusItem{sym, it.Style, true})
			}
		}
		right = kept
	}
	sep := " · "
	rightW := func(items []statusItem) int {
		n := 0
		for i, it := range items {
			if i > 0 {
				n += textwidth.Width(sep)
			}
			n += textwidth.Width(textwidth.Sanitize(it.Text))
		}
		if n > 0 {
			n++ // trailing blank
		}
		return n
	}
	hint := textwidth.Sanitize(strings.Join(in.Hints, sep))
	hintW := textwidth.Width(hint)
	if hintW > 0 {
		hintW++ // leading blank after the pill
	}
	if pillW+hintW+1+rightW(right) > w { // drop non-essential right items first
		var kept []statusItem
		for _, it := range right {
			if it.Essential {
				kept = append(kept, it)
			}
		}
		right = kept
	}
	rw := rightW(right)
	room := w - pillW - rw - 1 // at least one blank between hint and right side
	if hint != "" && room < hintW {
		if room >= 2 {
			hint = textwidth.Truncate(hint, room-1, "…")
		} else {
			hint = ""
		}
	}

	var b strings.Builder
	b.WriteString(pill)
	used := pillW
	if hint != "" {
		b.WriteString(bar.Render(" "))
		b.WriteString(s.StatusHint.Render(hint))
		used += 1 + textwidth.Width(hint)
	}
	if gap := w - used - rw; gap > 0 {
		b.WriteString(bar.Render(strings.Repeat(" ", gap)))
		used += gap
	}
	for i, it := range right {
		if i > 0 {
			b.WriteString(bar.Render(sep))
		}
		b.WriteString(it.Style.Render(textwidth.Sanitize(it.Text)))
	}
	if rw > 0 {
		b.WriteString(bar.Render(" "))
	}
	line := b.String()
	if textwidth.Width(line) > w {
		line = textwidth.Truncate(line, w, "")
	}
	if pad := w - textwidth.Width(line); pad > 0 {
		line += bar.Render(strings.Repeat(" ", pad))
	}
	return line
}
