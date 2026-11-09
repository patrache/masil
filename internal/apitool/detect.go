package apitool

import (
	"math"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

// Finding is a secret-looking span value[Start:End] (byte offsets) found by
// the detector rule Rule.
type Finding struct {
	Rule       string
	Start, End int
}

// Detector thresholds (C3.16, decided here): a named value needs 16+
// characters outside {{…}} and 3.5+ bits of Shannon entropy per character;
// a Bearer/Basic credential needs 16+ characters and no placeholder.
const (
	minSecretLen = 16
	minEntropy   = 3.5
)

type detectRule struct {
	name  string
	re    *regexp.Regexp
	group int // submatch holding the secret (0 = whole match)
}

// detectRules, most specific first: a span found by an earlier rule is not
// reported again by a later one.
var detectRules = []detectRule{
	{"private_key", regexp.MustCompile(`-----BEGIN (?:[A-Z0-9]+ )*PRIVATE KEY-----(?s:.*?-----END (?:[A-Z0-9]+ )*PRIVATE KEY-----)?`), 0},
	{"jwt", regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]{5,}`), 0},
	{"aws_access_key", regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`), 0},
	{"github_token", regexp.MustCompile(`\b(?:gh[pousr]_[A-Za-z0-9]{36}|github_pat_[A-Za-z0-9_]{22,})\b`), 0},
	{"slack_token", regexp.MustCompile(`\bxox[abprs]-[A-Za-z0-9-]{10,}`), 0},
	{"bearer", regexp.MustCompile(`(?i)\bbearer\s+([A-Za-z0-9\-._~+/]+=*)`), 1},
	{"basic", regexp.MustCompile(`(?i)\bbasic\s+([A-Za-z0-9+/]+=*)`), 1},
}

// Detect lists secret-looking spans in value; name is the header, query or
// variable name (it enables named_high_entropy). Rules: bearer, basic, jwt,
// aws_access_key, private_key, github_token, slack_token, named_high_entropy.
func Detect(name, value string) []Finding {
	var out []Finding
	overlaps := func(s, e int) bool {
		for _, f := range out {
			if s < f.End && f.Start < e {
				return true
			}
		}
		return false
	}
	for _, r := range detectRules {
		for _, m := range r.re.FindAllStringSubmatchIndex(value, -1) {
			s, e := m[2*r.group], m[2*r.group+1]
			if s < 0 || s >= e {
				continue
			}
			if (r.name == "bearer" || r.name == "basic") && !credentialLike(value, s, e) {
				continue
			}
			if !overlaps(s, e) {
				out = append(out, Finding{Rule: r.name, Start: s, End: e})
			}
		}
	}
	if namedSecret(name) {
		s, e := trimmedSpan(value)
		if s < e && !overlaps(s, e) && highEntropy(placeholder.ReplaceAllString(value[s:e], "")) {
			out = append(out, Finding{Rule: "named_high_entropy", Start: s, End: e})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Start < out[j].Start })
	return out
}

// credentialLike: long enough and not followed by a placeholder.
func credentialLike(value string, s, e int) bool {
	if e-s < minSecretLen {
		return false
	}
	return !strings.HasPrefix(value[e:], "{{")
}

func namedSecret(name string) bool {
	n := strings.ToLower(name)
	if n == "" {
		return false
	}
	for _, w := range sensitiveWords {
		if strings.Contains(n, w) {
			return true
		}
	}
	return false
}

func trimmedSpan(v string) (int, int) {
	s := len(v) - len(strings.TrimLeft(v, " \t\r\n"))
	e := len(strings.TrimRight(v, " \t\r\n"))
	if e < s {
		return 0, 0
	}
	return s, e
}

// highEntropy: 16+ characters and 3.5+ bits per character.
func highEntropy(v string) bool {
	v = strings.TrimSpace(v)
	n := utf8.RuneCountInString(v)
	if n < minSecretLen {
		return false
	}
	counts := map[rune]int{}
	for _, r := range v {
		counts[r]++
	}
	h := 0.0
	for _, c := range counts {
		p := float64(c) / float64(n)
		h -= p * math.Log2(p)
	}
	return h >= minEntropy
}
