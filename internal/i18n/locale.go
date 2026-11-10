package i18n

import (
	"strings"

	"golang.org/x/text/language"
)

// Locale is a supported UI language code (BCP 47).
type Locale string

const (
	Ko     Locale = "ko"
	En     Locale = "en"
	ZhHans Locale = "zh-Hans"
	// Pseudo is the developer-only pseudo locale (MASIL_LANG=x-pseudo).
	Pseudo Locale = "x-pseudo"
)

// Supported lists the real languages in display order. Pseudo is not listed.
var Supported = []Locale{Ko, En, ZhHans}

// native holds each language's own name (i18n spec 6.1).
var native = map[Locale]string{
	Ko:     "한국어",
	En:     "English",
	ZhHans: "简体中文",
	Pseudo: "x-pseudo",
}

// Native returns the language's own name ("한국어", "English", "简体中文").
// Unknown values are returned as is.
func Native(l Locale) string {
	if n, ok := native[l]; ok {
		return n
	}
	return string(l)
}

// matcher maps any BCP 47 tag to the closest supported language; index 0 (en)
// is the "no match" answer. Chinese is folded to zh-Hans before matching.
var (
	matchTags = []language.Tag{language.English, language.Korean, language.MustParse("zh-Hans")}
	matchOut  = []Locale{En, Ko, ZhHans}
	matcher   = language.NewMatcher(matchTags)
)

// Parse maps a BCP 47 tag or a POSIX locale ("en_US.UTF-8", "fr_FR@euro") to a
// supported language. "", "C" and "POSIX" (any encoding) report false. Every
// other parseable tag maps to ko, zh-Hans (all Chinese, traditional included)
// or en (everything else). "x-pseudo" maps to Pseudo.
func Parse(tag string) (Locale, bool) {
	s := strings.TrimSpace(tag)
	if strings.EqualFold(s, string(Pseudo)) {
		return Pseudo, true
	}
	if i := strings.IndexAny(s, ".@"); i >= 0 {
		s = s[:i]
	}
	if s == "" || strings.EqualFold(s, "C") || strings.EqualFold(s, "POSIX") {
		return "", false
	}
	t, err := language.Parse(strings.ReplaceAll(s, "_", "-"))
	if err != nil || t == language.Und {
		return "", false
	}
	if base, _ := t.Base(); base.String() == "zh" {
		return ZhHans, true
	}
	_, idx, _ := matcher.Match(t)
	return matchOut[idx], true
}
