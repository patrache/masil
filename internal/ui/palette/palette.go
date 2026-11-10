// Package palette is the command palette: command/source registries and the ctrl+k overlay (CA 5.6).
package palette

import (
	"fmt"
	"slices"
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/ui"
)

// Palette groups (Command.Group). The header text comes from the catalog
// (common.palette_group_*): ko "이동", "명령", "설정".
const (
	GroupNav      = "nav"
	GroupCommand  = "command"
	GroupSettings = "settings"
)

// Command is a palette command.
type Command struct {
	ID    string // "theme.cycle", "layout.pin.command", "status.set", ...
	Title string // 0.1.2 rows only (Korean, e.g. "테마: 레트로 앰버"); ignored when Label is set
	// Label is the title in the screen's language, called at draw time with
	// ctx.T. Every 0.1.3 command registers with Label, never Title.
	Label    func(t *i18n.T) string
	Keywords []string
	// KeywordsIn are extra search words in t's language (called with the
	// screen's catalog at search time), e.g. palette.Words(t.Chat.FriendsKeywords()).
	// Keywords stay the language-independent (ASCII) words.
	KeywordsIn func(t *i18n.T) []string
	Group      string // GroupNav | GroupCommand | GroupSettings ("" = GroupCommand)
	KeyHint    string // "ctrl+w 1"
	Available  func(ctx *ui.Context) bool
	Run        func(ctx *ui.Context, args string) tea.Cmd
	Badge      func(ctx *ui.Context) ui.Badge // drawn on the item (nil = none), e.g. the 알림함's unread count
}

var (
	mu       sync.Mutex
	commands []Command
	sources  []Source
)

// Register adds a command (init()-time); duplicate id or unknown Group => panic.
func Register(c Command) {
	switch c.Group {
	case "", GroupNav, GroupCommand, GroupSettings:
	default:
		panic(fmt.Sprintf("palette: command %q: unknown group %q (use GroupNav, GroupCommand or GroupSettings)", c.ID, c.Group))
	}
	mu.Lock()
	defer mu.Unlock()
	for _, have := range commands {
		if have.ID == c.ID {
			panic(fmt.Sprintf("palette: duplicate command id %q", c.ID))
		}
	}
	commands = append(commands, cloneCommand(c))
}

// Lookup returns a copy of the registered command with that id (CR-B).
func Lookup(id string) (Command, bool) {
	mu.Lock()
	defer mu.Unlock()
	for _, c := range commands {
		if c.ID == id {
			return cloneCommand(c), true
		}
	}
	return Command{}, false
}

// TitleIn is c's title in t's language: Label(t), or the 0.1.2 Title.
func (c Command) TitleIn(t *i18n.T) string {
	if c.Label == nil {
		return c.Title
	}
	if t == nil {
		t = i18n.KoT()
	}
	return c.Label(t)
}

// GroupTitle is the header text of group g in t's language (nil = ko).
func GroupTitle(t *i18n.T, g string) string {
	if t == nil {
		t = i18n.KoT()
	}
	switch g {
	case GroupNav:
		return t.Common.PaletteGroupNav()
	case GroupSettings:
		return t.Common.PaletteGroupSettings()
	default:
		return t.Common.PaletteGroupCommand()
	}
}

// Words splits a catalog keyword list "a, b c, d" on commas (keywords may hold
// spaces), dropping blanks.
func Words(s string) []string {
	var out []string
	for _, w := range strings.Split(s, ",") {
		if w = strings.TrimSpace(w); w != "" {
			out = append(out, w)
		}
	}
	return out
}

func cloneCommand(c Command) Command {
	c.Keywords = slices.Clone(c.Keywords)
	return c
}

// ResetForTest empties the command and source registries. Tests only; call
// t.Cleanup(palette.SnapshotForTest()) first.
func ResetForTest() {
	mu.Lock()
	defer mu.Unlock()
	commands, sources = nil, nil
}

// SnapshotForTest copies the current registries (and the recently chosen routes) and
// returns a func that restores them. Tests only.
func SnapshotForTest() (restore func()) {
	mu.Lock()
	savedCmds := cloneCommands(commands)
	savedSrcs := slices.Clone(sources)
	mu.Unlock()
	recentMu.Lock()
	savedRecent := slices.Clone(recent)
	recentMu.Unlock()
	return func() {
		mu.Lock()
		commands = cloneCommands(savedCmds)
		sources = slices.Clone(savedSrcs)
		mu.Unlock()
		recentMu.Lock()
		recent = slices.Clone(savedRecent)
		recentMu.Unlock()
	}
}

// ItemsForTest returns the ranked items for query (built-in and registered sources,
// no people-search results), before grouping. Tests only (any package's).
func ItemsForTest(ctx *ui.Context, query string) []Item {
	return collect(ctx, query, nil, "")
}

func cloneCommands(cs []Command) []Command {
	if cs == nil {
		return nil
	}
	out := make([]Command, len(cs))
	for i, c := range cs {
		out[i] = cloneCommand(c)
	}
	return out
}
