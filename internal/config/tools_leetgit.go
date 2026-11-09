package config

// Tool-owned [tools.<section>] key sets (P1; CA 3.3). Read them with
// Config.ToolSection, starting from the Default… value so missing keys keep
// their defaults:
//
//	lg := config.DefaultLazygitToolConfig()
//	err := cfg.ToolSection("git", &lg) // [tools.lazygit]
//
// The open mode is read with Config.ToolOpen (it validates the value).

// LeetToolConfig is [tools.leet].
type LeetToolConfig struct {
	Open       string `toml:"open"`        // auto | pane | exec | window
	Lang       string `toml:"lang"`        // LeetCode language slug ("" = remembered/default)
	Dir        string `toml:"dir"`         // solution folder ("" = ~/masil/leetcode)
	AutoStatus bool   `toml:"auto_status"` // show the solve in the status message
}

// DefaultLeetToolConfig returns [tools.leet]'s defaults.
func DefaultLeetToolConfig() LeetToolConfig {
	return LeetToolConfig{Open: "auto"}
}

// LazygitToolConfig is [tools.lazygit] (tool ID "git").
type LazygitToolConfig struct {
	Open         string `toml:"open"`          // auto | pane | exec | window
	Path         string `toml:"path"`          // lazygit executable ("" = PATH)
	Repo         string `toml:"repo"`          // repository to open ("" = start folder, then the picker)
	ThemeOverlay *bool  `toml:"theme_overlay"` // nil = on
}

// DefaultLazygitToolConfig returns [tools.lazygit]'s defaults.
func DefaultLazygitToolConfig() LazygitToolConfig {
	return LazygitToolConfig{Open: "auto"}
}

// OverlayOn reports whether the pane theme overlay is on (unset = on).
func (c LazygitToolConfig) OverlayOn() bool {
	return c.ThemeOverlay == nil || *c.ThemeOverlay
}
