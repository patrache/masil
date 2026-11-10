package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/patrache/masil/internal/api"
)

func apiErr(code, details string) *api.Error {
	e := &api.Error{Status: 400, Code: code}
	if details != "" {
		e.Details = json.RawMessage(details)
	}
	return e
}

// hasEscape reports ESC, BEL or any other C0/DEL/C1 control, a format
// character (Cf: bidi, zero-width, BOM, soft hyphen, tags), U+2028/U+2029, or
// invalid UTF-8.
func hasEscape(s string) bool {
	if !utf8.ValidString(s) {
		return true
	}
	for _, r := range s {
		if r < 0x20 || r >= 0x7f && r <= 0x9f || unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp) {
			return true
		}
	}
	return false
}

// TestErrorStringSanitized: (*Error).Error() reaches stderr and toasts as is,
// so the server's code, message and X-Request-Id carry no escapes either.
func TestErrorStringSanitized(t *testing.T) {
	e := &api.Error{Status: 400, Code: "x\x1b[2J\u202e", Message: "m\x1b]52;c;ZXZpbA==\x07\u009b31m\u200b\u2028", RequestID: "r\x1b]0;T\x07\u2067"}
	if got := e.Error(); hasEscape(got) || strings.Contains(got, "]52;") || strings.Contains(got, "]0;") || strings.Contains(got, "[2J") {
		t.Errorf("Error() = %q: carries an escape", got)
	}
	if got := (&api.Error{Status: 404, Code: "not_found", Message: "no such user", RequestID: "abc"}).Error(); got != "api: 404 not_found: no such user (request abc)" {
		t.Errorf("plain Error() = %q", got)
	}
}

func TestExplainFields(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want api.Explanation
	}{
		{nil, api.Explanation{}},
		{errors.New("x"), api.Explanation{}},
		{apiErr("not_found", ""), api.Explanation{Code: "not_found"}},
		{apiErr("invalid_request", `{"fields":{"name":"too_long","body":"required"}}`), api.Explanation{Code: "invalid_request", Field: "body", Reason: "required"}},
		{apiErr("invalid_request", ""), api.Explanation{Code: "invalid_request"}},
		{apiErr("limit_exceeded", `{"what":"group_dm_members","limit":10}`), api.Explanation{Code: "limit_exceeded", Limit: "10"}},
		{apiErr("upgrade_required", `{"min_client_version":"0.2.0"}`), api.Explanation{Code: "upgrade_required", MinVersion: "0.2.0"}},
		{&api.Error{Status: 429, Code: "rate_limited", RetryAfter: 11200 * time.Millisecond}, api.Explanation{Code: "rate_limited", RetryAfterSec: 12}},
		{&url.Error{Op: "Get", URL: "https://h", Err: errors.New("refused")}, api.Explanation{Code: "network"}},
		{fmt.Errorf("api: %w", context.Canceled), api.Explanation{Code: "canceled"}},
	} {
		if got := api.Explain(tc.err); got != tc.want {
			t.Errorf("Explain(%v) = %+v, want %+v", tc.err, got, tc.want)
		}
	}
	// Server strings come sanitized and capped.
	x := api.Explain(apiErr("teapot\x1b[2J‮", `{"fields":{"x":"y"}}`))
	if x.Code != "teapot[2J" && hasEscape(x.Code) {
		t.Errorf("Explain code = %q: carries an escape", x.Code)
	}
	if hasEscape(x.Code) {
		t.Errorf("Explain code = %q: carries an escape", x.Code)
	}
}
