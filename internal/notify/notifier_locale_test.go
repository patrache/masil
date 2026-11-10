package notify

import (
	"testing"

	"github.com/patrache/masil/internal/i18n"
)

func TestNotifierSetLocale(t *testing.T) {
	n := New(nil, nil, nil)
	if got := n.Locale(); got != i18n.Ko {
		t.Fatalf("default locale = %q, want ko", got)
	}
	n.SetLocale(i18n.En)
	if got := n.Locale(); got != i18n.En {
		t.Fatalf("after SetLocale(en) = %q", got)
	}
	var nilN *Notifier
	nilN.SetLocale(i18n.En) // no panic
}
