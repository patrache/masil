// Package theme defines the four color themes (CA 5.1).
package theme

import (
	"hash/fnv"
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
)

// Name is a theme id.
type Name string

// Theme names.
const (
	Charm    Name = "charm"    // A. Charm 보라
	Midnight Name = "midnight" // B. 미드나잇 블루
	Cream    Name = "cream"    // C. 크림 파스텔
	Amber    Name = "amber"    // D. 레트로 앰버
	Default       = Amber
)

// Palette is the 11 color tokens.
type Palette struct{ BG, FG, Dim, Line, Acc, Acc2, Acc3, Acc4, Chip, SelBG, CodeBG color.Color }

// Theme is a palette with derived styles.
type Theme struct {
	Name   Name
	Letter string // "A".."D"
	Label  string // "Charm 보라", "미드나잇 블루", "크림 파스텔", "레트로 앰버"
	Dark   bool
	P      Palette
	Border lipgloss.Border // RoundedBorder / NormalBorder / RoundedBorder / DoubleBorder
	S      Styles
}

func newTheme(n Name, letter, label string, dark bool, p Palette, border lipgloss.Border) *Theme {
	return &Theme{Name: n, Letter: letter, Label: label, Dark: dark, P: p, Border: border, S: newStyles(p, border)}
}

// themes is built once; themes are read-only after init.
var themes = []*Theme{
	newTheme(Charm, "A", "Charm 보라", true, charmPalette, lipgloss.RoundedBorder()),
	newTheme(Midnight, "B", "미드나잇 블루", true, midnightPalette, lipgloss.NormalBorder()),
	newTheme(Cream, "C", "크림 파스텔", false, creamPalette, lipgloss.RoundedBorder()),
	newTheme(Amber, "D", "레트로 앰버", true, amberPalette, lipgloss.DoubleBorder()),
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

// All returns A, B, C, D in order (a fresh slice; themes themselves are shared).
func All() []*Theme {
	out := make([]*Theme, len(themes))
	copy(out, themes)
	return out
}

// Parse accepts "charm|midnight|cream|amber" and "a|b|c|d" (case-insensitive).
func Parse(s string) (Name, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	for _, t := range themes {
		if s == string(t.Name) || s == strings.ToLower(t.Letter) {
			return t.Name, true
		}
	}
	return "", false
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
