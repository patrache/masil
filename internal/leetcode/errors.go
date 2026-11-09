package leetcode

import (
	"bytes"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"strings"
)

// Error classes of a LeetCode answer (A10.2). The UI picks its sentence by
// errors.Is; nothing here retries on its own.
var (
	// ErrRateLimited: 429, too many requests.
	ErrRateLimited = errors.New("leetcode: rate limited")
	// ErrAuthExpired: 401/403 JSON or an empty answer — the cookies are
	// missing or expired; or a judge call made while signed out.
	ErrAuthExpired = errors.New("leetcode: sign-in expired")
	// ErrBlocked: an HTML page (Cloudflare check) instead of JSON, whatever
	// the status code. The UI offers the browser instead.
	ErrBlocked = errors.New("leetcode: blocked")
	// ErrPremiumOnly: a premium problem; its body is never requested.
	ErrPremiumOnly = errors.New("leetcode: premium only")
	// ErrUnexpected: any other answer masil cannot use.
	ErrUnexpected = errors.New("leetcode: unexpected response")
)

// classify maps an HTTP answer to an error class (nil = a usable 2xx body).
func classify(resp *http.Response, body []byte) error {
	// a redirect first: Django's login redirect is text/html with an empty
	// body, and it means the sign-in expired, not a block page
	if code := resp.StatusCode; code >= 300 && code < 400 {
		if strings.Contains(resp.Header.Get("Location"), "/accounts/login") {
			return fmt.Errorf("%w: redirect to sign-in", ErrAuthExpired)
		}
		return fmt.Errorf("%w: redirect (status %d)", ErrUnexpected, code)
	}
	trimmed := bytes.TrimSpace(body)
	if isHTML(resp.Header.Get("Content-Type")) || bytes.HasPrefix(trimmed, []byte("<")) {
		return fmt.Errorf("%w: html page (status %d)", ErrBlocked, resp.StatusCode)
	}
	switch code := resp.StatusCode; {
	case code == http.StatusTooManyRequests:
		return fmt.Errorf("%w: status %d", ErrRateLimited, code)
	case code == http.StatusUnauthorized || code == http.StatusForbidden:
		return fmt.Errorf("%w: status %d", ErrAuthExpired, code)
	case code < 200 || code >= 300:
		return fmt.Errorf("%w: status %d", ErrUnexpected, code)
	case len(trimmed) == 0:
		return fmt.Errorf("%w: empty answer", ErrAuthExpired)
	}
	return nil
}

func isHTML(contentType string) bool {
	mt, _, err := mime.ParseMediaType(contentType)
	return err == nil && (mt == "text/html" || mt == "application/xhtml+xml")
}
