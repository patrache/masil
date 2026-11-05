package ui

import "github.com/patrache/masil/internal/ui/layout"

// Overlay is a component drawn above the panes (CA 5.3.7).
type Overlay interface {
	Component
	Bounds(body layout.Rect) layout.Rect // desired OUTER box (border included) inside body; centered box typically
	Modal() bool                         // true: blocks everything below
}
