package api

import (
	"encoding/json"
	"errors"
	"math"
	"sort"
	"strings"

	"github.com/patrache/masil/internal/textwidth"
)

// maxServerText caps, in cells, each string the server supplies to Explain.
const maxServerText = 40

// serverText makes a server-supplied string (error code, field name or reason,
// version) safe to show (pending-fixes 0-b): textwidth.Sanitize drops escape
// sequences and control/format characters, and the result is cut to
// maxServerText cells, so a hostile or broken server cannot write ESC/BEL/C1
// (screen clears, OSC 52 clipboard writes, titles) through an error line.
func serverText(s string) string {
	return strings.TrimRight(textwidth.Truncate(textwidth.Sanitize(s), maxServerText, "…"), " ")
}

// Explanation is an error taken apart for display (P5 Task 6): what the
// screen says is errtext.Describe(t, err), which renders it in t's language
// (the words live in the catalog's errors area).
// Every server-supplied string in it went through serverText (sanitized, at
// most maxServerText cells).
type Explanation struct {
	// Code is CodeOf(err) ("network", "canceled" for transport errors);
	// "" = not an API error (unknown).
	Code string
	// Field and Reason are invalid_request's first details.fields entry by
	// name ("" = no fields).
	Field, Reason string
	// Limit is limit_exceeded's details.limit ("" = absent).
	Limit string
	// MinVersion is upgrade_required's details.min_client_version ("" = absent).
	MinVersion string
	// RetryAfterSec is rate_limited's Retry-After, rounded up (0 = absent).
	RetryAfterSec int
}

// Explain takes err apart (nil => the zero Explanation).
func Explain(err error) Explanation {
	if err == nil {
		return Explanation{}
	}
	x := Explanation{Code: serverText(CodeOf(err))}
	var e *Error
	if !errors.As(err, &e) {
		return x
	}
	switch e.Code {
	case "invalid_request":
		if f := e.Fields(); len(f) > 0 {
			names := make([]string, 0, len(f))
			for k := range f {
				names = append(names, k)
			}
			sort.Strings(names)
			x.Field, x.Reason = serverText(names[0]), serverText(f[names[0]])
		}
	case "limit_exceeded":
		var d struct {
			Limit *json.Number `json:"limit"`
		}
		if e.details(&d) && d.Limit != nil {
			x.Limit = serverText(d.Limit.String())
		}
	case "upgrade_required":
		var d struct {
			Min string `json:"min_client_version"`
		}
		if e.details(&d) && d.Min != "" {
			x.MinVersion = serverText(d.Min)
		}
	case "rate_limited":
		if e.RetryAfter > 0 {
			x.RetryAfterSec = int(math.Ceil(e.RetryAfter.Seconds()))
		}
	}
	return x
}
