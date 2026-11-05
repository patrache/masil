package termcaps

import "testing"

func TestDetect(t *testing.T) {
	for _, c := range []struct {
		override string
		want     Graphics
	}{{"kitty", GraphicsKitty}, {"iterm2", GraphicsITerm2}, {"halfblock", GraphicsHalfBlock}, {"off", GraphicsNone}} {
		if got := Detect([]string{"TERM=xterm-kitty"}, c.override).Graphics; got != c.want {
			t.Errorf("override %q => %v, want %v", c.override, got, c.want)
		}
	}

	auto := []struct {
		env    []string
		want   Graphics
		inTmux bool
	}{
		{[]string{"TMUX=x", "TERM=xterm-kitty"}, GraphicsHalfBlock, true},
		{[]string{"TERM=screen-256color", "TERM_PROGRAM=iTerm.app"}, GraphicsHalfBlock, false},
		{[]string{"TERM=tmux-256color"}, GraphicsHalfBlock, false},
		{[]string{"TERM=xterm-kitty"}, GraphicsKitty, false},
		{[]string{"TERM=xterm-256color", "KITTY_WINDOW_ID=1"}, GraphicsKitty, false},
		{[]string{"TERM=xterm-ghostty", "TERM_PROGRAM=ghostty"}, GraphicsKitty, false},
		{[]string{"TERM=xterm-256color", "TERM_PROGRAM=iTerm.app"}, GraphicsITerm2, false},
		{[]string{"TERM=xterm-256color", "TERM_PROGRAM=WezTerm"}, GraphicsITerm2, false},
		{[]string{"TERM=xterm-256color", "TERM_PROGRAM=Apple_Terminal"}, GraphicsHalfBlock, false},
		{nil, GraphicsHalfBlock, false},
		{[]string{"TMUX=", "TERM=xterm-kitty"}, GraphicsKitty, false},
	}
	for _, c := range auto {
		for _, ov := range []string{"auto", ""} {
			got := Detect(c.env, ov)
			if got.Graphics != c.want || got.InTmux != c.inTmux {
				t.Errorf("Detect(%v, %q) = %+v, want graphics %v tmux %v", c.env, ov, got, c.want, c.inTmux)
			}
		}
	}

	// InTmux comes from env even when the override forces a method.
	if c := Detect([]string{"TMUX=/tmp/x"}, "kitty"); !c.InTmux || c.Graphics != GraphicsKitty {
		t.Errorf("override in tmux: %+v", c)
	}

	for _, ct := range []string{"truecolor", "24bit"} {
		if !Detect([]string{"COLORTERM=" + ct}, "auto").TrueColor {
			t.Errorf("COLORTERM=%s should be TrueColor", ct)
		}
	}
	if Detect([]string{"COLORTERM=yes"}, "auto").TrueColor {
		t.Error("COLORTERM=yes should not be TrueColor")
	}

	c := Detect([]string{"TERM=xterm-256color", "TERM_PROGRAM=WezTerm", "OTHER=a=b"}, "auto")
	if c.Term != "xterm-256color" || c.Program != "WezTerm" {
		t.Errorf("Term/Program not copied: %+v", c)
	}
	if c.FocusReports || c.UnicodeCore {
		t.Errorf("FocusReports/UnicodeCore must start false: %+v", c)
	}
}
