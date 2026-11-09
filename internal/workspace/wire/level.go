package wire

// Level is a participant's participation level. Higher includes lower.
type Level uint8

const (
	LevelNone Level = iota
	LevelViewer
	LevelPrompter
	LevelEditor
	LevelKeyboard
)

// LevelNoPeer is the MinLevel of kinds participants never send (owner or
// server only). It is never put on the wire.
const LevelNoPeer Level = 0xFF

var levelNames = [...]string{"", "viewer", "prompter", "editor", "keyboard"}

func (l Level) String() string {
	if int(l) < len(levelNames) {
		return levelNames[l]
	}
	if l == LevelNoPeer {
		return "-"
	}
	return "level?"
}

// ParseLevel maps a wire level name ("" for none) to a Level.
func ParseLevel(s string) (Level, bool) {
	for i, n := range levelNames {
		if n == s {
			return Level(i), true
		}
	}
	return LevelNone, false
}

// Allows reports whether a seat at level l may send kind k to the owner.
func (l Level) Allows(k Kind) bool {
	m := k.MinLevel()
	return m != LevelNoPeer && l <= LevelKeyboard && l >= m
}
