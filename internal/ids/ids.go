// Package ids generates and checks ULIDs (CA 4.2).
package ids

import "github.com/oklog/ulid/v2"

// New returns a ULID, monotonic within the process, Crockford upper-case, 26 chars (oklog/ulid Make).
func New() string {
	return ulid.Make().String()
}

// Valid reports whether s is 26 chars of Crockford base32.
// Only upper-case is accepted (I, L, O, U are excluded) and the first char must be 0-7
// so the 128-bit value does not overflow.
func Valid(s string) bool {
	if len(s) != 26 || s[0] < '0' || s[0] > '7' {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !crockford(s[i]) {
			return false
		}
	}
	return true
}

func crockford(c byte) bool {
	switch {
	case c >= '0' && c <= '9':
		return true
	case c >= 'A' && c <= 'Z':
		return c != 'I' && c != 'L' && c != 'O' && c != 'U'
	}
	return false
}

// Less reports a < b lexicographically (IDs are opaque; ordering only).
func Less(a, b string) bool {
	return a < b
}
