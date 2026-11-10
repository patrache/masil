package lsp

import "unicode/utf8"

// UTF16Col converts a rune column of line (0 = before the first rune) into
// the UTF-16 code-unit column the protocol uses: a rune above U+FFFF (most
// emoji) is two units, every other rune (Hangul included) one. A column past
// the end clamps to the line's length. Invalid UTF-8 bytes count as one rune
// and one unit each.
func UTF16Col(line string, runeCol int) int {
	units, n := 0, 0
	for _, r := range line {
		if n >= runeCol {
			break
		}
		units += utf16Len(r)
		n++
	}
	return units
}

// RuneCol converts a UTF-16 column of line into a rune column. A column that
// falls inside a surrogate pair rounds down to the rune's start; one past the
// end clamps to the rune count.
func RuneCol(line string, utf16Col int) int {
	units, n := 0, 0
	for _, r := range line {
		w := utf16Len(r)
		if units+w > utf16Col {
			break
		}
		units += w
		n++
	}
	return n
}

func utf16Len(r rune) int {
	if r >= 0x10000 && r <= utf8.MaxRune {
		return 2
	}
	return 1
}
