package fsroot

import (
	"regexp"
	"strings"
)

// Rule file names. Both are hidden from peers so the owner's folder layout
// does not leak through them.
const (
	GitIgnoreFile   = ".gitignore"
	MasilIgnoreFile = ".masilignore"
)

func isRuleFile(rel string) bool {
	base := rel
	if i := strings.LastIndexByte(rel, '/'); i >= 0 {
		base = rel[i+1:]
	}
	return base == GitIgnoreFile || base == MasilIgnoreFile
}

// Ignorer answers, for many paths at once, whether each is ignored. A path is
// ignored when it or any parent folder matches (git cannot re-include a file
// inside an excluded folder).
type Ignorer interface {
	Ignored(rels []string, dirs []bool) ([]bool, error)
}

type ignoreRule struct {
	re      *regexp.Regexp
	neg     bool
	dirOnly bool
}

// Matcher is one parsed ignore file.
type Matcher struct {
	base  string // folder of the file, slash, "" for the root
	rules []ignoreRule
}

// ParseIgnore parses gitignore syntax (#, !, trailing /, leading /, *, ?,
// **, [...], \ escapes) for a file in baseDir (slash, "" or "." = root).
func ParseIgnore(baseDir string, lines []string) *Matcher {
	if baseDir == "." {
		baseDir = ""
	}
	m := &Matcher{base: strings.Trim(baseDir, "/")}
	for _, line := range lines {
		if r, ok := parseRule(line); ok {
			m.rules = append(m.rules, r)
		}
	}
	return m
}

func parseRule(line string) (ignoreRule, bool) {
	line = strings.TrimSuffix(line, "\r")
	// Trailing spaces are dropped unless escaped with a backslash.
	for strings.HasSuffix(line, " ") && !strings.HasSuffix(line, `\ `) {
		line = line[:len(line)-1]
	}
	if line == "" || line[0] == '#' {
		return ignoreRule{}, false
	}
	var r ignoreRule
	if line[0] == '!' {
		r.neg = true
		line = line[1:]
	}
	if strings.HasSuffix(line, "/") && !strings.HasSuffix(line, `\/`) {
		r.dirOnly = true
		line = strings.TrimRight(line, "/")
	}
	if line == "" {
		return ignoreRule{}, false
	}
	anchored := strings.Contains(line, "/")
	line = strings.TrimPrefix(line, "/")
	re, err := regexp.Compile(globRegexp(line, anchored))
	if err != nil {
		return ignoreRule{}, false
	}
	r.re = re
	return r, true
}

func globRegexp(pat string, anchored bool) string {
	var b strings.Builder
	b.WriteString("^")
	if !anchored {
		b.WriteString("(?:.*/)?")
	}
	segs := strings.Split(pat, "/")
	for i, seg := range segs {
		last := i == len(segs)-1
		if seg == "**" {
			if last {
				b.WriteString(".*")
			} else {
				b.WriteString("(?:.*/)?")
			}
			continue
		}
		b.WriteString(segRegexp(seg))
		if !last {
			b.WriteString("/")
		}
	}
	b.WriteString("$")
	return b.String()
}

func segRegexp(seg string) string {
	var b strings.Builder
	rs := []rune(seg)
	for i := 0; i < len(rs); i++ {
		c := rs[i]
		switch c {
		case '\\':
			if i+1 < len(rs) {
				i++
				b.WriteString(regexp.QuoteMeta(string(rs[i])))
			}
		case '*':
			b.WriteString("[^/]*")
		case '?':
			b.WriteString("[^/]")
		case '[':
			if class, n, ok := charClass(rs[i:]); ok {
				b.WriteString(class)
				i += n - 1
			} else {
				b.WriteString(`\[`)
			}
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	return b.String()
}

// charClass converts a leading "[...]" to a regexp class and returns how many
// runes it used.
func charClass(rs []rune) (string, int, bool) {
	i := 1
	var b strings.Builder
	b.WriteString("[")
	if i < len(rs) && (rs[i] == '!' || rs[i] == '^') {
		b.WriteString("^/")
		i++
	}
	first := true
	for ; i < len(rs); i++ {
		c := rs[i]
		if c == ']' && !first {
			b.WriteString("]")
			return b.String(), i + 1, true
		}
		first = false
		if c == '\\' && i+1 < len(rs) {
			i++
			c = rs[i]
		}
		switch c {
		case '[', ']', '\\', '^':
			b.WriteRune('\\')
		}
		b.WriteRune(c)
	}
	return "", 0, false
}

// Match reports the last rule matching rel (slash, relative to the folder
// root). decided is false when rel is outside the file's folder or no rule
// matches.
func (m *Matcher) Match(rel string, dir bool) (ignored, decided bool) {
	sub := rel
	if m.base != "" {
		if !strings.HasPrefix(rel, m.base+"/") {
			return false, false
		}
		sub = rel[len(m.base)+1:]
	}
	for i := len(m.rules) - 1; i >= 0; i-- {
		r := m.rules[i]
		if r.dirOnly && !dir {
			continue
		}
		if r.re.MatchString(sub) {
			return !r.neg, true
		}
	}
	return false, false
}
