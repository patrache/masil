// Package secrets is the tool-secret store (v0.1.3 spec A9): other services'
// cookies, tokens and keys (the LeetCode cookie, API-tool secret variables, the
// 0.1.4 device key) kept only on this PC — in the OS store (macOS keychain,
// Windows Credential Manager, Linux Secret Service) or, after the user agreed,
// in a 0600 file or in this run's memory.
//
// A value is a Secret: it prints, marshals and logs as "(redacted)"; Reveal is
// the only way to read it. Values never go to the masil server, logs, errors,
// goldens, taps or child environments; internal/api must not import this
// package (cmd/masil TestImportGraph).
package secrets

import (
	"fmt"
	"log/slog"
	"unicode/utf8"
)

// redacted is what every printed, marshaled or logged Secret shows.
const redacted = "(redacted)"

// mask is the masked prefix of Masked.
const mask = "••••"

// Secret is a secret value. The zero Secret is empty. It holds the value behind
// a pointer so that even reflection-based printing of a struct that embeds it
// in an unexported field shows an address, never the value.
type Secret struct{ p *string }

// New wraps s.
func New(s string) Secret { return Secret{p: &s} }

// Reveal returns the value: the only way to read it.
func (s Secret) Reveal() string {
	if s.p == nil {
		return ""
	}
	return *s.p
}

// IsZero reports whether the value is empty.
func (s Secret) IsZero() bool { return s.p == nil || *s.p == "" }

// Masked is "••••" and the last 4 characters (runes); 4 or fewer => "••••".
func (s Secret) Masked() string {
	v := s.Reveal()
	n := utf8.RuneCountInString(v)
	if n <= 4 {
		return mask
	}
	i := len(v)
	for range 4 {
		_, size := utf8.DecodeLastRuneInString(v[:i])
		i -= size
	}
	return mask + v[i:]
}

// String is "(redacted)".
func (s Secret) String() string { return redacted }

// GoString is "(redacted)".
func (s Secret) GoString() string { return redacted }

// Format writes "(redacted)" for every verb and flag.
func (s Secret) Format(f fmt.State, _ rune) { _, _ = f.Write([]byte(redacted)) }

// MarshalJSON is the JSON string "(redacted)".
func (s Secret) MarshalJSON() ([]byte, error) { return []byte(`"` + redacted + `"`), nil }

// MarshalText is "(redacted)".
func (s Secret) MarshalText() ([]byte, error) { return []byte(redacted), nil }

// LogValue is "(redacted)".
func (s Secret) LogValue() slog.Value { return slog.StringValue(redacted) }

var (
	_ fmt.Stringer   = Secret{}
	_ fmt.GoStringer = Secret{}
	_ fmt.Formatter  = Secret{}
	_ slog.LogValuer = Secret{}
)
