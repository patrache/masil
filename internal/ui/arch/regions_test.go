package arch

import (
	"fmt"
	"testing"

	"github.com/patrache/masil/internal/ui/layout"
)

func TestComputeRegions(t *testing.T) {
	for _, tc := range []struct {
		mode               layout.Mode
		w, h               int
		view               viewKind
		palDrawer, panDraw bool
		palW, panW, metH   int
	}{
		{layout.ModeStandard, 120, 40, viewCanvas, false, false, 16, 28, 3},
		{layout.ModeCommand, 90, 56, viewCanvas, true, false, 0, 28, 3},
		{layout.ModeCompact, 120, 30, viewCanvas, false, false, 16, 28, 1},
		{layout.ModeWidget, 80, 24, viewCanvas, true, true, 0, 0, 1},
		{layout.ModeWidget, 60, 24, viewList, true, true, 0, 0, 1},
		{layout.ModeTiny, 40, 14, viewSummary, false, false, 0, 0, 0},
		{layout.ModeStandard, 92, 37, viewCanvas, true, true, 0, 0, 3},
		{layout.ModeCommand, 166, 57, viewCanvas, false, false, 16, 28, 3},
	} {
		t.Run(fmt.Sprintf("%s_%dx%d", tc.mode.ID(), tc.w, tc.h), func(t *testing.T) {
			r := computeRegions(tc.mode, tc.w, tc.h, drawerState{})
			if r.View != tc.view || r.PaletteDrawer != tc.palDrawer || r.PanelDrawer != tc.panDraw ||
				r.Palette.W != tc.palW || r.Panel.W != tc.panW || r.Metrics.H != tc.metH {
				t.Errorf("got view %d drawers %v/%v palette %d panel %d metrics %d; want view %d drawers %v/%v palette %d panel %d metrics %d",
					r.View, r.PaletteDrawer, r.PanelDrawer, r.Palette.W, r.Panel.W, r.Metrics.H,
					tc.view, tc.palDrawer, tc.panDraw, tc.palW, tc.panW, tc.metH)
			}
			checkRects(t, r, tc.w, tc.h)
		})
	}
}

func TestComputeRegionsDrawersAndToggles(t *testing.T) {
	r := computeRegions(layout.ModeWidget, 80, 24, drawerState{palette: true})
	if r.Palette.W != 16 || r.Canvas.W != 80-16 {
		t.Errorf("open palette drawer: palette %+v canvas %+v", r.Palette, r.Canvas)
	}
	checkRects(t, r, 80, 24)
	r = computeRegions(layout.ModeWidget, 80, 24, drawerState{palette: true, panel: true})
	if r.Panel.W != 28 || r.Palette.W != 16 || r.Panel.X != 80-28 || r.Palette.X != 80-28-16 {
		t.Errorf("both drawers: palette %+v panel %+v", r.Palette, r.Panel)
	}
	checkRects(t, r, 80, 24)
	if r := computeRegions(layout.ModeCompact, 120, 30, drawerState{checks: true}); r.Metrics.H != 2 {
		t.Errorf("compact with checks open: metrics %d, want 2", r.Metrics.H)
	}
	if r := computeRegions(layout.ModeWidget, 60, 24, drawerState{view: viewCanvas, viewSet: true}); r.View != viewCanvas {
		t.Errorf("narrow v: view %d, want canvas", r.View)
	}
	if r := computeRegions(layout.ModeStandard, 120, 40, drawerState{view: viewList, viewSet: true}); r.View != viewList {
		t.Errorf("standard v: view %d, want list", r.View)
	}
	for _, s := range [][2]int{{44, 15}, {43, 30}, {100, 15}} {
		if r := computeRegions(layout.ModeWidget, s[0], s[1], drawerState{}); r.View != viewSummary {
			t.Errorf("%dx%d: view %d, want summary", s[0], s[1], r.View)
		}
	}
}

// checkRects: every rect inside the area, the shown ones not overlapping.
func checkRects(t *testing.T, r regions, w, h int) {
	t.Helper()
	all := map[string]layout.Rect{"header": r.Header, "palette": r.Palette, "canvas": r.Canvas, "panel": r.Panel, "metrics": r.Metrics, "hints": r.Hints}
	for name, a := range all {
		if a.W < 0 || a.H < 0 || a.X < 0 || a.Y < 0 || a.X+a.W > w || a.Y+a.H > h {
			t.Errorf("%s %+v outside %dx%d", name, a, w, h)
		}
	}
	for n1, a := range all {
		for n2, b := range all {
			if n1 >= n2 || a.W == 0 || a.H == 0 || b.W == 0 || b.H == 0 {
				continue
			}
			if a.X < b.X+b.W && b.X < a.X+a.W && a.Y < b.Y+b.H && b.Y < a.Y+a.H {
				t.Errorf("%s %+v overlaps %s %+v", n1, a, n2, b)
			}
		}
	}
}
