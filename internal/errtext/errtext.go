// Package errtext renders errors for the screen in the shown language
// (P5 Task 6): api.Explain takes an error apart, Describe says it with the
// catalog's errors area. Screens and tools call errtext.Describe(ctx.T, err).
package errtext

import (
	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/textwidth"
)

// Describe returns the one-line, user-facing description of err in t's
// language (nil t = ko); nil err => "". The result is always sanitized
// (textwidth.Sanitize): server-supplied pieces cannot carry escapes.
func Describe(t *i18n.T, err error) string {
	if err == nil {
		return ""
	}
	if t == nil {
		t = i18n.KoT()
	}
	return textwidth.Sanitize(render(t, api.Explain(err)))
}

func render(t *i18n.T, x api.Explanation) string {
	e := t.Errors
	switch x.Code {
	case "":
		return e.Unknown()
	case "invalid_request":
		if x.Field == "" {
			return e.InvalidRequestGeneric()
		}
		label, ok := t.Lookup("errors", "field_"+x.Field, nil)
		if !ok {
			label = x.Field
		}
		reason, ok := t.Lookup("errors", "reason_"+x.Reason, nil)
		if !ok {
			reason = x.Reason
		}
		return e.FieldReason(label, reason)
	case "limit_exceeded":
		if x.Limit != "" {
			return e.LimitExceededMax(x.Limit)
		}
	case "upgrade_required":
		if x.MinVersion != "" {
			return e.UpgradeRequiredMin(x.MinVersion)
		}
	case "rate_limited":
		if x.RetryAfterSec > 0 {
			return e.RateLimitedAfter(x.RetryAfterSec)
		}
	}
	if s, ok := t.Lookup("errors", "code_"+x.Code, nil); ok {
		return s
	}
	return e.UnknownCode(x.Code)
}
