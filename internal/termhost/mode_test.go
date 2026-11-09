package termhost

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/patrache/masil/internal/config"
)

func TestChooseRules(t *testing.T) {
	base := Env{GOOS: "darwin", TERM: "xterm-256color", Embedded: true, ToolOpen: ModeAuto}
	tmux := func(v string) Env {
		e := base
		e.TMUX = "/tmp/tmux-501/default,1,0"
		e.TmuxVersion = v
		e.ToolOpen = ModeWindow
		return e
	}
	with := func(f func(*Env)) Env { e := base; f(&e); return e }
	cases := []struct {
		name     string
		req      Request
		env      Env
		want     Mode
		downgrad bool
	}{
		{"windows", Request{}, with(func(e *Env) { e.GOOS = "windows" }), ModeExec, false},
		{"embedded off", Request{}, with(func(e *Env) { e.Embedded = false }), ModeExec, false},
		{"term dumb", Request{}, with(func(e *Env) { e.TERM = "dumb" }), ModeExec, false},
		{"tiny", Request{}, with(func(e *Env) { e.Tiny = true }), ModeExec, false},
		{"window tmux 3.2", Request{}, tmux("3.2"), ModeWindow, false},
		{"window tmux 3.1", Request{}, tmux("3.1"), ModeExec, true},
		{"window tmux 3.3a", Request{}, tmux("3.3a"), ModeWindow, false},
		{"window tmux next-3.5", Request{}, tmux("next-3.5"), ModeWindow, false},
		{"window tmux 4.0", Request{}, tmux("4.0"), ModeWindow, false},
		{"window tmux unknown", Request{}, tmux(""), ModeExec, true},
		{"window no tmux", Request{}, with(func(e *Env) { e.ToolOpen = ModeWindow }), ModeExec, true},
		{"window on windows", Request{}, with(func(e *Env) { e.GOOS = "windows"; e.ToolOpen = ModeWindow }), ModeExec, false},
		{"config exec", Request{}, with(func(e *Env) { e.ToolOpen = ModeExec }), ModeExec, false},
		{"config pane", Request{}, with(func(e *Env) { e.ToolOpen = ModePane }), ModePane, false},
		{"default pane", Request{}, base, ModePane, false},
		{"req auto", Request{Mode: ModeAuto}, base, ModePane, false},
		{"req exec wins", Request{Mode: ModeExec}, base, ModeExec, false},
		{"req exec over config window", Request{Mode: ModeExec}, tmux("3.4"), ModeExec, false},
		{"req pane over config window", Request{Mode: ModePane}, tmux("3.4"), ModePane, false},
		{"req window", Request{Mode: ModeWindow}, with(func(e *Env) { e.TMUX = "x"; e.TmuxVersion = "3.2" }), ModeWindow, false},
		{"req window no tmux", Request{Mode: ModeWindow}, base, ModeExec, true},
		{"req pane windows", Request{Mode: ModePane}, with(func(e *Env) { e.GOOS = "windows" }), ModeExec, true},
		{"req window windows", Request{Mode: ModeWindow}, with(func(e *Env) { e.GOOS = "windows"; e.TMUX = "x"; e.TmuxVersion = "3.4" }), ModeExec, true},
		{"req exec windows", Request{Mode: ModeExec}, with(func(e *Env) { e.GOOS = "windows" }), ModeExec, false},
		{"req pane embedded off", Request{Mode: ModePane}, with(func(e *Env) { e.Embedded = false }), ModeExec, true},
		// embedded = false turns off panes only; the external window stays (CA 3.2).
		{"config window embedded off", Request{}, with(func(e *Env) { *e = tmux("3.4"); e.Embedded = false }), ModeWindow, false},
		{"config window embedded off no tmux", Request{}, with(func(e *Env) { e.Embedded = false; e.ToolOpen = ModeWindow }), ModeExec, true},
		{"req window embedded off", Request{Mode: ModeWindow}, with(func(e *Env) { *e = tmux("3.4"); e.Embedded = false }), ModeWindow, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, down := Choose(c.req, c.env)
			if got != c.want || down != c.downgrad {
				t.Fatalf("Choose = (%q, %v), want (%q, %v)", got, down, c.want, c.downgrad)
			}
		})
	}
}

func TestTmuxVersionParse(t *testing.T) {
	cases := []struct {
		v            string
		major, minor int
		ok           bool
	}{
		{"3.2", 3, 2, true},
		{"3.3a", 3, 3, true},
		{"next-3.5", 3, 5, true},
		{"tmux 3.2a", 3, 2, true},
		{"tmux next-3.6", 3, 6, true},
		{"3.10", 3, 10, true},
		{"4", 4, 0, true},
		{"", 0, 0, false},
		{"master", 0, 0, false},
	}
	for _, c := range cases {
		ma, mi, ok := parseTmuxVersion(c.v)
		if ma != c.major || mi != c.minor || ok != c.ok {
			t.Errorf("parseTmuxVersion(%q) = %d.%d %v, want %d.%d %v", c.v, ma, mi, ok, c.major, c.minor, c.ok)
		}
	}
	if !tmuxAtLeast("3.10", 3, 3) || tmuxAtLeast("3.2", 3, 3) || !tmuxAtLeast("3.3", 3, 3) {
		t.Fatal("tmuxAtLeast comparisons wrong")
	}
}

func loadConfig(t *testing.T, body string) *config.Config {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("MASIL_CONFIG_DIR", dir)
	t.Setenv("MASIL_CACHE_DIR", t.TempDir())
	p, err := config.ResolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	c, _ := config.Load(p)
	return c
}

func TestToolConfigAccessors(t *testing.T) {
	t.Setenv("TMUX", "")
	c := loadConfig(t, "[tools.lazygit]\nopen = \"nope\"\n")
	if got := c.ToolOpen("git"); got != "auto" {
		t.Fatalf("ToolOpen(git) = %q, want auto", got)
	}
	c = loadConfig(t, "[tools.lazygit]\nopen = \"window\"\n")
	if got := c.ToolOpen("git"); got != "window" {
		t.Fatalf("ToolOpen(git) = %q, want window", got)
	}
	if env := CurrentEnv(c, "git", false); env.ToolOpen != ModeWindow {
		t.Fatalf("CurrentEnv(git).ToolOpen = %q, want window", env.ToolOpen)
	}
	c = loadConfig(t, "[tools.git]\nopen = \"window\"\n")
	if env := CurrentEnv(c, "git", false); env.ToolOpen != ModeAuto {
		t.Fatalf("[tools.git] was read: ToolOpen = %q", env.ToolOpen)
	}
}

func TestCurrentEnv(t *testing.T) {
	t.Setenv("TERM", "screen-256color")
	t.Setenv("TMUX", "/tmp/tmux,1,0")
	restore := detectTmuxVersion
	detectTmuxVersion = func() string { return "3.4" }
	t.Cleanup(func() { detectTmuxVersion = restore })

	c := loadConfig(t, "[terminal]\nembedded = false\n")
	env := CurrentEnv(c, "leet", true)
	if env.TERM != "screen-256color" || env.TMUX == "" || env.TmuxVersion != "3.4" || env.Embedded || !env.Tiny || env.ToolOpen != ModeAuto || env.GOOS == "" {
		t.Fatalf("CurrentEnv = %+v", env)
	}
	if env := CurrentEnv(nil, "leet", false); !env.Embedded || env.ToolOpen != ModeAuto {
		t.Fatalf("CurrentEnv(nil) = %+v, want embedded and auto", env)
	}

	t.Setenv("TMUX", "")
	detectTmuxVersion = func() string { t.Fatal("tmux probed without $TMUX"); return "" }
	if env := CurrentEnv(nil, "leet", false); env.TmuxVersion != "" {
		t.Fatalf("TmuxVersion = %q without $TMUX", env.TmuxVersion)
	}
}
