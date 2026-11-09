package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
)

// Locale directory names, ko first (ko is the source of keys, params and meta).
const srcLang = "ko"

var langs = []string{"ko", "en", "zh-Hans"}

// goLocale maps a locale directory to its i18n constant name.
var goLocale = map[string]string{"ko": "Ko", "en": "En", "zh-Hans": "ZhHans"}

var (
	areaRe  = regexp.MustCompile(`^[a-z][a-z0-9]*$`)
	keyRe   = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	paramRe = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
)

// Catalog is every area read from a locales directory.
type Catalog struct {
	Areas []*Area // sorted by name
}

// Area is one <area>.toml across languages.
type Area struct {
	Name, Field, Type string
	Keys              []*Key           // ko keys, sorted
	files             map[string]*File // by language
}

// File is one parsed locales/<lang>/<area>.toml.
type File struct {
	Lang, Area string
	Header     string // leading comment lines, kept by formatTOML
	Entries    map[string]*Raw
}

// Param is a declared message argument.
type Param struct{ Name, Type string }

// Raw is one [key] table as written.
type Raw struct {
	Desc              string
	Params            []Param
	Count             string
	Max               int
	NoTruncate, Legal bool
	One, Other, Short string
	has               map[string]bool
}

// Key is one ko key with every language's text.
type Key struct {
	Area, Name string
	*Raw                       // ko meta and text
	Text       map[string]*Raw // by language (ko included)
}

func (k *Key) id() string { return k.Area + "." + k.Name }

// areaNames turns an area file name into the T field and type names.
func areaNames(area string) (field, typ string, err error) {
	if !areaRe.MatchString(area) {
		return "", "", fmt.Errorf("%q: bad area name (want [a-z][a-z0-9]*)", area)
	}
	field = strings.ToUpper(area[:1]) + area[1:]
	return field, field + "T", nil
}

// load reads dir/<lang>/<area>.toml for every language. Errors are collected,
// not fatal, so one run reports them all.
func load(dir string) (*Catalog, []error) {
	var errs []error
	byArea := map[string]*Area{}
	for _, lang := range langs {
		matches, err := filepath.Glob(filepath.Join(dir, lang, "*.toml"))
		if err != nil {
			return nil, []error{err}
		}
		slices.Sort(matches)
		for _, path := range matches {
			name := strings.TrimSuffix(filepath.Base(path), ".toml")
			field, typ, err := areaNames(name)
			if err != nil {
				errs = append(errs, fmt.Errorf("%s/%s.toml: %w", lang, name, err))
				continue
			}
			a := byArea[name]
			if a == nil {
				if lang != srcLang {
					errs = append(errs, fmt.Errorf("%s/%s.toml: area %s not in ko", lang, name, name))
					continue
				}
				a = &Area{Name: name, Field: field, Type: typ, files: map[string]*File{}}
				byArea[name] = a
			}
			f, ferrs := parseFile(path, lang, name)
			errs = append(errs, ferrs...)
			if f != nil {
				a.files[lang] = f
			}
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, append(errs, err)
	}
	for _, e := range entries {
		if e.IsDir() && !slices.Contains(langs, e.Name()) {
			errs = append(errs, fmt.Errorf("%s: unknown locale directory (want %s)", e.Name(), strings.Join(langs, ", ")))
		}
	}
	c := &Catalog{}
	for _, name := range sortedKeys(byArea) {
		a := byArea[name]
		if ko := a.files[srcLang]; ko != nil {
			for _, k := range sortedKeys(ko.Entries) {
				key := &Key{Area: name, Name: k, Raw: ko.Entries[k], Text: map[string]*Raw{}}
				for _, lang := range langs {
					if f := a.files[lang]; f != nil && f.Entries[k] != nil {
						key.Text[lang] = f.Entries[k]
					}
				}
				a.Keys = append(a.Keys, key)
			}
		}
		c.Areas = append(c.Areas, a)
	}
	return c, errs
}

var koFields = []string{"desc", "params", "count", "max", "no_truncate", "legal", "one", "other", "short"}
var textFields = []string{"one", "other", "short"}

func parseFile(path, lang, area string) (*File, []error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, []error{err}
	}
	where := lang + "/" + area + ".toml"
	var raw map[string]any
	md, err := toml.Decode(string(data), &raw)
	if err != nil {
		return nil, []error{fmt.Errorf("%s: %v", where, err)}
	}
	f := &File{Lang: lang, Area: area, Header: header(string(data)), Entries: map[string]*Raw{}}
	var errs []error
	bad := func(key, format string, args ...any) {
		errs = append(errs, fmt.Errorf("%s.%s (%s): %s", area, key, lang, fmt.Sprintf(format, args...)))
	}
	// params declaration order comes from the metadata, not the map.
	order := map[string][]string{}
	for _, k := range md.Keys() {
		if len(k) == 3 && k[1] == "params" {
			order[k[0]] = append(order[k[0]], k[2])
		}
	}
	for key, v := range raw {
		tbl, ok := v.(map[string]any)
		if !ok {
			bad(key, "top-level values must be [key] tables")
			continue
		}
		if !keyRe.MatchString(key) {
			bad(key, "bad key (want [a-z][a-z0-9_]*)")
			continue
		}
		r := &Raw{has: map[string]bool{}}
		allowed := textFields
		if lang == srcLang {
			allowed = koFields
		}
		for field, fv := range tbl {
			if !slices.Contains(allowed, field) {
				if slices.Contains(koFields, field) {
					bad(key, "field %s only in ko", field)
				} else {
					bad(key, "unknown field %s", field)
				}
				continue
			}
			r.has[field] = true
			var typeErr bool
			switch field {
			case "desc":
				r.Desc, typeErr = str(fv)
			case "count":
				r.Count, typeErr = str(fv)
			case "one":
				r.One, typeErr = str(fv)
			case "other":
				r.Other, typeErr = str(fv)
			case "short":
				r.Short, typeErr = str(fv)
			case "max":
				n, ok := fv.(int64)
				r.Max, typeErr = int(n), !ok || n <= 0
			case "no_truncate":
				r.NoTruncate, typeErr = boolean(fv)
			case "legal":
				r.Legal, typeErr = boolean(fv)
			case "params":
				ps, ok := fv.(map[string]any)
				if !ok {
					typeErr = true
					break
				}
				for _, name := range order[key] {
					typ, _ := ps[name].(string)
					r.Params = append(r.Params, Param{Name: name, Type: typ})
				}
			}
			if typeErr {
				bad(key, "field %s has the wrong type", field)
			}
		}
		f.Entries[key] = r
	}
	slices.SortFunc(errs, func(a, b error) int { return strings.Compare(a.Error(), b.Error()) })
	return f, errs
}

func str(v any) (string, bool) {
	s, ok := v.(string)
	return s, !ok
}

func boolean(v any) (bool, bool) {
	b, ok := v.(bool)
	return b, !ok
}

// header returns the comment block before the first table.
func header(src string) string {
	var b strings.Builder
	for _, line := range strings.SplitAfter(src, "\n") {
		t := strings.TrimSpace(line)
		if t != "" && !strings.HasPrefix(t, "#") {
			break
		}
		b.WriteString(line)
	}
	h := strings.TrimRight(b.String(), "\n")
	if strings.TrimSpace(h) == "" {
		return ""
	}
	return h + "\n"
}

func sortedKeys[V any](m map[string]V) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	slices.Sort(ks)
	return ks
}

var errFailed = errors.New("i18n catalog check failed")
