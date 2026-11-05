package palette

import "strings"

// Hangul syllable block (U+AC00..U+D7A3): 19 initials × 21 medials × 28 finals.
const (
	hangulFirst   = 0xAC00
	hangulLast    = 0xD7A3
	perInitial    = 21 * 28
	initialsCount = 19
)

// initials are the 19 initial consonants as Hangul compatibility jamo (what a
// keyboard types for "ㄱ", "ㄲ", ...), in syllable-block order.
var initials = [initialsCount]rune{
	'ㄱ', 'ㄲ', 'ㄴ', 'ㄷ', 'ㄸ', 'ㄹ', 'ㅁ', 'ㅂ', 'ㅃ', 'ㅅ',
	'ㅆ', 'ㅇ', 'ㅈ', 'ㅉ', 'ㅊ', 'ㅋ', 'ㅌ', 'ㅍ', 'ㅎ',
}

// Chosung returns the Korean initial-consonant search key: "개발" => "ㄱㅂ". Every
// precomposed Hangul syllable becomes its initial consonant; anything else is kept.
func Chosung(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r >= hangulFirst && r <= hangulLast {
			r = initials[(r-hangulFirst)/perInitial]
		}
		b.WriteRune(r)
	}
	return b.String()
}
