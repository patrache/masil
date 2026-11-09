package config

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/BurntSushi/toml"
)

// toolSections maps a tool ID to its [tools.<section>] name where the two
// differ: the spec names the lazygit table [tools.lazygit] while the tool ID
// is "git". Every other tool (leet, workspace, arch, api) uses its ID.
var toolSections = map[string]string{
	"git": "lazygit",
}

// ToolConfigSection returns the [tools.<section>] name for a tool ID.
func ToolConfigSection(toolID string) string {
	if s, ok := toolSections[toolID]; ok {
		return s
	}
	return toolID
}

// validToolOpen lists the accepted [tools.<section>] open values.
var validToolOpen = []string{"auto", "pane", "exec", "window"}

// toolTable returns the raw table of a tool ID, or nil.
func (c *Config) toolTable(tool string) map[string]any {
	if c == nil {
		return nil
	}
	return c.Tools[ToolConfigSection(tool)]
}

// ToolString returns [tools.<section>] key as a string, or def when the
// section or key is missing or the value is not a string.
func (c *Config) ToolString(tool, key, def string) string {
	if v, ok := c.toolTable(tool)[key].(string); ok {
		return v
	}
	return def
}

// ToolBool returns [tools.<section>] key as a bool, or def when the section
// or key is missing or the value is not a bool.
func (c *Config) ToolBool(tool, key string, def bool) bool {
	if v, ok := c.toolTable(tool)[key].(bool); ok {
		return v
	}
	return def
}

// ToolOpen returns the tool's open mode: "auto", "pane", "exec" or "window"
// (case-insensitive in the file). Anything else, or no value, is "auto".
func (c *Config) ToolOpen(tool string) string {
	v := strings.ToLower(strings.TrimSpace(c.ToolString(tool, "open", "auto")))
	for _, ok := range validToolOpen {
		if v == ok {
			return v
		}
	}
	return "auto"
}

// ToolSection decodes the tool's [tools.<section>] table into the struct
// pointed to by v (toml tags). A missing section leaves v untouched and
// returns nil. A wrong-typed key keeps the value v already had (its default)
// and the error wraps ErrPartial naming the key; other keys still decode.
// Unknown keys are ignored.
func (c *Config) ToolSection(tool string, v any) error {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer || rv.IsNil() || rv.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("config: ToolSection(%q): target must be a non-nil pointer to a struct, got %T", tool, v)
	}
	table := c.toolTable(tool)
	if table == nil {
		return nil
	}
	// Re-encode the raw table so it goes through the same per-key decoding as
	// Load (a wrong-typed key resets only itself).
	b, err := toml.Marshal(table)
	if err != nil {
		return fmt.Errorf("%w: tools.%s: %v", ErrPartial, ToolConfigSection(tool), err)
	}
	var raw map[string]toml.Primitive
	md, err := toml.Decode(string(b), &raw)
	if err != nil {
		return fmt.Errorf("%w: tools.%s: %v", ErrPartial, ToolConfigSection(tool), err)
	}
	bad := decodeFields(&md, raw, rv.Elem(), "tools."+ToolConfigSection(tool)+".")
	if len(bad) > 0 {
		return fmt.Errorf("%w: %s", ErrPartial, strings.Join(bad, ", "))
	}
	return nil
}
