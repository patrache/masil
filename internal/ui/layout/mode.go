// Package layout picks the layout mode for a terminal size and computes pane geometry (CA 5.2).
package layout

import "github.com/patrache/masil/internal/config"

// Mode is a layout mode.
type Mode int

// Layout modes.
const (
	ModeTooSmall Mode = iota // below 20x6: centered "창을 조금만 키워 주세요"
	ModeTiny                 // 초소형
	ModeWidget               // 위젯
	ModeCompact              // 컴팩트
	ModeStandard             // 표준
	ModeCommand              // 지휘소
)

// Label returns "지휘소","표준","컴팩트","위젯","초소형","—".
func (m Mode) Label() string {
	// TODO(core): Task 18
	return ""
}

// ID returns "command","standard","compact","widget","tiny","too_small".
func (m Mode) ID() string {
	// TODO(core): Task 18
	return ""
}

// ParseMode parses the ids above; "auto" => (0,false).
func ParseMode(s string) (Mode, bool) {
	// TODO(core): Task 18
	return 0, false
}

// Breakpoints are the mode thresholds.
type Breakpoints struct {
	CommandMinCols, CommandMinRows   int // 200, 50
	StandardMinCols, StandardMinRows int // 100, 40
	TinyMaxCols, TinyMaxRows         int // 43, 15
	WidgetMinRows                    int // 24
	MinCols, MinRows                 int // 20, 6 (fixed, not configurable)
}

// DefaultBreakpoints returns the CA 5.2.1 defaults.
func DefaultBreakpoints() Breakpoints {
	// TODO(core): Task 18
	return Breakpoints{}
}

// FromConfig builds breakpoints from [layout].
func FromConfig(c config.LayoutConfig) Breakpoints {
	// TODO(core): Task 18
	return Breakpoints{}
}

// Auto selects a mode from terminal size (CA 5.2.1 rules, in order).
func Auto(cols, rows int, bp Breakpoints) Mode {
	// TODO(core): Task 18
	return ModeTooSmall
}

// Resolve applies an optional pin, falling back to Auto when the window is below the pin's minimum size.
func Resolve(cols, rows int, bp Breakpoints, pin *Mode) (mode Mode, pinFellBack bool) {
	// TODO(core): Task 18
	return ModeTooSmall, false
}

// PinMinSize returns the fixed minimum size for pinning m.
func PinMinSize(m Mode) (cols, rows int) {
	// TODO(core): Task 18
	return 0, 0
}
