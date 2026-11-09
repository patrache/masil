package keymap

import (
	"testing"

	"github.com/patrache/masil/internal/i18n"
)

// New rows are written as keyed literals with the contract's field name Desc
// (CA 5.5 / P0 Task 16: Desc: func(t *i18n.T) string { ... }).
func TestSpecKeyedDescField(t *testing.T) {
	s := spec{keys: []string{"ctrl+\\"}, Desc: func(t *i18n.T) string { return t.Testonly.EnOnly() }}
	if got, want := s.descIn(i18n.For(i18n.En)), "Only in English"; got != want {
		t.Fatalf("descIn(en) = %q, want %q", got, want)
	}
	if got, want := s.descIn(nil), i18n.KoT().Testonly.EnOnly(); got != want {
		t.Fatalf("descIn(nil) = %q, want ko %q", got, want)
	}
}
