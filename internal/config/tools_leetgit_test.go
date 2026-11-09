package config_test

import (
	"testing"

	"github.com/patrache/masil/internal/config"
)

func TestToolsConfigDecode(t *testing.T) {
	c, err := loadBody(t, "[tools.lazygit]\ntheme_overlay = false\npath = \"/opt/lg\"\nrepo = \"~/src/masil\"\nopen = \"pane\"\n")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	lg := config.DefaultLazygitToolConfig()
	if err := c.ToolSection("git", &lg); err != nil {
		t.Fatalf("ToolSection: %v", err)
	}
	if lg.OverlayOn() {
		t.Error("theme_overlay = false: OverlayOn() = true")
	}
	if lg.Path != "/opt/lg" || lg.Repo != "~/src/masil" || lg.Open != "pane" {
		t.Errorf("decoded = %+v", lg)
	}

	// Defaults: overlay on, open auto.
	c, err = loadBody(t, "")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	lg = config.DefaultLazygitToolConfig()
	if err := c.ToolSection("git", &lg); err != nil {
		t.Fatal(err)
	}
	if !lg.OverlayOn() || lg.Open != "auto" || lg.Path != "" || lg.Repo != "" {
		t.Errorf("default lazygit = %+v", lg)
	}
	if !(config.LazygitToolConfig{}).OverlayOn() {
		t.Error("nil ThemeOverlay: OverlayOn() = false")
	}

	c, err = loadBody(t, "[tools.leet]\nlang = \"golang\"\ndir = \"~/lc\"\nauto_status = true\n")
	if err != nil {
		t.Fatal(err)
	}
	lc := config.DefaultLeetToolConfig()
	if err := c.ToolSection("leet", &lc); err != nil {
		t.Fatal(err)
	}
	if lc.Lang != "golang" || lc.Dir != "~/lc" || !lc.AutoStatus || lc.Open != "auto" {
		t.Errorf("leet = %+v", lc)
	}
	if d := config.DefaultLeetToolConfig(); d != (config.LeetToolConfig{Open: "auto"}) {
		t.Errorf("default leet = %+v", d)
	}
}
