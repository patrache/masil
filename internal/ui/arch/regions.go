package arch

import "github.com/patrache/masil/internal/ui/layout"

// viewKind is what the canvas pane shows.
type viewKind int

const (
	viewCanvas  viewKind = iota // the drawing
	viewList                    // the outline (region > VPC > AZ > node)
	viewSummary                 // the 3–4 line tiny summary
)

// drawerState is the per-instance layout state the user toggles.
type drawerState struct {
	palette, panel bool     // a drawer is pulled out (drawer layouts only)
	checks         bool     // compact/widget: the design-check line is open ("!")
	view           viewKind // the user's view choice ("v"), when viewSet
	viewSet        bool
}

// regions is the pane's geometry (all rects relative to the pane's inner area).
type regions struct {
	Header, Palette, Canvas, Panel, Metrics, Hints layout.Rect
	// PaletteDrawer and PanelDrawer: that part is a drawer (pulled out with a
	// key, zero width while closed) instead of a docked column.
	PaletteDrawer, PanelDrawer bool
	View                       viewKind
}

// Region sizes (spec C2.6). The spec's widths are terminal columns; the pane
// computeRegions lays out is the tool slot inside its border, two columns
// narrower, so the column thresholds take paneBorder off (an 80-column widget
// terminal is a 78-wide pane and shows the canvas).
const (
	paletteW   = 16
	panelW     = 28
	wideMin    = 110 // docked palette and panel from here
	paneBorder = 2
	listBelow  = 80 - paneBorder // narrower: the list view is the default (terminal 44~79)
	tinyMaxW   = 43 - paneBorder // terminal ≤ 43: the summary
	tinyMaxH   = 15
	dragMinW   = 60 - paneBorder // narrower canvases only view and pan (terminal < 60)
	metricsBig = 3
)

// computeRegions lays the pane out for its mode and inner size (spec C2.6):
// ≥110 wide = docked palette 16 · canvas · panel 28 with 3 metric rows (1 in
// compact, 2 with the check line open); the command layout's ~90-wide tool slot
// = palette drawer · canvas · panel 28; 80–109 (widget, standard without the
// wide view) = full-width canvas with both as drawers on the right; 44–79
// terminal columns = the list view by default ("v" shows the canvas); tiny
// (≤43 columns or ≤15 high) = the summary. Hints are left empty: the key hints are the status line's
// (ui.Helper). Rects never overlap: an open drawer narrows the canvas.
func computeRegions(mode layout.Mode, w, h int, st drawerState) regions {
	w, h = max(w, 0), max(h, 0)
	var r regions
	if mode == layout.ModeTiny || mode == layout.ModeTooSmall || w <= tinyMaxW || h <= tinyMaxH {
		r.View = viewSummary
		r.Canvas = layout.Rect{W: w, H: h}
		return r
	}
	r.Header = layout.Rect{W: w, H: 1}
	metricsH := metricsBig
	if mode == layout.ModeCompact || mode == layout.ModeWidget || w < listBelow || h < 24 {
		metricsH = 1
		if st.checks {
			metricsH = 2
		}
	}
	r.Metrics = layout.Rect{Y: h - metricsH, W: w, H: metricsH}
	top, bodyH := 1, h-1-metricsH
	switch {
	case w < listBelow:
		r.View = viewList
		if st.viewSet && st.view == viewCanvas {
			r.View = viewCanvas
		}
	case st.viewSet:
		r.View = st.view
	}
	left, right := 0, w
	switch {
	case w >= wideMin:
		r.Palette = layout.Rect{X: 0, Y: top, W: paletteW, H: bodyH}
		r.Panel = layout.Rect{X: w - panelW, Y: top, W: panelW, H: bodyH}
		left, right = paletteW, w-panelW
	case mode == layout.ModeCommand:
		r.PaletteDrawer = true
		r.Panel = layout.Rect{X: w - panelW, Y: top, W: panelW, H: bodyH}
		right = w - panelW
		if st.palette {
			r.Palette = layout.Rect{X: 0, Y: top, W: paletteW, H: bodyH}
			left = paletteW
		}
	default:
		r.PaletteDrawer, r.PanelDrawer = true, true
		if st.panel && right-left >= panelW+20 {
			r.Panel = layout.Rect{X: right - panelW, Y: top, W: panelW, H: bodyH}
			right -= panelW
		}
		if st.palette && right-left >= paletteW+20 {
			r.Palette = layout.Rect{X: right - paletteW, Y: top, W: paletteW, H: bodyH}
			right -= paletteW
		}
	}
	r.Canvas = layout.Rect{X: left, Y: top, W: max(right-left, 0), H: max(bodyH, 0)}
	return r
}
