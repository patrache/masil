package chat

import (
	"context"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/keymap"
	"github.com/patrache/masil/internal/ui/palette"
	"github.com/patrache/masil/internal/ui/widgets"
)

func init() {
	ui.RegisterOverlay(overlayChannelCreate, newChannelCreate)
	palette.Register(palette.Command{
		ID: "channel.create", Label: func(t *i18n.T) string { return t.Chat.CmdChannelCreate() }, Group: palette.GroupCommand,
		Keywords:   []string{"channel", "create", "new", "private"},
		KeywordsIn: func(t *i18n.T) []string { return palette.Words(t.Chat.KwChannelCreate()) },
		Available:  canCreateChannel,
		Run: func(ctx *ui.Context, args string) tea.Cmd {
			return ui.OpenRegisteredOverlay(overlayChannelCreate, spaceArg(ctx, args))
		},
	})
}

// overlayChannelCreate is the channel creation dialog (args = the space id; "" = the current space).
const overlayChannelCreate = "chat.channel_create"

// Channel creation ids and limits (the texts are chat.toml keys).
const (
	channelRowNewID   = "new"
	tagChannelCreate  = "chat.channel_create:"
	tagPrivateMembers = "chat.channel_create_members:"
	privateMembersMax = 50
)

// validNewChannelName is the channel name rule (api.md 0.3) on the trimmed name,
// as a form Validate speaking c's language.
func validNewChannelName(c *i18n.T) func(string) string {
	return func(s string) string {
		if !channelNameRe.MatchString(strings.TrimSpace(s)) {
			return c.Chat.ChannelNameRule()
		}
		return ""
	}
}

// createChannelCmd is POST /v1/spaces/{space}/channels (tag chat.channel_create:<space>).
func createChannelCmd(ctx *ui.Context, spaceID string, r api.CreateChannelRequest) tea.Cmd {
	return ui.Do(ctx, tagChannelCreate+spaceID, func(c context.Context, cl *api.Client) (*api.Channel, error) {
		return cl.CreateChannel(c, spaceID, r)
	})
}

// channelCreated is the end of a creation (form / picker OnDone): the channel is
// stored, opened and announced.
func channelCreated(ctx *ui.Context, result tea.Msg) (tea.Cmd, ui.Overlay) {
	m, ok := result.(ui.ResultMsg[*api.Channel])
	if !ok || m.Value == nil || ctx == nil || ctx.Store == nil {
		return nil, nil
	}
	ch := *m.Value
	ctx.Store.ObserveChannel(ch)
	title := channelTitle(ctx, &ch, false)
	return tea.Batch(ui.Navigate(ui.Route{Kind: ui.RouteChannel, ID: ch.ID}), ui.Toast(cat(ctx).Chat.ChannelCreated(title), ui.ToastSuccess)), nil
}

// newChannelCreate is the chat.channel_create factory (args = the space): 이름
// (the name rule), 주제 (0..250) and 비공개. A public channel is created at once;
// a private one first picks its members among the space's (none is fine: the
// creator is always in). Empty topic, false privacy and no members are left out
// of the request. nil without a known space.
func newChannelCreate(ctx *ui.Context, args string) ui.Overlay {
	id := knownSpace(ctx, args)
	if id == "" {
		return nil
	}
	var pending api.CreateChannelRequest
	c := cat(ctx)
	return newForm(formSpec{
		Title: c.Chat.CmdChannelCreate(), Submit: c.Chat.CreateSubmit(),
		Fields: []field{
			{Key: "name", Label: c.Chat.NameLabel(), Placeholder: c.Chat.ChannelNameHolder(), Max: channelNameMax, Validate: validNewChannelName(c)},
			{Key: "topic", Label: c.Chat.TopicOptLabel(), Placeholder: c.Chat.TopicHolder(), Max: topicMax, Validate: validTopic(c)},
			{Key: "private", Label: c.Chat.PrivacyLabel(), Kind: fieldToggle, Text: c.Chat.PrivateToggle()},
		},
		OnSubmit: func(ctx *ui.Context, vals map[string]string) (string, tea.Cmd) {
			pending = api.CreateChannelRequest{Name: strings.TrimSpace(vals["name"]), Topic: strings.TrimSpace(vals["topic"]), IsPrivate: vals["private"] == "true"}
			if pending.IsPrivate {
				return "", nil // the member picker comes first (OnDone)
			}
			return tagChannelCreate + id, createChannelCmd(ctx, id, pending)
		},
		OnDone: func(ctx *ui.Context, result tea.Msg) (tea.Cmd, ui.Overlay) {
			if result == nil && pending.IsPrivate {
				return nil, privateMembersLoader(c, id, pending)
			}
			return channelCreated(ctx, result)
		},
	})
}

// privateMembersLoader fetches the space's members, then picks the private
// channel's first members among them (up to 50, none allowed) and creates it.
func privateMembersLoader(c *i18n.T, spaceID string, req api.CreateChannelRequest) ui.Overlay {
	return newLoader(c.Chat.PrivateMembersTitle(), tagPrivateMembers+spaceID, func(c context.Context, cl *api.Client) ([]api.UserSummary, error) {
		ms, err := allPages(func(cursor string) (*api.Page[api.SpaceMember], error) {
			return cl.ListSpaceMembers(c, spaceID, api.SpaceMembersQuery{PageOpts: api.PageOpts{Cursor: cursor, Limit: memberPageSize}})
		})
		users := make([]api.UserSummary, len(ms))
		for i, m := range ms {
			users[i] = m.User
		}
		return users, err
	}, func(_ *ui.Context, users []api.UserSummary) ui.Overlay {
		return newUserPicker(pickerSpec{
			Title: c.Chat.PrivateMembersTitle(), Submit: c.Chat.CreateSubmit(), Max: privateMembersMax, AllowEmpty: true, Empty: c.Chat.PrivateMembersNone(),
			Candidates: func(*ui.Context) []api.UserSummary { return users },
			Exclude:    blockedUser,
			OnPick: func(ctx *ui.Context, picked []api.UserSummary, _ string) (string, tea.Cmd) {
				r := req
				if len(picked) > 0 {
					r.Members = userRefs(picked)
				}
				return tagChannelCreate + spaceID, createChannelCmd(ctx, spaceID, r)
			},
			OnDone: channelCreated,
		})
	})
}

// ---- the channels tab ----

// channelItem is one channels-tab row: ＋ 새 채널 or a channel.
type channelItem struct {
	id    string // channelRowNewID or the channel id
	label string
}

// channelTab is the 채널 tab (owner, admin): ＋ 새 채널, then the space's text
// channels by position ("# 이름 · 주제", private "🔒이름"), read from the Store.
// n or enter on ＋ 새 채널 creates one, enter / m on a channel opens its settings.
type channelTab struct {
	v    *spaceSettingsView
	list *widgets.List[channelItem]
	rows []channelItem
}

func newSpaceChannels(v *spaceSettingsView) *channelTab {
	b := &channelTab{v: v}
	b.list = widgets.NewList(func(it channelItem) string { return it.id }, b.render)
	return b
}

func (b *channelTab) sync(ctx *ui.Context) {
	rows := []channelItem{{id: channelRowNewID, label: cat(ctx).Chat.ChannelRowNew()}}
	if ctx != nil && ctx.Store != nil {
		for _, ch := range ctx.Store.SpaceChannels(b.v.spaceID) {
			rows = append(rows, channelItem{id: ch.ID, label: channelTitle(ctx, ch, true)})
		}
	}
	b.rows = rows
	b.list.SetItems(rows)
}

// open runs the row: ＋ 새 채널 opens the creation dialog, a channel its settings menu.
func (b *channelTab) open(ctx *ui.Context, it channelItem) tea.Cmd {
	var o ui.Overlay
	if it.id == channelRowNewID {
		o = newChannelCreate(ctx, b.v.spaceID)
	} else {
		o = newChannelPrefs(ctx, it.id)
	}
	if o == nil {
		return nil
	}
	return ui.OpenOverlay(o)
}

func (b *channelTab) key(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	b.sync(ctx)
	if msg.String() == "n" {
		return true, b.open(ctx, channelItem{id: channelRowNewID})
	}
	if ctx.Keys.Matches(msg, keymap.ListOpen) || ctx.Keys.Matches(msg, keymap.MsgActions) {
		it, ok := b.list.Selected()
		if !ok {
			return false, nil
		}
		return true, b.open(ctx, it)
	}
	return b.list.HandleKey(ctx, msg)
}

// mouse: a click selects a row, a click on the selected row (or a right click) opens it.
func (b *channelTab) mouse(ctx *ui.Context, msg tea.MouseMsg, local ui.Point) tea.Cmd {
	b.sync(ctx)
	click, ok := msg.(tea.MouseClickMsg)
	if !ok {
		return b.list.HandleMouse(ctx, msg, local)
	}
	top := slices.IndexFunc(b.rows, func(it channelItem) bool { return it.id == b.list.Anchor().ItemID })
	i := max(top, 0) + local.Y
	if local.Y < 0 || i >= len(b.rows) {
		return nil
	}
	it := b.rows[i]
	if cur, ok := b.list.Selected(); ok && cur.id == it.id || click.Button == tea.MouseRight {
		b.list.Select(it.id)
		return b.open(ctx, it)
	}
	if click.Button == tea.MouseLeft {
		b.list.Select(it.id)
	}
	return nil
}

func (b *channelTab) render(ctx *ui.Context, it channelItem, selected bool, width int) []string {
	return []string{settingsRow(ctx, b.v.focused, selected, false, it.label, width)}
}

// view draws the rows in exactly size.
func (b *channelTab) view(ctx *ui.Context, _ *theme.Theme, size ui.Size) string {
	b.sync(ctx)
	return b.list.View(ctx, size)
}
