package i18n

import (
	"strings"

	"github.com/patrache/masil/internal/textwidth"
)

// pseudoVowels widens some Latin vowels to full width (2 cells).
var pseudoVowels = strings.NewReplacer("a", "ａ", "e", "ｅ", "o", "ｏ", "A", "Ａ", "E", "Ｅ", "O", "Ｏ")

// pseudo renders the x-pseudo form of template s: the template text (not the
// arguments — they are user data) gets full-width vowels, the whole is padded
// with "~" to at least 140% of the plain width and wrapped in ⟦…⟧. A string
// on screen without ⟦⟧ under MASIL_LANG=x-pseudo did not come from the catalog.
func pseudo(s string, args []string) string {
	plain := fill(s, args)
	var b strings.Builder
	last := 0
	for _, m := range placeholderRe.FindAllStringIndex(s, -1) {
		b.WriteString(pseudoVowels.Replace(s[last:m[0]]))
		b.WriteString(fill(s[m[0]:m[1]], args))
		last = m[1]
	}
	b.WriteString(pseudoVowels.Replace(s[last:]))
	target := (textwidth.Width(plain)*14 + 9) / 10
	for w := textwidth.Width(b.String()); w < target; w++ {
		b.WriteByte('~')
	}
	return "⟦" + b.String() + "⟧"
}
