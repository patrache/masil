package main

import (
	"testing"

	"github.com/patrache/masil/internal/i18n"
)

func TestTUIStartCarriesLocale(t *testing.T) {
	t.Setenv("MASIL_LANG", "")
	t.Setenv("LC_ALL", "")
	t.Setenv("LC_MESSAGES", "")
	t.Setenv("LANG", "en_US.UTF-8")
	e := newEnv(t, "https://masil.test")
	calls := fakeTUI(t)
	for _, args := range [][]string{{"tui"}, {"login"}, {"signup"}, {"join", "CODE"}} {
		if code := e.run(args...); code != 0 {
			t.Fatalf("%v exit %d: %s", args, code, e.err.String())
		}
	}
	for i, o := range *calls {
		if o.Locale == nil || o.Locale.Effective != i18n.Ko || o.Locale.Requested != i18n.En || o.Locale.Source != i18n.SourceOS {
			t.Fatalf("call %d: Options.Locale = %+v, want requested en (os), effective ko", i, o.Locale)
		}
	}
}
