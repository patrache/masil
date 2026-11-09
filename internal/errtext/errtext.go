// Package errtext renders errors for the screen in the shown language
// (P5 Task 6): api.Explain takes an error apart, Describe says it. Screens and
// tools call errtext.Describe(ctx.T, err) (never api.Describe).
package errtext

import (
	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/textwidth"
)

// Describe returns the one-line, user-facing description of err in t's
// language (nil t = ko); nil err => "". The result is always sanitized
// (textwidth.Sanitize): server-supplied pieces cannot carry escapes.
//
// Until P5 Task 6 moves the table to the catalog (errors.toml) every language
// gets the Korean 0.1.2 line.
func Describe(t *i18n.T, err error) string {
	if err == nil {
		return ""
	}
	return textwidth.Sanitize(api.DescribeKo(api.Explain(err)))
}
