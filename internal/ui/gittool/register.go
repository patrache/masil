// Package gittool is the lazygit tool (v0.1.3 spec A11): the hub row, the
// repository picker, the install guide and the running lazygit (pane, screen
// switch or tmux popup). lazygit is the user's own install; masil only finds
// it, picks the repository and passes arguments — never a shell, never the
// tool secrets, never anything to the masil server.
package gittool

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/config"
	"github.com/patrache/masil/internal/features"
	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/lazygit"
	"github.com/patrache/masil/internal/termhost"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/palette"
)

// Ids.
const (
	toolID          = ui.ToolGit
	cmdOpen         = "git.open"        // palette command
	OverlayInstall  = "gittool.install" // the install guide overlay
	pickID          = "git:pick"        // the picker instance
	instancePrefix  = "git:"            // instance ids: "git:" + absolute repo
	queryMode       = "mode"            // OpenTool query: the open mode asked for
	probeTTL        = 30 * time.Second  // how long the hub trusts a lazygit lookup
	versionTimeout  = 2 * time.Second
	resolveTimeout  = 5 * time.Second
	hubIcon         = "🌿"
	hubOrder        = 4
	stateSectionKey = toolID // tools-state.json section
)

// gitState is the tool's tools-state.json section (A8.5): the recent
// repositories, newest first, at most lazygit.RecentMax. Device-only.
type gitState struct {
	Recent []string `json:"recent"`
}

func init() {
	ui.RegisterTool(ui.ToolSpec{
		ID:          toolID,
		Order:       hubOrder,
		Icon:        hubIcon,
		Name:        func(t *i18n.T) string { return t.Lazygit.Name() },
		Status:      hubStatus,
		Available:   available,
		New:         newComponent,
		Open:        open,
		WideDefault: false,
	})
	palette.Register(palette.Command{
		ID:        cmdOpen,
		Label:     func(t *i18n.T) string { return t.Lazygit.CmdOpen() },
		Group:     palette.GroupCommand,
		Keywords:  []string{"lazygit", "git"},
		Available: func(*ui.Context) bool { return features.On(ui.ToolGit) },
		Run:       func(ctx *ui.Context, _ string) tea.Cmd { return open(ctx, termhost.ModeAuto) },
	})
	ui.RegisterOverlay(OverlayInstall, newInstall)
}

// ---- environment (tests swap it: testhooks.go) ----

var (
	envMu    sync.Mutex
	envCur   = lazygit.SystemEnv()
	probe    probeResult
	probing  bool // a lookup is running
	probeGen int  // bumped when the environment changes: an older lookup's answer is dropped
)

func curEnv() lazygit.Env {
	envMu.Lock()
	defer envMu.Unlock()
	return envCur
}

// probeResult is the last lazygit lookup the hub row used.
type probeResult struct {
	cfgPath string
	at      time.Time
	bin     string
	version string // "" = unknown
	asked   string // the binary "--version" last ran for (its answer is kept, even "")
	err     error  // lazygit.ErrNotInstalled
	done    bool
}

// probedMsg says a lookup finished (the hub redraws with it).
type probedMsg struct{}

const probedKey = "gittool.probe" // Poster.Coalesce key

// cachedProbe is the last lookup for the configured path (done false while
// there is none yet). It never runs anything itself: when the answer is
// missing or older than probeTTL it starts a lookup off the UI goroutine
// (the hub's Status runs inside View), which posts probedMsg when done.
func cachedProbe(ctx *ui.Context) probeResult {
	cfg := toolConfig(ctx)
	now := time.Now()
	if ctx != nil && ctx.Now != nil {
		now = ctx.Now()
	}
	var post ui.Poster = ui.DiscardPoster{}
	if ctx != nil && ctx.Post != nil {
		post = ctx.Post
	}
	envMu.Lock()
	defer envMu.Unlock()
	p := probe
	if p.done && p.cfgPath != cfg.Path {
		p = probeResult{} // another path: not known yet
	}
	fresh := p.done && now.Sub(p.at) < probeTTL && now.Sub(p.at) >= 0
	if !fresh && !probing {
		probing = true
		go runProbe(envCur, cfg.Path, now, probe, probeGen, post)
	}
	return p
}

// runProbe finds lazygit and, for a binary not asked before, its version.
func runProbe(env lazygit.Env, cfgPath string, now time.Time, last probeResult, gen int, post ui.Poster) {
	bin, err := lazygit.Locate(env, cfgPath)
	next := probeResult{cfgPath: cfgPath, at: now, bin: bin, err: err, done: true}
	if err == nil {
		if last.done && last.asked == bin {
			next.version, next.asked = last.version, bin
		} else {
			c, cancel := context.WithTimeout(context.Background(), versionTimeout)
			next.version, _ = lazygit.Version(c, env, bin)
			next.asked = bin
			cancel()
		}
	}
	envMu.Lock()
	stale := gen != probeGen
	if !stale {
		probe, probing = next, false
	}
	envMu.Unlock()
	if !stale {
		post.Coalesce(probedKey, probedMsg{})
	}
}

// toolConfig is [tools.lazygit] (defaults when there is no config).
func toolConfig(ctx *ui.Context) config.LazygitToolConfig {
	c := config.DefaultLazygitToolConfig()
	if ctx != nil && ctx.Config != nil {
		_ = ctx.Config.ToolSection(toolID, &c) // a wrong-typed key keeps its default
	}
	return c
}

// loadState reads the tool's section of tools-state.json.
func loadState(ctx *ui.Context) gitState {
	var st gitState
	if ctx != nil {
		ctx.ToolState.Section(stateSectionKey, &st)
	}
	return st
}

// pushRecent puts repo first in the recent list and saves it.
func pushRecent(ctx *ui.Context, repo string) {
	if ctx == nil || repo == "" {
		return
	}
	st := loadState(ctx)
	next := lazygit.PushRecent(st.Recent, repo)
	if equalStrings(next, st.Recent) {
		return
	}
	st.Recent = next
	_ = ctx.ToolState.SetSection(stateSectionKey, st) // device memory only: a failed write is not worth a toast
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// folderName is the last path element, sanitized (A11.2: only the folder
// name is shown).
func folderName(p string) string {
	return textwidth.Sanitize(filepath.Base(filepath.Clean(p)))
}

// available is ToolSpec.Available: false once a lookup found no lazygit
// (until then open's own lookup decides).
func available(ctx *ui.Context) bool {
	p := cachedProbe(ctx)
	return !p.done || p.err == nil
}

// hubStatus is the hub row's status (A8.2): the last lookup's answer, empty
// until the first one is in.
func hubStatus(ctx *ui.Context) string {
	t := ctx.T
	p := cachedProbe(ctx)
	if !p.done {
		return ""
	}
	if p.err != nil {
		return t.Lazygit.HubMissing()
	}
	if p.version == "" {
		return t.Lazygit.HubInstalledUnknown()
	}
	if st := loadState(ctx); len(st.Recent) > 0 {
		return t.Lazygit.HubInstalled(p.version, folderName(st.Recent[0]))
	}
	return t.Lazygit.HubInstalledNoRecent(p.version)
}

// open is ToolSpec.Open and the palette's git.open: the install guide when
// lazygit is missing; else the repository of A11.2 (the configured repo, the
// start folder's repository) or the picker. The lookups run in the Cmd.
func open(ctx *ui.Context, mode termhost.Mode) tea.Cmd {
	cfg := toolConfig(ctx)
	env := curEnv()
	var query map[string]string
	if mode != "" && mode != termhost.ModeAuto {
		query = map[string]string{queryMode: string(mode)}
	}
	return func() tea.Msg {
		if _, err := lazygit.Locate(env, cfg.Path); err != nil {
			return ui.OpenOverlayMsg{ID: OverlayInstall}
		}
		start, _ := os.Getwd()
		c, cancel := context.WithTimeout(context.Background(), resolveTimeout)
		defer cancel()
		repo, needPick, err := lazygit.ResolveRepo(c, env, cfg.Repo, start)
		if err != nil || needPick {
			return ui.OpenTool(toolID, pickID, query)()
		}
		return ui.OpenTool(toolID, instancePrefix+repo, query)()
	}
}

// newComponent is ToolSpec.New: the picker for the picker id, else the
// instance of one repository.
func newComponent(ctx *ui.Context, r ui.Route) ui.Component {
	if r.ID == pickID {
		return newPicker(ctx, r)
	}
	return newInstance(ctx, r)
}
