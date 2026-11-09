package archdraw

// Point is a cell in a zoom's absolute cell space (world coordinates scaled by
// the zoom; equal to world coordinates at ZoomDetail).
type Point struct{ X, Y int }

// obstacles is filled by the edge router (Task 13).
type obstacles struct{}

// drawEdges draws the routed edges (Task 13).
func (s *Scene) drawEdges(*Grid, Zoom, int, int, Overlay) {}

// edgeAt returns the edge drawn at absolute zoom cell (px, py) (Task 13).
func (s *Scene) edgeAt(Zoom, int, int) string { return "" }
