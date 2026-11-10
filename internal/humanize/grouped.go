package humanize

import "strconv"

// Grouped writes n with a comma every three digits ("1,840", "-1,840"). It is
// the same in every language masil ships, so it reads no catalog.
func Grouped(n int64) string {
	s := strconv.FormatInt(n, 10)
	neg := s[0] == '-'
	if neg {
		s = s[1:]
	}
	b := make([]byte, 0, len(s)+len(s)/3+1)
	if neg {
		b = append(b, '-')
	}
	for i := range len(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			b = append(b, ',')
		}
		b = append(b, s[i])
	}
	return string(b)
}
