package uitest

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// ReverseCell is a cell drawn in reverse video (SGR 7): its column and row
// in the frame and its text.
type ReverseCell struct {
	X, Y int
	Text string
}

// ReverseCells lists the cells of a styled frame (View().Content, or a
// component's own View) drawn in reverse video, in reading order — where a
// text editor draws its own cursor cell (v0.1.3-1).
func ReverseCells(styled string) []ReverseCell {
	var out []ReverseCell
	for y, line := range strings.Split(styled, "\n") {
		x, rev := 0, false
		var state byte
		for len(line) > 0 {
			seq, width, n, next := ansi.DecodeSequence(line, state, nil)
			if n <= 0 {
				break
			}
			state, line = next, line[n:]
			switch {
			case width > 0:
				if rev {
					out = append(out, ReverseCell{X: x, Y: y, Text: seq})
				}
				x += width
			case ansi.HasCsiPrefix(seq) && strings.HasSuffix(seq, "m"):
				rev = sgrReverse(seq, rev)
			}
		}
	}
	return out
}

// sgrReverse is the reverse-video attribute after the SGR sequence seq.
func sgrReverse(seq string, rev bool) bool {
	body := strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(seq, "\x1b["), "\x9b"), "m")
	if body == "" {
		return false
	}
	ps := strings.FieldsFunc(body, func(r rune) bool { return r == ';' })
	for i := 0; i < len(ps); i++ {
		p := ps[i]
		if j := strings.IndexByte(p, ':'); j >= 0 { // 38:2:r:g:b and friends
			p = p[:j]
			if p == "38" || p == "48" || p == "58" {
				continue
			}
		}
		v, err := strconv.Atoi(p)
		if err != nil {
			continue
		}
		switch v {
		case 0:
			rev = false
		case 7:
			rev = true
		case 27:
			rev = false
		case 38, 48, 58: // extended color: skip its arguments
			if i+1 < len(ps) {
				switch ps[i+1] {
				case "5":
					i += 2
				case "2":
					i += 4
				}
			}
		}
	}
	return rev
}
