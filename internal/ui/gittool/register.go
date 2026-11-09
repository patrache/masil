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
		Available:   func(ctx *ui.Context) bool { return probeNow(ctx).err == nil },
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

// ---- environment (tests swap it: export_test.go) ----

var (
	envMu  sync.Mutex
	envCur = lazygit.SystemEnv()
	probe  probeResult
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
	err     error  // lazygit.ErrNotInstalled
	done    bool
}

// probeNow finds lazygit and its version, reusing the last answer for
// probeTTL (the hub draws often; "--version" runs only when the binary
// changed).
func probeNow(ctx *ui.Context) probeResult {
	cfg := toolConfig(ctx)
	now := time.Now()
	if ctx != nil && ctx.Now != nil {
		now = ctx.Now()
	}
	envMu.Lock()
	p := probe
	env := envCur
	envMu.Unlock()
	if p.done && p.cfgPath == cfg.Path && now.Sub(p.at) < probeTTL && now.Sub(p.at) >= 0 {
		return p
	}
	bin, err := lazygit.Locate(env, cfg.Path)
	next := probeResult{cfgPath: cfg.Path, at: now, bin: bin, err: err, done: true}
	if err == nil {
		if p.done && p.bin == bin && p.version != "" {
			next.version = p.version
		} else {
			c, cancel := context.WithTimeout(context.Background(), versionTimeout)
			next.version, _ = lazygit.Version(c, env, bin)
			cancel()
		}
	}
	envMu.Lock()
	probe = next
	envMu.Unlock()
	return next
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

// hubStatus is the hub row's status (A8.2).
func hubStatus(ctx *ui.Context) string {
	t := ctx.T
	p := probeNow(ctx)
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
