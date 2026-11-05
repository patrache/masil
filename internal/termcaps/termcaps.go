// Package termcaps detects terminal capabilities (CA 4.9).
package termcaps

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
func Detect(env []string, override string) Caps {
	// TODO(core): Task 7
	return Caps{}
}
