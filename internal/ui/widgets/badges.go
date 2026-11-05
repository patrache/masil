package widgets

import (
	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui"
)

// Badge renders " 3 ", " 99+ ", "●".
func Badge(t *theme.Theme, b ui.Badge) string {
	// TODO(core): Task 21
	return ""
}

// PresenceDot renders the presence glyph.
func PresenceDot(t *theme.Theme, s api.PresenceStatus) string {
	// TODO(core): Task 21
	return ""
}

// Avatar renders a text avatar: initial in user color chip (images are sns-fun).
func Avatar(t *theme.Theme, u api.UserSummary, w int) string {
	// TODO(core): Task 21
	return ""
}

// UserName renders a display name in the user's color.
func UserName(t *theme.Theme, u api.UserSummary, self bool) string {
	// TODO(core): Task 21
	return ""
}
