package notify

import (
	"strings"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/textwidth"
)

// alertsArea is the catalog area of the alert templates (alerts.toml).
const alertsArea = "alerts"

// Render is the alert's title and body in t's language (nil = ko): a known
// template id renders from the catalog (key = id with "." as "_" plus
// "_title", and "_body" for body "template"; string params are sanitized), and
// a body the catalog does not template is the server's (verbatim user text, or
// empty). An unknown id or no template => the server's Title / Body. Both come
// back sanitized; length limits are Format's.
func Render(a api.Alert, t *i18n.T) (title, body string) {
	title, body = textwidth.Sanitize(a.Title), textwidth.Sanitize(a.Body)
	if a.Template == nil || a.Template.ID == "" {
		return title, body
	}
	if t == nil {
		t = i18n.KoT()
	}
	base := strings.ReplaceAll(a.Template.ID, ".", "_")
	args := make(map[string]any, len(a.Template.Params))
	for k, v := range a.Template.Params {
		if s, ok := v.(string); ok {
			v = textwidth.Sanitize(s)
		}
		args[k] = v
	}
	s, ok := t.Lookup(alertsArea, base+"_title", args)
	if !ok {
		return title, body
	}
	title = textwidth.Sanitize(s)
	if s, ok := t.Lookup(alertsArea, base+"_body", args); ok {
		body = textwidth.Sanitize(s)
	}
	return title, body
}
