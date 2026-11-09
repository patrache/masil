package i18n_test

import (
	"testing"

	"github.com/patrache/masil/internal/i18n"
)

func TestSelectable(t *testing.T) {
	for _, tc := range []struct {
		open []i18n.Locale
		want bool
	}{
		{[]i18n.Locale{i18n.Ko}, false},
		{[]i18n.Locale{i18n.Ko, i18n.En}, true},
		{[]i18n.Locale{i18n.Ko, i18n.Pseudo}, false}, // Pseudo is not a choice
		{[]i18n.Locale{i18n.Ko, i18n.En, i18n.ZhHans}, true},
	} {
		i18n.SetOpenForTest(t, tc.open...)
		if got := i18n.Selectable(); got != tc.want {
			t.Errorf("open %v: Selectable() = %v, want %v", tc.open, got, tc.want)
		}
	}
}

// 0.1.3 ships with ko only: the language UI stays hidden.
func TestSelectableDefault(t *testing.T) {
	if i18n.Selectable() {
		t.Fatalf("Selectable() with the shipped gate %v", i18n.OpenLocales())
	}
}
