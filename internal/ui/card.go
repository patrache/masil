package ui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
)

// Card types (api.md 2 CardType; C1.2). Each tool registers its renderer.
const (
	CardLeetDaily  = "leet.daily"
	CardLeetSolve  = "leet.solve"
	CardArchDesign = "arch.design"
	CardArchSim    = "arch.sim"
	CardAPIResult  = "api.result"
	CardAPIRun     = "api.run"
)

// CardRenderer draws and drives a kind "card" message of one type in chat.
// A type without a renderer (an older client, the tool's flag off) is drawn
// from Message.Body like a text message.
//
// The renderer decides on a compact drawing from ctx.Mode (layout.ModeWidget,
// ModeTiny) and width — there is no separate argument; it never reads the
// layout mode for anything else.
type CardRenderer struct {
	// Lines draws the card body: every line at most width cells, styled; strings
	// taken from Card.Data go through textwidth.Sanitize. Required.
	Lines func(ctx *Context, m *api.Message, width int) []string
	// Open runs on enter (list.open) on the selected card; nil = enter opens the
	// thread as for any message.
	Open func(ctx *Context, m *api.Message) tea.Cmd
	// Key sees keys first while the card is selected (P1 `b` ranking, spoiler
	// toggles); handled=false (or nil Key) leaves the key to chat.
	Key func(ctx *Context, m *api.Message, k tea.KeyPressMsg) (handled bool, cmd tea.Cmd)
}

// RegisterCard registers the renderer of cardType; call it from init().
// Empty type, nil Lines or a duplicate => panic.
func RegisterCard(cardType string, r CardRenderer) {
	if cardType == "" || r.Lines == nil {
		panic(fmt.Sprintf("ui: RegisterCard: empty card type or nil Lines (%q)", cardType))
	}
	regMu.Lock()
	defer regMu.Unlock()
	if _, dup := reg.Cards[cardType]; dup {
		panic(fmt.Sprintf("ui: RegisterCard: duplicate card type %q", cardType))
	}
	reg.Cards[cardType] = r
}

// CardFor is the renderer registered for cardType.
func CardFor(cardType string) (CardRenderer, bool) {
	regMu.Lock()
	defer regMu.Unlock()
	r, ok := reg.Cards[cardType]
	return r, ok
}

// MessageCard is the renderer of m when m is a card message of a registered
// type (nil m, another kind or an unknown type => false).
func MessageCard(m *api.Message) (CardRenderer, bool) {
	if m == nil || m.Kind != api.MessageKindCard || m.Card == nil {
		return CardRenderer{}, false
	}
	return CardFor(m.Card.Type)
}
