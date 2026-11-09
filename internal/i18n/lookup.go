package i18n

import (
	"maps"
	"math"
	"slices"
	"strconv"
)

// Lookup renders area.key with named args for keys only known at run time
// (alert template ids). Numbers may be any Go integer or float64 (JSON).
// Unknown area or key reports false.
func (t *T) Lookup(area, key string, args map[string]any) (string, bool) {
	a := areas[area]
	if a == nil {
		return "", false
	}
	e := a.keys[key]
	if e == nil {
		return "", false
	}
	var pairs []string
	n := 0
	for _, p := range e.params {
		v, num := argString(args[p.Name])
		pairs = append(pairs, p.Name, v)
		if p.Name == e.count {
			n = num
		}
	}
	return t.renderEntry(e, false, n, pairs), true
}

// argString formats one dynamic argument; the int is its value for plurals.
func argString(v any) (string, int) {
	switch x := v.(type) {
	case nil:
		return "", 0
	case string:
		return x, 0
	case int:
		return strconv.Itoa(x), x
	case int64:
		return strconv.FormatInt(x, 10), int(x)
	case int32:
		return strconv.FormatInt(int64(x), 10), int(x)
	case uint:
		return strconv.FormatUint(uint64(x), 10), int(x)
	case uint64:
		return strconv.FormatUint(x, 10), int(x)
	case float64:
		if x == math.Trunc(x) && math.Abs(x) < 1<<53 {
			return strconv.FormatInt(int64(x), 10), int(x)
		}
		return strconv.FormatFloat(x, 'f', -1, 64), int(x)
	case interface{ String() string }:
		return x.String(), 0
	default:
		return "", 0
	}
}

// Entry is one catalog key with its meta and every language's text.
type Entry struct {
	Area, Key, Desc, Count string
	Params                 []Param
	Max                    int
	NoTruncate, Legal      bool
	Text                   map[Locale]Forms // only languages that have the key
}

// Entries lists every key sorted by area then key (review sheets, width and
// no_truncate tests, the alert-template coverage test).
func Entries() []Entry {
	var out []Entry
	for _, name := range slices.Sorted(maps.Keys(areas)) {
		a := areas[name]
		for _, key := range slices.Sorted(maps.Keys(a.keys)) {
			e := a.keys[key]
			out = append(out, Entry{
				Area: name, Key: key, Desc: e.desc, Count: e.count,
				Params: slices.Clone(e.params), Max: e.max,
				NoTruncate: e.noTruncate, Legal: e.legal,
				Text: maps.Clone(e.text),
			})
		}
	}
	return out
}
