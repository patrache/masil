package apitool

import (
	"net/http"
	"net/url"
	"slices"
	"strings"
)

// Mask replaces a hidden value in cards, history and exports (C3.10 layer 4).
const Mask = "•••"

var sensitiveExact = map[string]bool{
	"authorization": true, "proxy-authorization": true, "cookie": true,
	"set-cookie": true, "x-api-key": true, "x-auth-token": true,
}

// sensitiveWords mark a header or query name whose value is hidden.
var sensitiveWords = []string{"token", "secret", "key", "auth", "session", "password"}

// SensitiveHeader reports whether the value of the header (or query
// parameter) name is always hidden.
func SensitiveHeader(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	if n == "" {
		return false
	}
	if sensitiveExact[n] {
		return true
	}
	for _, w := range sensitiveWords {
		if strings.Contains(n, w) {
			return true
		}
	}
	return false
}

// RedactHeader is a copy of h with sensitive header values replaced by Mask
// and every secret value inside the other values replaced too.
func RedactHeader(h http.Header, secretValues []string) http.Header {
	if h == nil {
		return nil
	}
	r := newRedactor(secretValues)
	out := make(http.Header, len(h))
	for k, vs := range h {
		cp := make([]string, len(vs))
		for i, v := range vs {
			if SensitiveHeader(k) {
				cp[i] = Mask
			} else {
				cp[i] = r.replace(v)
			}
		}
		out[k] = cp
	}
	return out
}

// RedactString replaces every occurrence of each non-empty secret value in s
// (also its URL query and path encodings) with Mask, longest first, in one
// pass so overlapping values are fully hidden.
func RedactString(s string, secretValues []string) string {
	return newRedactor(secretValues).replace(s)
}

type redactor struct{ r *strings.Replacer }

func newRedactor(secretValues []string) redactor {
	set := map[string]bool{}
	for _, v := range secretValues {
		if v == "" {
			continue
		}
		set[v] = true
		set[url.QueryEscape(v)] = true
		set[url.PathEscape(v)] = true
	}
	if len(set) == 0 {
		return redactor{}
	}
	vals := make([]string, 0, len(set))
	for v := range set {
		vals = append(vals, v)
	}
	slices.SortFunc(vals, func(a, b string) int {
		if len(a) != len(b) {
			return len(b) - len(a)
		}
		return strings.Compare(a, b)
	})
	pairs := make([]string, 0, 2*len(vals))
	for _, v := range vals {
		pairs = append(pairs, v, Mask)
	}
	return redactor{strings.NewReplacer(pairs...)}
}

func (r redactor) replace(s string) string {
	if r.r == nil {
		return s
	}
	return r.r.Replace(s)
}
