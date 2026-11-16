// Package theme defines the color themes (CA 5.1; E Stealth since v0.1.3-2).
package theme

import (
	"hash/fnv"
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/patrache/masil/internal/i18n"
)

// Name is a theme id.
type Name string

// Theme names.
const (
	Charm    Name = "charm"    // A. Charm 보라
	Midnight Name = "midnight" // B. 미드나잇 블루
	Cream    Name = "cream"    // C. 크림 파스텔
	Amber    Name = "amber"    // D. 레트로 앰버
	Stealth  Name = "stealth"  // E. 스텔스: Claude Code 다크처럼 수수한 회색 + 주황 (v0.1.3-2)
	Default       = Amber
)

// Palette is the 11 color tokens, plus Edge: the focused-border color (nil =
// Acc; v0.1.3-2 Stealth sets a plain grey so no border is colored).
type Palette struct {
	BG, FG, Dim, Line, Acc, Acc2, Acc3, Acc4, Chip, SelBG, CodeBG color.Color
	Edge                                                          color.Color
}

// Theme is a palette with derived styles.
type Theme struct {
	Name   Name
	Letter string // "A".."E" (the name in a language: Label(t, Name))
	Dark   bool
	P      Palette
	Border lipgloss.Border // Rounded / Normal / Rounded / Double / Rounded
	S      Styles
}

func newTheme(n Name, letter string, dark bool, p Palette, border lipgloss.Border) *Theme {
	return &Theme{Name: n, Letter: letter, Dark: dark, P: p, Border: border, S: newStyles(p, border)}
}

// Label is theme n's name in t's language ("Charm 보라", "Retro amber"); an
// unknown name is itself. nil t = ko.
func Label(t *i18n.T, n Name) string {
	if t == nil {
		t = i18n.KoT()
	}
	switch n {
	case Charm:
		return t.Settings.ThemeCharm()
	case Midnight:
		return t.Settings.ThemeMidnight()
	case Cream:
		return t.Settings.ThemeCream()
	case Amber:
		return t.Settings.ThemeAmber()
	case Stealth:
		return t.Settings.ThemeStealth()
	}
	return string(n)
}

// themes is built once; themes are read-only after init.
var themes = []*Theme{
	newTheme(Charm, "A", true, charmPalette, lipgloss.RoundedBorder()),
	newTheme(Midnight, "B", true, midnightPalette, lipgloss.NormalBorder()),
	newTheme(Cream, "C", false, creamPalette, lipgloss.RoundedBorder()),
	newTheme(Amber, "D", true, amberPalette, lipgloss.DoubleBorder()),
	newTheme(Stealth, "E", true, stealthPalette, lipgloss.RoundedBorder()),
}

// Get returns the theme by name; an unknown name yields the Default theme.
func Get(n Name) *Theme {
	for _, t := range themes {
		if t.Name == n {
			return t
		}
	}
	for _, t := range themes {
		if t.Name == Default {
			return t
		}
	}
	return nil
}

// All returns A, B, C, D, E in order (a fresh slice; themes themselves are shared).
func All() []*Theme {
	out := make([]*Theme, len(themes))
	copy(out, themes)
	return out
}

// Parse accepts "charm|midnight|cream|amber|stealth" and "a|b|c|d|e" (case-insensitive).
func Parse(s string) (Name, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	for _, t := range themes {
		if s == string(t.Name) || s == strings.ToLower(t.Letter) {
			return t.Name, true
		}
	}
	return "", false
}

// FocusEdge is the focused pane / input / overlay border color: P.Edge, or Acc.
func (t *Theme) FocusEdge() color.Color {
	if t.P.Edge != nil {
		return t.P.Edge
	}
	return t.P.Acc
}

// UserColor: self => Acc; others FNV-1a(userID) % 3 over [Acc2, Acc3, Acc4].
func (t *Theme) UserColor(userID string, self bool) color.Color {
	if self {
		return t.P.Acc
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(userID))
	switch h.Sum32() % 3 {
	case 0:
		return t.P.Acc2
	case 1:
		return t.P.Acc3
	default:
		return t.P.Acc4
	}
}

// StoryBG maps accent|accent2|accent3|accent4|plain(chip); unknown slots fall back to chip.
func (t *Theme) StoryBG(slot string) color.Color {
	switch slot {
	case "accent":
		return t.P.Acc
	case "accent2":
		return t.P.Acc2
	case "accent3":
		return t.P.Acc3
	case "accent4":
		return t.P.Acc4
	default:
		return t.P.Chip
	}
}
