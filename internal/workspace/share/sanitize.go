package share

import "strings"

// Sanitize makes participant text safe to type into a terminal (spec B8.3):
// it keeps newlines and tabs, drops every other C0 control (0x00-0x1F), DEL
// and C1 control (U+0080-U+009F), and removes whole escape sequences (ESC
// with what follows: CSI up to its final byte, OSC/DCS/SOS/PM/APC up to
// their terminator, otherwise ESC with its intermediates and final byte) so
// a proposal cannot carry ESC[201~ to break out of a bracketed paste.
func Sanitize(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	rs := []rune(s)
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		switch {
		case r == '\n' || r == '\t':
			b.WriteRune(r)
		case r == 0x1b:
			i = skipEscape(rs, i)
		case r < 0x20 || r == 0x7f || r >= 0x80 && r <= 0x9f:
			// dropped
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// skipEscape returns the index of the last rune of the escape sequence
// starting at rs[i] (ESC).
func skipEscape(rs []rune, i int) int {
	if i+1 >= len(rs) {
		return i
	}
	switch rs[i+1] {
	case '[': // CSI: parameters and intermediates, then a final byte 0x40-0x7E
		j := i + 2
		for j < len(rs) && rs[j] >= 0x20 && rs[j] <= 0x3f {
			j++
		}
		for j < len(rs) && rs[j] >= 0x20 && rs[j] <= 0x2f {
			j++
		}
		if j < len(rs) && rs[j] >= 0x40 && rs[j] <= 0x7e {
			return j
		}
		return j - 1
	case ']', 'P', 'X', '^', '_': // string commands, until BEL or ST (ESC \)
		for j := i + 2; j < len(rs); j++ {
			if rs[j] == 0x07 {
				return j
			}
			if rs[j] == 0x1b && j+1 < len(rs) && rs[j+1] == '\\' {
				return j + 1
			}
		}
		return len(rs) - 1
	default: // ESC, intermediates 0x20-0x2F, one final byte
		j := i + 1
		for j < len(rs) && rs[j] >= 0x20 && rs[j] <= 0x2f {
			j++
		}
		if j < len(rs) && rs[j] >= 0x30 && rs[j] <= 0x7e {
			return j
		}
		return j - 1
	}
}
