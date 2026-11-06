package api

import "testing"

// TestCheckPathBranches pins each refusal branch of checkPath on its own
// (pending-fixes 5), so dropping one is caught even where checkMediaPath or
// escPath would also have stopped the value.
func TestCheckPathBranches(t *testing.T) {
	for _, tc := range []struct {
		url string
		bad bool
	}{
		// percent-encoded dot segments, decoded by WHATWG / RFC 3986 normalisers
		{"http://h/v1/media/%2e%2e", true},
		{"http://h/v1/media/.%2E", true},
		{"http://h/v1/media/%2E", true},
		{"http://h/v1/media/%2e.", true},
		{"http://h/v1/%2e%2e/me", true},
		{"/v1/invites/%2E%2E?x=1", true},
		// raw backslash (WHATWG and Cloudflare read it as "/")
		{`http://h/v1/media/a\b`, true},
		{`http://h/a\`, true},
		{`/v1/x\y`, true},
		// plain dot and empty segments
		{"http://h/v1/media/..", true},
		{"http://h/v1/./me", true},
		{"http://h/v1//me", true},
		{"http://h/v1/me/", true},
		{"v1/me", true},
		// look-alikes that no normaliser turns into a dot segment
		{"http://h/v1/media/...", false},
		{"http://h/v1/media/%2e%2e%2e", false},
		{"http://h/v1/media/.a", false},
		{"http://h/v1/media/%252e%252e", false},
		{"http://h/v1/media/a%5Cb", false},
		{"http://h/v1/media/U?q=%2e%2e", false},
		{"http://h", false},
	} {
		err := checkPath("GET", tc.url)
		if got := err != nil; got != tc.bad {
			t.Errorf("checkPath(%q) = %v, want refused=%v", tc.url, err, tc.bad)
		}
		if err != nil {
			if e, ok := err.(*Error); !ok || e.Status != 400 || e.Code != "invalid_request" || e.Fields()["path"] != "invalid" {
				t.Errorf("checkPath(%q) err = %#v, want 400 invalid_request path=invalid", tc.url, err)
			}
		}
	}
}
