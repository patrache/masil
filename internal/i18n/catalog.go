package i18n

import (
	"regexp"
	"sync"
)

// Forms is one language's text for a key: One (en singular), Other, Short.
type Forms struct{ One, Other, Short string }

// Param is a declared message argument; Type is "int" or "string".
type Param struct{ Name, Type string }

// entry is one key of an area table (generated).
type entry struct {
	desc              string
	params            []Param // declaration order
	count             string  // plural-selecting int param, "" = none
	max               int     // width budget in cells, 0 = none
	noTruncate, legal bool
	text              map[Locale]Forms
}

// area is one generated area table.
type area struct {
	name string
	keys map[string]*entry
}

var (
	cacheMu sync.Mutex
	cache   = map[Locale]*T{}
)

// For returns the catalog for l (cached). Unknown locales get ko.
func For(l Locale) *T {
	switch l {
	case Ko, En, ZhHans, Pseudo:
	default:
		l = Ko
	}
	cacheMu.Lock()
	defer cacheMu.Unlock()
	t := cache[l]
	if t == nil {
		t = newT(l)
		cache[l] = t
	}
	return t
}

// KoT returns the Korean catalog (For(Ko)). It is not named Ko because Ko is
// the Locale constant.
func KoT() *T { return For(Ko) }

// Locale is the language this catalog renders.
func (t *T) Locale() Locale { return t.locale }

// fallback is the lookup order for a language: zh-Hans → en → ko, en → ko.
func fallback(l Locale) []Locale {
	switch l {
	case ZhHans:
		return []Locale{ZhHans, En, Ko}
	case En, Pseudo:
		return []Locale{En, Ko}
	default:
		return []Locale{Ko}
	}
}

// pick returns the first language in the fallback chain that has the key.
func (e *entry) pick(l Locale) (Locale, Forms) {
	for _, c := range fallback(l) {
		if f, ok := e.text[c]; ok && f.Other != "" {
			return c, f
		}
	}
	return Ko, e.text[Ko]
}

// render picks the form (short, en plural by n) and fills args (name, value
// pairs). Args are inserted as is — they are never parsed as templates.
func (t *T) render(a *area, key string, short bool, n int, args ...string) string {
	e := a.keys[key]
	if e == nil {
		return a.name + "." + key
	}
	return t.renderEntry(e, short, n, args)
}

func (t *T) renderEntry(e *entry, short bool, n int, args []string) string {
	l, f := e.pick(t.locale)
	s := f.Other
	switch {
	case short && f.Short != "":
		s = f.Short
	case !short && e.count != "" && l == En && n == 1 && f.One != "":
		s = f.One
	}
	if t.locale == Pseudo {
		return pseudo(s, args)
	}
	return fill(s, args)
}

// placeholderRe matches {name} and the ko particle marker {name|A/B}.
var placeholderRe = regexp.MustCompile(`\{([a-z][a-z0-9_]*)(?:\|([^{}]*))?\}`)

func fill(s string, args []string) string {
	if len(args) == 0 {
		return s
	}
	return placeholderRe.ReplaceAllStringFunc(s, func(m string) string {
		sub := placeholderRe.FindStringSubmatch(m)
		v := ""
		for i := 0; i+1 < len(args); i += 2 {
			if args[i] == sub[1] {
				v = args[i+1]
				break
			}
		}
		if sub[2] != "" {
			return Particle(v, sub[2])
		}
		return v
	})
}
