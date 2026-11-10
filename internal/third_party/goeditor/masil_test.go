package goeditor

import "testing"

// masil's patch: with WithDarkBackground, New never asks the terminal for its
// background (on Windows that query opens the console, puts it in raw mode
// and reads it for up to 2s while Bubble Tea is reading it too).
func TestNewWithDarkBackgroundSkipsQuery(t *testing.T) {
	asked := 0
	saved := hasDarkBackground
	hasDarkBackground = func() bool { asked++; return true }
	t.Cleanup(func() { hasDarkBackground = saved })
	for _, dark := range []bool{true, false} {
		m := New(80, 20, WithDarkBackground(dark))
		if want := DefaultTheme(dark); m.theme.StatusLineStyle.GetForeground() != want.StatusLineStyle.GetForeground() {
			t.Fatalf("dark=%v: theme does not follow the option", dark)
		}
	}
	if asked != 0 {
		t.Fatalf("the terminal was asked %d times", asked)
	}
	New(80, 20) // without the option: upstream's query
	if asked != 1 {
		t.Fatalf("asked = %d without the option", asked)
	}
}
