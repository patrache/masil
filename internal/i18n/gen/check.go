package main

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/patrache/masil/internal/textwidth"
)

// placeholder matches {name} and {name|A/B} (particle marker, ko only).
var placeholderRe = regexp.MustCompile(`\{([a-z][a-z0-9_]*)(\|[^{}]*)?\}`)

// d8Re catches "{name} 님" — D8 writes "{name}님" with no space.
var d8Re = regexp.MustCompile(`\}\s+님`)

// check validates the catalog. Errors stop generation; warnings (missing
// translations) are reported and allowed during development.
func check(c *Catalog) (errs []error, warns []string) {
	fail := func(k *Key, format string, args ...any) {
		errs = append(errs, fmt.Errorf("%s: %s", k.id(), fmt.Sprintf(format, args...)))
	}
	for _, a := range c.Areas {
		// Keys present in a translation but not in ko.
		for _, lang := range langs[1:] {
			f := a.files[lang]
			if f == nil {
				continue
			}
			for _, k := range sortedKeys(f.Entries) {
				if a.files[srcLang] == nil || a.files[srcLang].Entries[k] == nil {
					errs = append(errs, fmt.Errorf("%s.%s: %s key not in ko", a.Name, k, lang))
				}
			}
		}
		methods := map[string]string{}
		for _, k := range a.Keys {
			name := goName(k.Name)
			for _, m := range []string{name, name + "Short"} {
				if m == name+"Short" && k.Short == "" {
					continue
				}
				if prev, dup := methods[m]; dup {
					fail(k, "name collision: method %s also made by %s", m, prev)
				}
				methods[m] = k.id()
			}
			checkKey(k, fail)
			for _, lang := range langs[1:] {
				switch r := k.Text[lang]; {
				case r == nil:
					warns = append(warns, fmt.Sprintf("%s: %s missing", k.id(), lang))
				case k.Short != "" && r.Short == "":
					// Short calls then get this language's long form.
					warns = append(warns, fmt.Sprintf("%s: %s missing short", k.id(), lang))
				}
			}
		}
	}
	return errs, warns
}

func checkKey(k *Key, fail func(*Key, string, ...any)) {
	if strings.TrimSpace(k.Desc) == "" {
		fail(k, "missing desc (translator context)")
	}
	declared := map[string]string{}
	for _, p := range k.Params {
		switch {
		case !paramRe.MatchString(p.Name):
			fail(k, "bad param name %q", p.Name)
		case p.Type != "int" && p.Type != "string":
			fail(k, "bad param type %q for %s (want int or string)", p.Type, p.Name)
		}
		declared[p.Name] = p.Type
	}
	if k.Count != "" && declared[k.Count] != "int" {
		fail(k, "count must name an int param, got %q", k.Count)
	}
	want := sortedKeys(declared)
	for _, lang := range langs {
		r := k.Text[lang]
		if r == nil {
			continue
		}
		if r.Other == "" {
			fail(k, "missing other in %s", lang)
		}
		switch {
		case lang == "en" && k.Count != "":
			if r.One == "" {
				fail(k, "missing plural one in en")
			}
		case lang == "en" && r.One != "":
			fail(k, "one without count in en")
		case r.One != "":
			fail(k, "plural one only in en (%s has other only)", lang)
		}
		if lang != srcLang && r.Short != "" && k.Short == "" {
			fail(k, "short not in ko (%s)", lang)
		}
		used := map[string]bool{}
		for _, form := range forms(r) {
			for _, m := range placeholderRe.FindAllStringSubmatch(form, -1) {
				used[m[1]] = true
				if m[2] != "" && lang != srcLang {
					fail(k, "particle outside ko: %s in %s", m[0], lang)
				}
			}
			if d8Re.MatchString(form) {
				fail(k, "D8: write {name}님 with no space (%s: %q)", lang, form)
			}
			if r, bad := controlRune(form); bad {
				fail(k, "control character %U in %s", r, lang)
			}
			if k.Max > 0 {
				if w := textwidth.Width(fill(form, declared)); w > k.Max {
					fail(k, "%s %q is %d cells, exceeds max %d", lang, form, w, k.Max)
				}
			}
		}
		if got := sortedKeys(used); !slices.Equal(got, want) {
			fail(k, "params differ in %s: uses %v, declared %v", lang, got, want)
		}
	}
}

func forms(r *Raw) []string {
	var out []string
	for _, s := range []string{r.One, r.Other, r.Short} {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// controlRune finds C0/C1 controls, DEL, tabs, newlines and bidi controls.
func controlRune(s string) (rune, bool) {
	for _, r := range s {
		switch {
		case r < 0x20, r == 0x7f, r >= 0x80 && r <= 0x9f,
			r >= 0x202a && r <= 0x202e, r >= 0x2066 && r <= 0x2069, r == 0x200e, r == 0x200f:
			return r, true
		}
	}
	return 0, false
}

// fill renders a form for width budgeting: int args "999", string args "",
// particle markers in their widest (bracketed) shape.
func fill(form string, declared map[string]string) string {
	return placeholderRe.ReplaceAllStringFunc(form, func(m string) string {
		sub := placeholderRe.FindStringSubmatch(m)
		v := ""
		if declared[sub[1]] == "int" {
			v = "999"
		}
		if sub[2] != "" {
			a, b, _ := strings.Cut(sub[2][1:], "/")
			return v + a + "(" + b + ")"
		}
		return v
	})
}
