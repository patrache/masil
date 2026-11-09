package layout_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/exp/golden"

	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/uitest"
)

var slotLetter = map[layout.SlotID]byte{
	layout.SlotRail:          'r',
	layout.SlotSidebar:       's',
	layout.SlotMain:          'm',
	layout.SlotFeed:          'f',
	layout.SlotThread:        't',
	layout.SlotMembers:       'b',
	layout.SlotNotifications: 'n',
	layout.SlotTabs:          'T',
	layout.SlotContext:       'c',
	layout.SlotStatus:        'S',
	layout.SlotTool:          'o',
}

// diagram renders a layout as one letter per cell ('?' = uncovered, '!' = covered twice).
func diagram(l layout.Layout) string {
	grid := make([][]byte, l.Rows)
	for y := range grid {
		grid[y] = []byte(strings.Repeat("?", l.Cols))
	}
	for _, p := range l.Panes {
		for y := p.Rect.Y; y < p.Rect.Y+p.Rect.H; y++ {
			for x := p.Rect.X; x < p.Rect.X+p.Rect.W; x++ {
				if grid[y][x] != '?' {
					grid[y][x] = '!'
				} else {
					grid[y][x] = slotLetter[p.Slot]
				}
			}
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "mode=%s %dx%d\n", l.Mode.ID(), l.Cols, l.Rows)
	for _, p := range l.Panes {
		fmt.Fprintf(&b, "%c %-13s {%d,%d,%d,%d} bordered=%v\n", slotLetter[p.Slot], p.Slot, p.Rect.X, p.Rect.Y, p.Rect.W, p.Rect.H, p.Bordered)
	}
	for _, row := range grid {
		b.Write(row)
		b.WriteByte('\n')
	}
	return b.String()
}

func TestDiagramGolden(t *testing.T) {
	sizes := uitest.AllSizes()
	if len(sizes) != 24 {
		t.Fatalf("uitest.AllSizes() has %d sizes, want 24", len(sizes))
	}
	bp := layout.DefaultBreakpoints()
	for _, sz := range sizes {
		t.Run(fmt.Sprintf("%dx%d", sz.W, sz.H), func(t *testing.T) {
			l := layout.Compute(layout.Auto(sz.W, sz.H, bp), sz.W, sz.H, layout.State{})
			out := diagram(l)
			if strings.ContainsAny(out[strings.Index(out, "\n"):], "?!") {
				t.Fatalf("layout does not tile the screen:\n%s", out)
			}
			golden.RequireEqual(t, out)
		})
	}
	// Tool surface geometry (P0 Task 17): open and zoomed.
	for _, c := range []struct {
		name string
		w, h int
		st   layout.State
	}{
		{"tool_open_200x60", 200, 60, layout.State{ToolOpen: true}},
		{"tool_zoomed_200x60", 200, 60, layout.State{ToolOpen: true, Zoomed: true}},
		{"tool_open_120x40", 120, 40, layout.State{ToolOpen: true}},
		{"tool_zoomed_120x40", 120, 40, layout.State{ToolOpen: true, Zoomed: true}},
		{"tool_zoomed_80x24", 80, 24, layout.State{ToolOpen: true, Zoomed: true}},
	} {
		t.Run(c.name, func(t *testing.T) {
			l := layout.Compute(layout.Auto(c.w, c.h, bp), c.w, c.h, c.st)
			out := diagram(l)
			if strings.ContainsAny(out[strings.Index(out, "\n"):], "?!") {
				t.Fatalf("layout does not tile the screen:\n%s", out)
			}
			golden.RequireEqual(t, out)
		})
	}
}
