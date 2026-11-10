package notify

import (
	"unicode/utf8"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/textwidth"
)

// OSNotification is one desktop notification.
type OSNotification struct {
	Title, Body string
	AlertID     string
}

// Limits in code points (api.md Alert.title / Alert.body).
const (
	maxTitle = 80
	maxBody  = 200
	ellipsis = "…"
)

// Format builds the notification from Render(a, t) (t nil = ko); title/body
// sanitized: control chars stripped, title <= 80, body <= 200 code points.
func Format(a api.Alert, t *i18n.T) OSNotification {
	title, body := Render(a, t)
	return OSNotification{
		Title:   clamp(title, maxTitle),
		Body:    clamp(body, maxBody),
		AlertID: a.ID,
	}
}

// clamp sanitizes s and limits it to max code points, ending with "…" when cut.
// Sanitizing runs first and again after the cut: it can grow the text (tab ->
// spaces) and a cut can split a grapheme, so the limit is enforced on the
// final, sanitized string.
func clamp(s string, max int) string {
	s = textwidth.Sanitize(s)
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)
	keep := max - utf8.RuneCountInString(ellipsis)
	for keep > 0 {
		head := textwidth.Sanitize(string(r[:keep]))
		if utf8.RuneCountInString(head) <= max-1 {
			return head + ellipsis
		}
		keep--
	}
	return ellipsis
}
