package termpane

import "strings"

// scrollView is the pane's "보기 스크롤" state (A7.4): the program's kept lines
// (Handle.Scrollback) above its current screen, read-only. off counts the
// lines between the bottom of the view and the bottom of the screen; 0 ends
// the view.
type scrollView struct {
	on  bool
	off int
}

// wheelStep is how many lines one wheel notch scrolls.
const wheelStep = 3

// all is the scrollback followed by the current screen.
func scrollLines(h Handle) []string {
	return append(h.Scrollback(), strings.Split(h.Render(), "\n")...)
}

// by scrolls delta lines up (positive) or down within total lines shown
// height at a time; scrolling back to the bottom ends the view.
func (s *scrollView) by(delta, total, height int) {
	maxOff := max(0, total-height)
	s.off = min(max(0, s.off+delta), maxOff)
	s.on = s.off > 0
}

// start opens the view at the bottom (the next wheel or pgup moves it).
func (s *scrollView) start() { s.on, s.off = true, 0 }

// end closes the view.
func (s *scrollView) end() { s.on, s.off = false, 0 }

// window is the height lines of all the view shows.
func (s *scrollView) window(all []string, height int) []string {
	if height <= 0 {
		return nil
	}
	end := max(0, len(all)-s.off)
	start := max(0, end-height)
	return all[start:end]
}

// key handles a key while the view is open (true when it was one of its
// keys). Every other key is swallowed too: the view is read-only.
func (s *scrollView) key(k string, total, height int) {
	page := max(1, height-1)
	switch k {
	case "esc", "q":
		s.end()
	case "pgup", "ctrl+u":
		s.by(page, total, height)
		s.on = true
	case "pgdown", "ctrl+d":
		s.by(-page, total, height)
		s.on = true
	case "up", "k":
		s.by(1, total, height)
		s.on = true
	case "down", "j":
		s.by(-1, total, height)
		s.on = true
	case "home", "g":
		s.by(total, total, height)
		s.on = true
	case "end", "G":
		s.end()
	}
}
