// Package termcaps detects terminal capabilities (CA 4.9).
package termcaps

import "strings"

// Graphics is the image rendering method.
type Graphics int

// Graphics methods.
const (
	GraphicsNone Graphics = iota
	GraphicsHalfBlock
	GraphicsKitty
	GraphicsITerm2
)

// Caps describes the terminal.
type Caps struct {
	Graphics      Graphics
	InTmux        bool
	FocusReports  bool // updated when FocusMsg/BlurMsg first arrives
	UnicodeCore   bool // mode 2027 reported (renderer uses grapheme width)
	TrueColor     bool
	Term, Program string // $TERM, $TERM_PROGRAM
}

// Detect inspects env; override = config.ui.images.
// env is in os.Environ form ("KEY=value"); later entries win and empty values count as unset.
func Detect(env []string, override string) Caps {
	vars := make(map[string]string, len(env))
	for _, kv := range env {
		if k, v, ok := strings.Cut(kv, "="); ok {
			vars[k] = v
		}
	}
	term, program := vars["TERM"], vars["TERM_PROGRAM"]
	ct := strings.ToLower(vars["COLORTERM"])
	c := Caps{
		InTmux:    vars["TMUX"] != "",
		TrueColor: ct == "truecolor" || ct == "24bit",
		Term:      term,
		Program:   program,
	}
	switch override {
	case "kitty":
		c.Graphics = GraphicsKitty
	case "iterm2":
		c.Graphics = GraphicsITerm2
	case "halfblock":
		c.Graphics = GraphicsHalfBlock
	case "off":
		c.Graphics = GraphicsNone
	default: // "auto" (and anything unrecognized)
		c.Graphics = detectGraphics(vars, term, program)
	}
	return c
}

func detectGraphics(vars map[string]string, term, program string) Graphics {
	switch {
	case vars["TMUX"] != "" || strings.HasPrefix(term, "screen") || strings.HasPrefix(term, "tmux"):
		return GraphicsHalfBlock
	case term == "xterm-kitty" || vars["KITTY_WINDOW_ID"] != "" || program == "ghostty":
		return GraphicsKitty
	case program == "iTerm.app" || program == "WezTerm":
		return GraphicsITerm2
	default:
		return GraphicsHalfBlock
	}
}
