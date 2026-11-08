package layout

// SlotID names a screen region.
type SlotID string

// Slots.
const (
	SlotRail          SlotID = "rail"          // 방 레일 (chat)
	SlotSidebar       SlotID = "sidebar"       // 채널·DM 목록 (chat)
	SlotMain          SlotID = "main"          // 현재 경로(대화/피드/프로필…)
	SlotFeed          SlotID = "feed"          // 지휘소 전용 피드 열 (sns-fun)
	SlotThread        SlotID = "thread"        // 지휘소 오른쪽 위 / 표준 아래 분할
	SlotMembers       SlotID = "members"       // 지휘소 오른쪽 위(스레드 닫힘일 때) (chat)
	SlotNotifications SlotID = "notifications" // 지휘소 오른쪽 아래 (sns-fun)
	SlotTabs          SlotID = "tabs"          // 컴팩트·위젯·초소형 상단 탭 (core)
	SlotContext       SlotID = "context"       // 위젯 탭 아래 "#개발 · 노바 개발방 ▾" (core, 내용은 경로 제목)
	SlotStatus        SlotID = "status"        // 하단 상태줄 (core)
)

// Rect is a screen rectangle.
type Rect struct{ X, Y, W, H int }

// Contains reports whether (x, y) is inside r.
func (r Rect) Contains(x, y int) bool {
	return x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H
}

// Inner returns r minus a 1-cell border on each side.
func (r Rect) Inner() Rect {
	return Rect{X: r.X + 1, Y: r.Y + 1, W: max(r.W-2, 0), H: max(r.H-2, 0)}
}

// Pane is one placed slot.
type Pane struct {
	Slot     SlotID
	Rect     Rect
	Bordered bool // app draws the theme border + title; component gets Rect.Inner()
}

// State is derived from app.NavState every frame (app.NavState.LayoutState, CA 5.3.3); layout never stores it.
type State struct {
	ThreadOpen  bool // the active tab has an open thread (NavState.Tabs[Active].Thread != nil)
	FeedSwapped bool // standard: NavState.Front == FrontFeed (feed surface shown in Main)
}

// Layout is the computed geometry.
type Layout struct {
	Mode       Mode
	Cols, Rows int
	Panes      []Pane // draw order; no overlaps; union covers the whole screen exactly
}

// Compute returns the pane geometry (CA 5.2.2); never panics.
//
// Negative sizes count as 0; an empty screen has no panes. Any mode on a screen below the
// fixed 20x6 minimum is laid out as ModeTooSmall. For in-domain inputs (a mode chosen by
// Resolve) the CA formulas apply verbatim; for out-of-domain inputs (a mode far below its
// minimum size) widths are clamped to what is left, so panes still tile the screen without
// overlaps, empty panes are dropped, and a slot is bordered only if its inner rect is >= 1x1.
func Compute(mode Mode, cols, rows int, st State) Layout {
	w, h := max(cols, 0), max(rows, 0)
	l := Layout{Mode: mode, Cols: w, Rows: h}
	if mode < ModeTooSmall || mode > ModeCommand || w < 20 || h < 6 {
		l.Mode = ModeTooSmall
	}
	if w == 0 || h == 0 {
		return l
	}
	add := func(s SlotID, x, y, pw, ph int, bordered bool) {
		if pw <= 0 || ph <= 0 {
			return
		}
		l.Panes = append(l.Panes, Pane{
			Slot:     s,
			Rect:     Rect{X: x, Y: y, W: pw, H: ph},
			Bordered: bordered && pw >= 3 && ph >= 3,
		})
	}
	if l.Mode == ModeTooSmall {
		add(SlotMain, 0, 0, w, h, false)
		return l
	}
	b := h - 1 // body height; the last row is the status line in every mode
	switch l.Mode {
	case ModeCommand:
		rail := min(6, w)
		side := min(clamp(w*13/100, 24, 34), w-rail)
		right := min(clamp(w*17/100, 32, 48), w-rail-side)
		rest := w - rail - side - right
		chat := rest * 7 / 12
		feed := rest - chat
		x := 0
		add(SlotRail, x, 0, rail, b, true)
		x += rail
		add(SlotSidebar, x, 0, side, b, true)
		x += side
		add(SlotMain, x, 0, chat, b, true)
		x += chat
		add(SlotFeed, x, 0, feed, b, true)
		x += feed
		top := b * 55 / 100
		upper := SlotMembers
		if st.ThreadOpen {
			upper = SlotThread
		}
		add(upper, x, 0, right, top, true)
		add(SlotNotifications, x, top, right, b-top, true)
	case ModeStandard:
		side := min(clamp(w*22/100, 24, 32), w)
		main := w - side
		add(SlotSidebar, 0, 0, side, b, true)
		top := max(10, b*60/100)
		if st.ThreadOpen && !st.FeedSwapped && b-top >= 6 {
			add(SlotMain, side, 0, main, top, true)
			add(SlotThread, side, top, main, b-top, true)
		} else {
			add(SlotMain, side, 0, main, b, true)
		}
	case ModeCompact:
		add(SlotTabs, 0, 0, w, 1, false)
		add(SlotMain, 0, 1, w, b-1, true)
	case ModeWidget:
		add(SlotTabs, 0, 0, w, 1, false)
		add(SlotContext, 0, 1, w, 1, false)
		add(SlotMain, 0, 2, w, b-2, true)
	case ModeTiny:
		add(SlotTabs, 0, 0, w, 1, false)
		add(SlotMain, 0, 1, w, b-1, false)
	}
	add(SlotStatus, 0, b, w, 1, false)
	return l
}

func clamp(v, lo, hi int) int {
	return min(max(v, lo), hi)
}

// Pane returns the pane for slot s.
func (l Layout) Pane(s SlotID) (Pane, bool) {
	for _, p := range l.Panes {
		if p.Slot == s {
			return p, true
		}
	}
	return Pane{}, false
}

// At returns the pane containing (x, y).
func (l Layout) At(x, y int) (Pane, bool) {
	for _, p := range l.Panes {
		if p.Rect.Contains(x, y) {
			return p, true
		}
	}
	return Pane{}, false
}
