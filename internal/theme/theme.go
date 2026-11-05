// Package theme defines the four color themes (CA 5.1).
package theme

import (
	"image/color"

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

// Get returns the theme by name.
func Get(n Name) *Theme {
	// TODO(core): Task 16
	return nil
}

// All returns A, B, C, D in order.
func All() []*Theme {
	// TODO(core): Task 16
	return nil
}

// Parse accepts "charm|midnight|cream|amber" and "a|b|c|d" (case-insensitive).
func Parse(s string) (Name, bool) {
	// TODO(core): Task 16
	return "", false
}

// UserColor: self => Acc; others FNV-1a(userID) % 3 over [Acc2, Acc3, Acc4].
func (t *Theme) UserColor(userID string, self bool) color.Color {
	// TODO(core): Task 16
	return nil
}

// StoryBG maps accent|accent2|accent3|accent4|plain(chip).
func (t *Theme) StoryBG(slot string) color.Color {
	// TODO(core): Task 16
	return nil
}
