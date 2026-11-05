// Package ids generates and checks ULIDs (CA 4.2).
package ids

// New returns a ULID, monotonic within the process, Crockford upper-case, 26 chars (oklog/ulid Make).
func New() string {
	// TODO(core): Task 3
	return ""
}

// Valid reports whether s is 26 chars of Crockford base32.
func Valid(s string) bool {
	// TODO(core): Task 3
	return false
}

// Less reports a < b lexicographically (IDs are opaque; ordering only).
func Less(a, b string) bool {
	// TODO(core): Task 3
	return false
}
