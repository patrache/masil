package chat

import (
	"context"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/palette"
)

func init() {
	ui.RegisterOverlay(overlaySpaceCreate, newSpaceCreate)
	ui.RegisterOverlay(overlaySpaceJoin, newSpaceJoin)
	palette.Register(palette.Command{
		ID: "space.create", Label: func(t *i18n.T) string { return t.Chat.CmdSpaceCreate() }, Group: palette.GroupCommand,
		Keywords:   []string{"space", "create", "new"},
		KeywordsIn: func(t *i18n.T) []string { return palette.Words(t.Chat.KwSpaceCreate()) },
		Run:        func(_ *ui.Context, args string) tea.Cmd { return ui.OpenRegisteredOverlay(overlaySpaceCreate, args) },
	})
	palette.Register(palette.Command{
		ID: "space.join", Label: func(t *i18n.T) string { return t.Chat.CmdSpaceJoin() }, Group: palette.GroupCommand,
		Keywords:   []string{"space", "join", "invite", "code"},
		KeywordsIn: func(t *i18n.T) []string { return palette.Words(t.Chat.KwSpaceJoin()) },
		Run:        func(_ *ui.Context, args string) tea.Cmd { return ui.OpenRegisteredOverlay(overlaySpaceJoin, args) },
	})
}

// Registered space dialog ids.
const (
	overlaySpaceCreate = "chat.space_create"
	overlaySpaceJoin   = "chat.space_join"
)

// Space dialog limits (api.md 0.3: name 1..50, description 0..200); the texts
// are chat.toml keys.
const (
	spaceNameMax   = 50
	spaceDescMax   = 200
	inviteCodeLen  = 8
	inviteInputMax = 64
	tagSpaceCreate = "chat.space_create:new"
)

// spaceIcons are the emoji choices of space.create (spaceIconChoices puts "없음" first).
var spaceIcons = []string{"🚀", "🍚", "🎮", "🎧", "💼", "🐱", "🌱", "☕", "📚"}

// spaceIconChoices are the icon choices in c's language: 없음 (no icon), then spaceIcons.
func spaceIconChoices(c *i18n.T) []string {
	return append([]string{c.Chat.IconNone()}, spaceIcons...)
}

// newSpaceCreate is the space.create form: 이름 (required), 설명, 아이콘. Success
// adopts the space, selects it on the rail and opens its default channel; an error
// stays in the form.
func newSpaceCreate(ctx *ui.Context, args string) ui.Overlay {
	c := cat(ctx)
	none := c.Chat.IconNone()
	return newForm(formSpec{
		Title: c.Chat.CmdSpaceCreate(), Submit: c.Chat.CreateSubmit(),
		Fields: []field{
			{Key: "name", Label: c.Chat.SpaceNameLabel(), Placeholder: c.Chat.SpaceNameHolder(), Value: args, Max: spaceNameMax, Validate: validSpaceName(c)},
			{Key: "description", Label: c.Chat.SpaceDescLabel(), Placeholder: c.Chat.SpaceDescHolder(), Max: spaceDescMax, Validate: validSpaceDesc(c)},
			{Key: "icon", Label: c.Chat.SpaceIconLabel(), Kind: fieldChoice, Choices: spaceIconChoices(c)},
		},
		OnSubmit: func(ctx *ui.Context, vals map[string]string) (string, tea.Cmd) {
			req := api.CreateSpaceRequest{Name: strings.TrimSpace(vals["name"]), Description: strings.TrimSpace(vals["description"])}
			if icon := vals["icon"]; icon != none {
				req.IconEmoji = icon
			}
			return tagSpaceCreate, ui.Do(ctx, tagSpaceCreate, func(c context.Context, cl *api.Client) (*api.SpaceDetail, error) {
				return cl.CreateSpace(c, req)
			})
		},
		OnDone: func(ctx *ui.Context, result tea.Msg) (tea.Cmd, ui.Overlay) {
			m, ok := result.(ui.ResultMsg[*api.SpaceDetail])
			if !ok || m.Value == nil {
				return nil, nil
			}
			return enterSpace(ctx, m.Value), nil
		},
	})
}

// validSpaceName is the space name Validate (1..50 after trimming) in c's language.
func validSpaceName(c *i18n.T) func(string) string {
	return func(v string) string {
		switch n := utf8.RuneCountInString(strings.TrimSpace(v)); {
		case n == 0:
			return c.Chat.SpaceNameRequired()
		case n > spaceNameMax:
			return c.Chat.SpaceNameTooLong()
		}
		return ""
	}
}

// validSpaceDesc is the space description Validate (0..200) in c's language.
func validSpaceDesc(c *i18n.T) func(string) string {
	return func(v string) string {
		if utf8.RuneCountInString(strings.TrimSpace(v)) > spaceDescMax {
			return c.Chat.SpaceDescTooLong()
		}
		return ""
	}
}

// newSpaceJoin is the space.join form: one 초대 코드 field (args prefill it). It
// calls no API: a valid code opens its preview (RouteInvite{ID: code}).
func newSpaceJoin(ctx *ui.Context, args string) ui.Overlay {
	c := cat(ctx)
	return newForm(formSpec{
		Title: c.Chat.SpaceJoinTitle(), Submit: c.Chat.SpaceJoinSubmit(),
		Fields: []field{{
			Key: "code", Label: c.Chat.InviteCodeLabel(), Placeholder: c.Chat.InviteCodeHolder(), Value: args, Max: inviteInputMax,
			Validate: func(v string) string {
				if strings.TrimSpace(v) == "" {
					return c.Chat.InviteCodeRequired()
				}
				if _, ok := parseInviteCode(v); !ok {
					return c.Chat.InviteCodeBad()
				}
				return ""
			},
		}},
		OnSubmit: func(_ *ui.Context, vals map[string]string) (string, tea.Cmd) {
			code, _ := parseInviteCode(vals["code"])
			return "", ui.Navigate(ui.Route{Kind: ui.RouteInvite, ID: code})
		},
	})
}

// parseInviteCode accepts "AB12CD34" or the share line "masil join AB12CD34"
// (any surrounding or repeated white space): the code is exactly 8 ASCII letters
// or digits, its case kept.
func parseInviteCode(s string) (string, bool) {
	f := strings.Fields(s)
	switch {
	case len(f) == 3 && f[0] == "masil" && f[1] == "join":
		f = f[2:]
	case len(f) != 1:
		return "", false
	}
	code := f[0]
	if len(code) != inviteCodeLen {
		return "", false
	}
	for i := range len(code) {
		c := code[i]
		if !('0' <= c && c <= '9' || 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z') {
			return "", false
		}
	}
	return code, true
}

// enterSpace stores a space I just created or joined and opens it: rail
// selection + its default channel (else its first channel); nil when it has none.
func enterSpace(ctx *ui.Context, d *api.SpaceDetail) tea.Cmd {
	if ctx == nil || ctx.Store == nil || d == nil {
		return nil
	}
	adoptSpace(ctx.Store, d)
	if cmd := goSpace(ctx, d.ID); cmd != nil {
		return cmd
	}
	if d.DefaultChannelID != "" {
		return ui.Navigate(ui.Route{Kind: ui.RouteChannel, ID: d.DefaultChannelID})
	}
	return nil
}
