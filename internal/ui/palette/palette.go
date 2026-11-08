// Package palette is the command palette: command/source registries and the ctrl+k overlay (CA 5.6).
package palette

import (
	"fmt"
	"slices"
	"sync"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/ui"
)

// Command is a palette command.
type Command struct {
	ID        string // "theme.cycle", "layout.pin.command", "status.set", ...
	Title     string // Korean, e.g. "테마: 레트로 앰버"
	Keywords  []string
	Group     string // "이동" | "명령" | "설정"
	KeyHint   string // "ctrl+w 1"
	Available func(ctx *ui.Context) bool
	Run       func(ctx *ui.Context, args string) tea.Cmd
}

var (
	mu       sync.Mutex
	commands []Command
	sources  []Source
)

// Register adds a command (init()-time); duplicate id => panic.
func Register(c Command) {
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
