package widgets

import (
	"strconv"
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui"
)

// Badge renders " 3 ", " 99+ ", "●". Dot wins over Count; Count <= 0 without Dot is "".
// Mention uses BadgeMention, else Badge (both acc2 bg); the dot is an acc2 glyph on Base.
func Badge(t *theme.Theme, b ui.Badge) string {
	switch {
	case b.Dot:
		return t.S.Base.Foreground(t.P.Acc2).Render("●")
	case b.Count <= 0:
		return ""
	}
	n := strconv.Itoa(b.Count)
	if b.Count > 99 {
		n = "99+"
	}
	st := t.S.Badge
	if b.Mention {
		st = t.S.BadgeMention
	}
	return st.Render(" " + n + " ")
}

// PresenceDot renders the presence glyph: online ●, away ◐, dnd ⊘, offline/unknown ○.
func PresenceDot(t *theme.Theme, s api.PresenceStatus) string {
	switch s {
	case api.PresenceOnline:
		return t.S.Online.Render("●")
	case api.PresenceAway:
		return t.S.Away.Render("◐")
	case api.PresenceDND:
		return t.S.DND.Render("⊘")
	default:
		return t.S.Offline.Render("○")
	}
}

// Avatar renders a text avatar: initial in user color chip (images are sns-fun).
// Exactly w cells: w == 1 is the handle's first letter upper-cased; wider avatars
// center the first grapheme of the sanitized display name (handle initial when empty).
func Avatar(t *theme.Theme, u api.UserSummary, w int) string {
	if w <= 0 {
		return ""
	}
	initial := handleInitial(u.Handle)
	if w >= 2 {
		if name := textwidth.Sanitize(u.DisplayName); name != "" {
			if g, _ := ansi.FirstGraphemeCluster(name, ansi.GraphemeWidth); g != "" {
				initial = g
			}
		}
	}
	st := t.S.Base.Background(t.P.Chip).Foreground(t.UserColor(u.ID, false)).Bold(true)
	return st.Render(textwidth.Center(initial, w))
}

// handleInitial is the first letter of the sanitized handle upper-cased ("?" if none).
func handleInitial(handle string) string {
	h := textwidth.Sanitize(handle)
	if h == "" {
		return "?"
	}
	g, _ := ansi.FirstGraphemeCluster(h, ansi.GraphemeWidth)
	if g == "" || textwidth.Width(g) != 1 {
		return "?"
	}
	return strings.Map(unicode.ToUpper, g)
}

// UserName renders a display name in the user's color (bold; self => acc).
// An empty display name falls back to the handle; the text is sanitized.
func UserName(t *theme.Theme, u api.UserSummary, self bool) string {
	name := textwidth.Sanitize(u.DisplayName)
	if name == "" {
		name = textwidth.Sanitize(u.Handle)
	}
	return t.S.Base.Foreground(t.UserColor(u.ID, self)).Bold(true).Render(name)
}
