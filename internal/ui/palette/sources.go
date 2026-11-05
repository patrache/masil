package palette

import "github.com/patrache/masil/internal/ui"

// ItemKind classifies palette items.
type ItemKind int

// Item kinds.
const (
	ItemChannel ItemKind = iota
	ItemDM
	ItemSpace
	ItemPerson
	ItemCommand
	ItemRoute
)

// Item is one palette row.
type Item struct {
	Kind     ItemKind
	Title    string
	Subtitle string
	Badge    ui.Badge
	Route    *ui.Route
	Command  *Command
	Score    int
}

// Source contributes palette items.
type Source interface {
	Items(ctx *ui.Context, query string) []Item
}

// RegisterSource adds a source (init()-time).
func RegisterSource(s Source) {
	mu.Lock()
	defer mu.Unlock()
	sources = append(sources, s)
}
