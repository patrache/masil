package config

import "errors"

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
	// TODO(core): Task 4
	return nil
}

// Load reads config.toml: missing file => Defaults(); unknown keys ignored;
// invalid values => default + warning in the returned error wrapper ErrPartial.
func Load(p Paths) (*Config, error) {
	// TODO(core): Task 4
	return nil, nil
}

// Save writes config.toml atomically with 0600.
func Save(p Paths, c *Config) error {
	// TODO(core): Task 4
	return nil
}

// ServerURL resolves env MASIL_SERVER > c.Server > DefaultServer; trailing "/" trimmed.
func (c *Config) ServerURL() string {
	// TODO(core): Task 4
	return ""
}
