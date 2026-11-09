package chat

import (
	"context"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/palette"
)

func init() {
	ui.RegisterOverlay(overlaySpaceCreate, newSpaceCreate)
	ui.RegisterOverlay(overlaySpaceJoin, newSpaceJoin)
	palette.Register(palette.Command{
		ID: "space.create", Title: "방 만들기", Keywords: []string{"space", "create", "new", "방", "새 방", "만들기"}, Group: palette.GroupCommand,
		Run: func(_ *ui.Context, args string) tea.Cmd { return ui.OpenRegisteredOverlay(overlaySpaceCreate, args) },
	})
	palette.Register(palette.Command{
		ID: "space.join", Title: "초대 코드로 방 참여", Keywords: []string{"space", "join", "invite", "code", "참여", "초대 코드", "입장"}, Group: palette.GroupCommand,
		Run: func(_ *ui.Context, args string) tea.Cmd { return ui.OpenRegisteredOverlay(overlaySpaceJoin, args) },
	})
}

// Registered space dialog ids.
const (
	overlaySpaceCreate = "chat.space_create"
	overlaySpaceJoin   = "chat.space_join"
)

// Space dialog texts and limits (api.md 0.3: name 1..50, description 0..200).
const (
	spaceCreateTitle   = "방 만들기"
	spaceCreateSubmit  = "만들기"
	spaceJoinTitle     = "방 참여"
	spaceJoinSubmit    = "미리 보기"
	spaceNameMax       = 50
	spaceDescMax       = 200
	inviteCodeLen      = 8
	inviteInputMax     = 64
	spaceNameRequired  = "이름을 입력해 주세요"
	spaceNameTooLong   = "이름은 50자까지예요"
	spaceDescTooLong   = "설명은 200자까지예요"
	inviteCodeRequired = "초대 코드를 입력해 주세요"
	inviteCodeBad      = "초대 코드는 8자예요"
	iconNone           = "없음"
	tagSpaceCreate     = "chat.space_create:new"
)

// spaceIcons are the icon choices of space.create ("없음" = no icon).
var spaceIcons = []string{iconNone, "🚀", "🍚", "🎮", "🎧", "💼", "🐱", "🌱", "☕", "📚"}

// newSpaceCreate is the space.create form: 이름 (required), 설명, 아이콘. Success
// adopts the space, selects it on the rail and opens its default channel; an error
// stays in the form.
func newSpaceCreate(_ *ui.Context, args string) ui.Overlay {
	return newForm(formSpec{
		Title: spaceCreateTitle, Submit: spaceCreateSubmit,
		Fields: []field{
			{Key: "name", Label: "이름", Placeholder: "예: 노바 개발방", Value: args, Max: spaceNameMax, Validate: validSpaceName},
			{Key: "description", Label: "설명 (선택)", Placeholder: "어떤 방인가요?", Max: spaceDescMax, Validate: validSpaceDesc},
			{Key: "icon", Label: "아이콘", Kind: fieldChoice, Choices: spaceIcons},
		},
		OnSubmit: func(ctx *ui.Context, vals map[string]string) (string, tea.Cmd) {
			req := api.CreateSpaceRequest{Name: strings.TrimSpace(vals["name"]), Description: strings.TrimSpace(vals["description"])}
			if icon := vals["icon"]; icon != iconNone {
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

func validSpaceName(v string) string {
	switch n := utf8.RuneCountInString(strings.TrimSpace(v)); {
	case n == 0:
		return spaceNameRequired
	case n > spaceNameMax:
		return spaceNameTooLong
	}
	return ""
}

func validSpaceDesc(v string) string {
	if utf8.RuneCountInString(strings.TrimSpace(v)) > spaceDescMax {
		return spaceDescTooLong
	}
	return ""
}

// newSpaceJoin is the space.join form: one 초대 코드 field (args prefill it). It
// calls no API: a valid code opens its preview (RouteInvite{ID: code}).
func newSpaceJoin(_ *ui.Context, args string) ui.Overlay {
	return newForm(formSpec{
		Title: spaceJoinTitle, Submit: spaceJoinSubmit,
		Fields: []field{{
			Key: "code", Label: "초대 코드", Placeholder: "AB12CD34 또는 masil join AB12CD34", Value: args, Max: inviteInputMax,
			Validate: func(v string) string {
				if strings.TrimSpace(v) == "" {
					return inviteCodeRequired
				}
				if _, ok := parseInviteCode(v); !ok {
					return inviteCodeBad
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
