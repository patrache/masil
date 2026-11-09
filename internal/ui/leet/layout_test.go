package leet

import (
	"testing"

	"github.com/patrache/masil/internal/ui/layout"
)

func TestSplitFor(t *testing.T) {
	for _, c := range []struct {
		mode   layout.Mode
		zoomed bool
		w      int
		want   split
	}{
		{layout.ModeStandard, false, 94, split{Left: 40, Right: 53}},
		{layout.ModeCompact, false, 94, split{Left: 40, Right: 53}},
		{layout.ModeStandard, true, 120, split{Left: 48, Right: 71}},
		{layout.ModeCommand, false, 90, split{Left: 38, Right: 51}},
		{layout.ModeWidget, false, 80, split{Tabbed: true}},
		{layout.ModeTiny, false, 40, split{Tiny: true}},
	} {
		if got := splitFor(c.mode, c.zoomed, c.w); got != c.want {
			t.Errorf("splitFor(%v, %v, %d) = %+v, want %+v", c.mode, c.zoomed, c.w, got, c.want)
		}
	}
}
