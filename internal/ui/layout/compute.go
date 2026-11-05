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
	// TODO(core): Task 18
	return false
}

// Inner returns r minus a 1-cell border on each side.
func (r Rect) Inner() Rect {
	// TODO(core): Task 18
	return Rect{}
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
func Compute(mode Mode, cols, rows int, st State) Layout {
	// TODO(core): Task 18
	return Layout{}
}

// Pane returns the pane for slot s.
func (l Layout) Pane(s SlotID) (Pane, bool) {
	// TODO(core): Task 18
	return Pane{}, false
}

// At returns the pane containing (x, y).
func (l Layout) At(x, y int) (Pane, bool) {
	// TODO(core): Task 18
	return Pane{}, false
}
