package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"reflect"
	"slices"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// DefaultServer is the built-in server URL. Release builds override it via
// -ldflags "-X github.com/patrache/masil/internal/config.DefaultServer=https://...".
var DefaultServer = "http://localhost:8787"

// ErrPartial reports that Load replaced some invalid values with defaults.
var ErrPartial = errors.New("config: some values were invalid and replaced by defaults")

// ErrNotLoaded is returned by Save for a Config that Load built from Defaults()
// because config.toml could not be read or parsed: saving it would overwrite the
// user's file (server URL, key overrides, ...) with defaults. The caller keeps the
// change in memory only and asks the user to fix the file.
var ErrNotLoaded = errors.New("config: config.toml did not load; not overwriting it")

// Config mirrors config.toml (CA 3.3).
type Config struct {
	Server string              `toml:"server"`
	Theme  string              `toml:"theme"`
	Layout LayoutConfig        `toml:"layout"`
	UI     UIConfig            `toml:"ui"`
	Notify NotifyConfig        `toml:"notify"`
	Keys   map[string][]string `toml:"keys"`

	// Terminal is the [terminal] table (embedded panes, spec A7.4).
	Terminal TerminalConfig `toml:"terminal"`
	// Tools holds the raw [tools.<section>] tables. Keys are config section
	// names (ToolConfigSection), not tool IDs; read them through ToolString,
	// ToolBool, ToolOpen and ToolSection, which take tool IDs.
	Tools map[string]map[string]any `toml:"tools"`

	notLoaded bool // Load fell back to Defaults() on a read/parse failure => Save refuses
}

// LayoutConfig is the [layout] table.
type LayoutConfig struct {
	Pin             string `toml:"pin"`
	CommandMinCols  int    `toml:"command_min_cols"`
	CommandMinRows  int    `toml:"command_min_rows"`
	StandardMinCols int    `toml:"standard_min_cols"`
	StandardMinRows int    `toml:"standard_min_rows"`
	TinyMaxCols     int    `toml:"tiny_max_cols"`
	TinyMaxRows     int    `toml:"tiny_max_rows"`
	WidgetMinRows   int    `toml:"widget_min_rows"`
}

// UIConfig is the [ui] table.
type UIConfig struct {
	Mouse           bool   `toml:"mouse"`
	Images          string `toml:"images"`
	Animations      bool   `toml:"animations"`
	PaintBackground bool   `toml:"paint_background"`
	Clock           string `toml:"clock"`
	// Language is auto | ko | en | zh-Hans (i18n spec 6.2). 0.1.3 opens ko only
	// and hides the settings row; MASIL_LANG still overrides it.
	Language string `toml:"language"`
}

// NotifyConfig is the [notify] table.
type NotifyConfig struct {
	Desktop bool `toml:"desktop"`
}

// TerminalConfig is the [terminal] table.
type TerminalConfig struct {
	// Embedded allows the embedded terminal pane (default true). false makes
	// every tool open in exec (screen switch) mode: the escape hatch of A7.4.
	Embedded bool `toml:"embedded"`
}

// Defaults returns a Config with every field filled (CA 3.3 values).
func Defaults() *Config {
	return &Config{
		Server: "",
		Theme:  "amber",
		Layout: LayoutConfig{
			Pin:             "auto",
			CommandMinCols:  200,
			CommandMinRows:  50,
			StandardMinCols: 100,
			StandardMinRows: 40,
			TinyMaxCols:     43,
			TinyMaxRows:     15,
			WidgetMinRows:   24,
		},
		UI: UIConfig{
			Mouse:           true,
			Images:          "auto",
			Animations:      true,
			PaintBackground: true,
			Clock:           "24h",
			Language:        "auto",
		},
		Notify:   NotifyConfig{Desktop: true},
		Keys:     map[string][]string{},
		Terminal: TerminalConfig{Embedded: true},
	}
}

// Accepted enum values (CA 3.3). Theme letters a–d alias the four themes;
// config does not import theme, so the list is duplicated here.
var (
	validThemes = []string{"charm", "midnight", "cream", "amber", "a", "b", "c", "d"}
	validPins   = []string{"auto", "command", "standard", "compact", "widget", "tiny"}
	validImages = []string{"auto", "kitty", "iterm2", "halfblock", "off"}
	validClocks = []string{"24h", "12h"}
	validLangs  = []string{"auto", "ko", "en", "zh-hans"} // compared lower-cased

)

// Load reads config.toml: missing file => Defaults(); unknown keys ignored;
// invalid values => default + warning in the returned error wrapper ErrPartial.
// Invalid is per field: a wrong-typed value (e.g. mouse="no") resets only that
// key and every other decoded field is kept; the error names the reset keys.
// Only a file that cannot be read or does not parse at all (toml.ParseError)
// yields Defaults() and an ErrPartial-wrapped error; that Config (and any copy of
// it) is marked so Save refuses it with ErrNotLoaded instead of overwriting the file.
// The returned *Config is never nil.
func Load(p Paths) (*Config, error) {
	b, err := os.ReadFile(p.Config)
	if errors.Is(err, os.ErrNotExist) {
		return Defaults(), nil
	}
	if err != nil {
		return fallback(), fmt.Errorf("%w: read %s: %v", ErrPartial, p.Config, err)
	}
	permErr := fixPerm(p.Config, 0o600)

	// Parse into primitives first: any error here is a real syntax error
	// (toml.ParseError), and only then does the whole file fall back.
	var raw map[string]toml.Primitive
	md, err := toml.Decode(string(b), &raw)
	if err != nil {
		return fallback(), errors.Join(fmt.Errorf("%w: %v", ErrPartial, err), permErr)
	}
	c := Defaults()
	bad := decodeFields(&md, raw, reflect.ValueOf(c).Elem(), "")
	if c.Keys == nil {
		c.Keys = map[string][]string{}
	}
	bad = append(bad, c.validate()...)
	if len(bad) > 0 {
		return c, errors.Join(fmt.Errorf("%w: %s", ErrPartial, strings.Join(bad, ", ")), permErr)
	}
	return c, permErr
}

// fallback is Defaults() marked as not loaded from the existing file (Save refuses it).
func fallback() *Config {
	c := Defaults()
	c.notLoaded = true
	return c
}

// decodeFields decodes each key of prims into the matching (toml-tagged) field
// of the struct rv on its own, so a wrong-typed value only leaves that one
// field at its default. Nested structs recurse per key; maps decode per entry.
// Unknown keys are ignored. It returns the sorted key paths that failed.
func decodeFields(md *toml.MetaData, prims map[string]toml.Primitive, rv reflect.Value, prefix string) []string {
	var bad []string
	rt := rv.Type()
	for key, prim := range prims {
		idx := fieldIndex(rt, key)
		if idx < 0 {
			continue // unknown key
		}
		fv := rv.Field(idx)
		name := prefix + key
		switch fv.Kind() {
		case reflect.Struct:
			sub, ok := decodeTable(md, prim)
			if !ok {
				bad = append(bad, name)
				continue
			}
			bad = append(bad, decodeFields(md, sub, fv, name+".")...)
		case reflect.Map:
			sub, ok := decodeTable(md, prim)
			if !ok {
				bad = append(bad, name)
				continue
			}
			m := reflect.MakeMapWithSize(fv.Type(), len(sub))
			for k, ep := range sub {
				ev := reflect.New(fv.Type().Elem())
				if err := md.PrimitiveDecode(ep, ev.Interface()); err != nil {
					bad = append(bad, name+"."+k)
					continue
				}
				m.SetMapIndex(reflect.ValueOf(k).Convert(fv.Type().Key()), ev.Elem())
			}
			fv.Set(m)
		default:
			v := reflect.New(fv.Type())
			if err := md.PrimitiveDecode(prim, v.Interface()); err != nil {
				bad = append(bad, name)
				continue
			}
			fv.Set(v.Elem())
		}
	}
	sort.Strings(bad)
	return bad
}

// decodeTable splits a TOML table into per-key primitives. ok is false when
// prim is not a table (toml's map decoding silently accepts a non-table as an
// empty map, so the type is probed first).
func decodeTable(md *toml.MetaData, prim toml.Primitive) (map[string]toml.Primitive, bool) {
	var probe any
	if err := md.PrimitiveDecode(prim, &probe); err != nil {
		return nil, false
	}
	if _, ok := probe.(map[string]any); !ok {
		return nil, false
	}
	var sub map[string]toml.Primitive
	if err := md.PrimitiveDecode(prim, &sub); err != nil {
		return nil, false
	}
	return sub, true
}

// fieldIndex finds the field whose toml tag is key (exact match first, then
// case-insensitive, like toml.Decode), or -1.
func fieldIndex(rt reflect.Type, key string) int {
	fold := -1
	for i := range rt.NumField() {
		tag, _, _ := strings.Cut(rt.Field(i).Tag.Get("toml"), ",")
		if tag == key {
			return i
		}
		if fold < 0 && strings.EqualFold(tag, key) {
			fold = i
		}
	}
	return fold
}

// validate resets every invalid field to its default and returns the names of
// the reset fields (TOML key paths).
func (c *Config) validate() []string {
	d := Defaults()
	var bad []string
	enum := func(name string, v *string, def string, allowed []string) {
		if !slices.Contains(allowed, strings.ToLower(*v)) {
			bad = append(bad, name)
			*v = def
		}
	}
	positive := func(name string, v *int, def int) {
		if *v <= 0 {
			bad = append(bad, name)
			*v = def
		}
	}
	enum("theme", &c.Theme, d.Theme, validThemes)
	enum("layout.pin", &c.Layout.Pin, d.Layout.Pin, validPins)
	positive("layout.command_min_cols", &c.Layout.CommandMinCols, d.Layout.CommandMinCols)
	positive("layout.command_min_rows", &c.Layout.CommandMinRows, d.Layout.CommandMinRows)
	positive("layout.standard_min_cols", &c.Layout.StandardMinCols, d.Layout.StandardMinCols)
	positive("layout.standard_min_rows", &c.Layout.StandardMinRows, d.Layout.StandardMinRows)
	positive("layout.tiny_max_cols", &c.Layout.TinyMaxCols, d.Layout.TinyMaxCols)
	positive("layout.tiny_max_rows", &c.Layout.TinyMaxRows, d.Layout.TinyMaxRows)
	positive("layout.widget_min_rows", &c.Layout.WidgetMinRows, d.Layout.WidgetMinRows)
	enum("ui.images", &c.UI.Images, d.UI.Images, validImages)
	enum("ui.clock", &c.UI.Clock, d.UI.Clock, validClocks)
	enum("ui.language", &c.UI.Language, d.UI.Language, validLangs)
	return bad
}

// Save writes config.toml atomically with 0600. A Config that Load could not
// read from the file (see Load) is refused with ErrNotLoaded and nothing is written.
func Save(p Paths, c *Config) error {
	if c == nil {
		return errors.New("config: Save: nil config")
	}
	if c.notLoaded {
		return ErrNotLoaded
	}
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(c); err != nil {
		return fmt.Errorf("config: encode: %w", err)
	}
	return WriteFileAtomic(p.Config, buf.Bytes(), 0o600)
}

// ServerURL resolves env MASIL_SERVER > c.Server > DefaultServer; trailing "/" trimmed.
// An empty (or blank) MASIL_SERVER counts as unset.
func (c *Config) ServerURL() string {
	s := strings.TrimSpace(os.Getenv("MASIL_SERVER"))
	if s == "" && c != nil {
		s = strings.TrimSpace(c.Server)
	}
	if s == "" {
		s = strings.TrimSpace(DefaultServer)
	}
	return strings.TrimRight(s, "/")
}
