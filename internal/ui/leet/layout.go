package leet

import "github.com/patrache/masil/internal/ui/layout"

// split is how the tool body is laid out (A10.4): side by side (problem
// Left cells | a one-cell rule | editor Right cells), sub-tabs (widget) or
// the tiny screen's few lines.
type split struct {
	Tabbed, Tiny bool
	Left, Right  int
}

// splitFor is the layout for a mode and a body w cells wide: 94 (standard,
// compact) -> 40 | 53; 120 (zoomed) -> 48 | 71; the command center's ~90 ->
// 38 | 51. zoomed only says why the body is wide — the width decides.
func splitFor(mode layout.Mode, zoomed bool, w int) split {
	_ = zoomed
	switch mode {
	case layout.ModeWidget:
		return split{Tabbed: true}
	case layout.ModeTiny, layout.ModeTooSmall:
		return split{Tiny: true}
	}
	if w < 3 {
		return split{Tabbed: true}
	}
	left := w * 38 / 90
	switch {
	case w >= 120:
		left = 48
	case w >= 94:
		left = 40
	}
	return split{Left: left, Right: w - left - 1}
}
