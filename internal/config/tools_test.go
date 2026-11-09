package config_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/patrache/masil/internal/config"
)

func loadBody(t *testing.T, body string) (*config.Config, error) {
	t.Helper()
	p := testPaths(t)
	writeConfig(t, p, body, 0o600)
	return config.Load(p)
}

func TestTerminalEmbeddedDefault(t *testing.T) {
	if !config.Defaults().Terminal.Embedded {
		t.Fatal("Defaults().Terminal.Embedded = false, want true")
	}
	c, err := loadBody(t, "[terminal]\nembedded = false\n")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Terminal.Embedded {
		t.Fatal("[terminal] embedded = false not loaded")
	}
	c, err = loadBody(t, "[terminal]\nembedded = \"no\"\n")
	if !errors.Is(err, config.ErrPartial) || !c.Terminal.Embedded {
		t.Fatalf("wrong-typed embedded: err=%v embedded=%v, want ErrPartial and default true", err, c.Terminal.Embedded)
	}
}

func TestToolConfigSectionNames(t *testing.T) {
	for id, want := range map[string]string{"git": "lazygit", "leet": "leet", "workspace": "workspace", "arch": "arch", "api": "api"} {
		if got := config.ToolConfigSection(id); got != want {
			t.Errorf("ToolConfigSection(%q) = %q, want %q", id, got, want)
		}
	}
}

func TestToolStringBool(t *testing.T) {
	c, err := loadBody(t, "[tools.lazygit]\npath = \"/opt/lg\"\ntheme_overlay = false\n[tools.git]\npath = \"/wrong\"\n")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := c.ToolString("git", "path", "lazygit"); got != "/opt/lg" {
		t.Fatalf("ToolString(git, path) = %q, want /opt/lg", got)
	}
	if got := c.ToolString("git", "missing", "def"); got != "def" {
		t.Fatalf("missing key = %q, want def", got)
	}
	if got := c.ToolBool("git", "theme_overlay", true); got {
		t.Fatal("ToolBool(git, theme_overlay) = true, want false")
	}
	if got := c.ToolBool("git", "path", true); !got {
		t.Fatal("wrong-typed bool did not fall back to the default")
	}
	if got := c.ToolString("leet", "dir", "x"); got != "x" {
		t.Fatalf("absent section = %q, want default", got)
	}
	var nilCfg *config.Config
	if nilCfg.ToolString("git", "path", "d") != "d" || !nilCfg.ToolBool("git", "x", true) || nilCfg.ToolOpen("git") != "auto" {
		t.Fatal("nil *Config accessors must return defaults")
	}
}

func TestToolOpenValues(t *testing.T) {
	for body, want := range map[string]string{
		"[tools.lazygit]\nopen = \"nope\"\n":   "auto",
		"[tools.lazygit]\nopen = \"window\"\n": "window",
		"[tools.lazygit]\nopen = \"PANE\"\n":   "pane",
		"[tools.lazygit]\nopen = \"exec\"\n":   "exec",
		"[tools.lazygit]\nopen = 3\n":          "auto",
		"[tools.git]\nopen = \"window\"\n":     "auto",
		"":                                     "auto",
	} {
		c, _ := loadBody(t, body)
		if got := c.ToolOpen("git"); got != want {
			t.Errorf("%q: ToolOpen(git) = %q, want %q", body, got, want)
		}
	}
}

func TestToolSectionDecode(t *testing.T) {
	type leetCfg struct {
		Dir  string `toml:"dir"`
		Lang string `toml:"lang"`
		Auto bool   `toml:"auto_status"`
	}
	c, err := loadBody(t, "[tools.leet]\ndir = \"~/lc\"\nauto_status = true\n")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got := leetCfg{Lang: "python3"}
	if err := c.ToolSection("leet", &got); err != nil {
		t.Fatalf("ToolSection: %v", err)
	}
	if want := (leetCfg{Dir: "~/lc", Lang: "python3", Auto: true}); got != want {
		t.Fatalf("decoded = %+v, want %+v", got, want)
	}

	// No section: v untouched, nil error.
	def := leetCfg{Dir: "keep", Lang: "go"}
	v := def
	if err := c.ToolSection("api", &v); err != nil || v != def {
		t.Fatalf("absent section: err=%v v=%+v, want nil and untouched", err, v)
	}

	// Wrong type: that key keeps its default, others decode, ErrPartial names the key.
	c, err = loadBody(t, "[tools.leet]\ndir = 5\nlang = \"rust\"\n")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	v = def
	err = c.ToolSection("leet", &v)
	if !errors.Is(err, config.ErrPartial) {
		t.Fatalf("err = %v, want ErrPartial", err)
	}
	if v.Dir != "keep" || v.Lang != "rust" {
		t.Fatalf("partial decode = %+v, want dir kept and lang rust", v)
	}

	// git reads [tools.lazygit].
	type lgCfg struct {
		Repo string `toml:"repo"`
	}
	c, _ = loadBody(t, "[tools.lazygit]\nrepo = \"~/src/masil\"\n")
	var lg lgCfg
	if err := c.ToolSection("git", &lg); err != nil || lg.Repo != "~/src/masil" {
		t.Fatalf("git section: err=%v %+v", err, lg)
	}

	if err := c.ToolSection("git", lg); err == nil {
		t.Fatal("non-pointer target must fail")
	}
}

func TestToolsRoundTrip(t *testing.T) {
	p := testPaths(t)
	c := config.Defaults()
	c.Terminal.Embedded = false
	c.Tools = map[string]map[string]any{"lazygit": {"open": "window", "theme_overlay": false}}
	if err := config.Save(p, c); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := config.Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(got, c) {
		t.Fatalf("round trip = %+v, want %+v", *got, *c)
	}
}
