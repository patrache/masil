// Package layout picks the layout mode for a terminal size and computes pane geometry (CA 5.2).
package layout

import (
	"strings"

	"github.com/patrache/masil/internal/config"
)

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
	switch m {
	case ModeCommand:
		return "지휘소"
	case ModeStandard:
		return "표준"
	case ModeCompact:
		return "컴팩트"
	case ModeWidget:
		return "위젯"
	case ModeTiny:
		return "초소형"
	default:
		return "—"
	}
}

// modeIDs maps every valid mode to its id.
var modeIDs = map[Mode]string{
	ModeTooSmall: "too_small",
	ModeTiny:     "tiny",
	ModeWidget:   "widget",
	ModeCompact:  "compact",
	ModeStandard: "standard",
	ModeCommand:  "command",
}

// ID returns "command","standard","compact","widget","tiny","too_small".
func (m Mode) ID() string {
	return modeIDs[m] // "" for an out-of-range mode
}

// ParseMode parses the ids above; "auto" => (0,false).
func ParseMode(s string) (Mode, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	for m, id := range modeIDs {
		if id == s {
			return m, true
		}
	}
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
	return Breakpoints{
		CommandMinCols: 200, CommandMinRows: 50,
		StandardMinCols: 100, StandardMinRows: 40,
		TinyMaxCols: 43, TinyMaxRows: 15,
		WidgetMinRows: 24,
		MinCols:       20, MinRows: 6,
	}
}

// FromConfig builds breakpoints from [layout]. Non-positive values use the defaults;
// MinCols/MinRows are always the fixed defaults.
func FromConfig(c config.LayoutConfig) Breakpoints {
	bp := DefaultBreakpoints()
	set := func(dst *int, v int) {
		if v > 0 {
			*dst = v
		}
	}
	set(&bp.CommandMinCols, c.CommandMinCols)
	set(&bp.CommandMinRows, c.CommandMinRows)
	set(&bp.StandardMinCols, c.StandardMinCols)
	set(&bp.StandardMinRows, c.StandardMinRows)
	set(&bp.TinyMaxCols, c.TinyMaxCols)
	set(&bp.TinyMaxRows, c.TinyMaxRows)
	set(&bp.WidgetMinRows, c.WidgetMinRows)
	return bp
}

// Auto selects a mode from terminal size (CA 5.2.1 rules, in order).
//
// With the default breakpoints every rule result already satisfies PinMinSize; as a safety
// net for odd custom breakpoints, a result below its mode's minimum size steps down to the
// next smaller mode that fits (so Compute never gets a mode it cannot lay out).
func Auto(cols, rows int, bp Breakpoints) Mode {
	cols, rows = max(cols, 0), max(rows, 0)
	minCols, minRows := max(bp.MinCols, 20), max(bp.MinRows, 6)
	var m Mode
	switch {
	case cols < minCols || rows < minRows:
		return ModeTooSmall
	case cols <= bp.TinyMaxCols || rows <= bp.TinyMaxRows:
		m = ModeTiny
	case cols < bp.StandardMinCols:
		if rows < bp.WidgetMinRows {
			m = ModeTiny
		} else {
			m = ModeWidget
		}
	case rows < bp.StandardMinRows:
		m = ModeCompact
	case cols >= bp.CommandMinCols && rows >= bp.CommandMinRows:
		m = ModeCommand
	default:
		m = ModeStandard
	}
	for m > ModeTooSmall {
		pc, pr := PinMinSize(m)
		if cols >= pc && rows >= pr {
			break
		}
		m--
	}
	return m
}

// Resolve applies an optional pin, falling back to Auto when the window is below the pin's minimum size.
// A nil, too_small or out-of-range pin means no pin.
func Resolve(cols, rows int, bp Breakpoints, pin *Mode) (mode Mode, pinFellBack bool) {
	auto := Auto(cols, rows, bp)
	if pin == nil || *pin <= ModeTooSmall || *pin > ModeCommand {
		return auto, false
	}
	pc, pr := PinMinSize(*pin)
	if cols < pc || rows < pr {
		return auto, true
	}
	return *pin, false
}

// PinMinSize returns the fixed minimum size for pinning m.
func PinMinSize(m Mode) (cols, rows int) {
	switch m {
	case ModeCommand:
		return 160, 40
	case ModeStandard:
		return 80, 24
	case ModeCompact:
		return 60, 16
	case ModeWidget:
		return 44, 16
	case ModeTiny:
		return 20, 6
	default:
		return 0, 0
	}
}
