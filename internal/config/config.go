package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
)

// DefaultServer is the built-in server URL. Release builds override it via
// -ldflags "-X github.com/patrache/masil/internal/config.DefaultServer=https://...".
var DefaultServer = "http://localhost:8787"

// ErrPartial reports that Load replaced some invalid values with defaults.
var ErrPartial = errors.New("config: some values were invalid and replaced by defaults")

// Config mirrors config.toml (CA 3.3).
type Config struct {
	Server string              `toml:"server"`
	Theme  string              `toml:"theme"`
	Layout LayoutConfig        `toml:"layout"`
	UI     UIConfig            `toml:"ui"`
	Notify NotifyConfig        `toml:"notify"`
	Keys   map[string][]string `toml:"keys"`
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
}

// NotifyConfig is the [notify] table.
type NotifyConfig struct {
	Desktop bool `toml:"desktop"`
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
		},
		Notify: NotifyConfig{Desktop: true},
		Keys:   map[string][]string{},
	}
}

// Accepted enum values (CA 3.3). Theme letters a–d alias the four themes;
// config does not import theme, so the list is duplicated here.
var (
	validThemes = []string{"charm", "midnight", "cream", "amber", "a", "b", "c", "d"}
	validPins   = []string{"auto", "command", "standard", "compact", "widget", "tiny"}
	validImages = []string{"auto", "kitty", "iterm2", "halfblock", "off"}
	validClocks = []string{"24h", "12h"}
)

// Load reads config.toml: missing file => Defaults(); unknown keys ignored;
// invalid values => default + warning in the returned error wrapper ErrPartial.
// A file that does not parse yields Defaults() and an ErrPartial-wrapped error.
// The returned *Config is never nil.
func Load(p Paths) (*Config, error) {
	b, err := os.ReadFile(p.Config)
	if errors.Is(err, os.ErrNotExist) {
		return Defaults(), nil
	}
	if err != nil {
		return Defaults(), fmt.Errorf("%w: read %s: %v", ErrPartial, p.Config, err)
	}
	permErr := fixPerm(p.Config, 0o600)

	c := Defaults()
	if _, err := toml.Decode(string(b), c); err != nil {
		return Defaults(), errors.Join(fmt.Errorf("%w: %v", ErrPartial, err), permErr)
	}
	if c.Keys == nil {
		c.Keys = map[string][]string{}
	}
	if bad := c.validate(); len(bad) > 0 {
		return c, errors.Join(fmt.Errorf("%w: %s", ErrPartial, strings.Join(bad, ", ")), permErr)
	}
	return c, permErr
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
	return bad
}

// Save writes config.toml atomically with 0600.
func Save(p Paths, c *Config) error {
	if c == nil {
		return errors.New("config: Save: nil config")
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
