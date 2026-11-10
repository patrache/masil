package chat

import (
	"context"
	"encoding/json"
	"regexp"
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/errtext"
	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/realtime"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/palette"
	"github.com/patrache/masil/internal/ui/widgets"
)

func init() {
	ui.RegisterOverlay(overlayChannelPrefs, newChannelPrefs)
	palette.Register(palette.Command{
		ID: "channel.prefs", Label: func(t *i18n.T) string { return t.Chat.CmdChannelPrefs() }, Group: palette.GroupCommand,
		Keywords:   []string{"channel", "settings", "notify", "mute"},
		KeywordsIn: func(t *i18n.T) []string { return palette.Words(t.Chat.KwChannelPrefs()) },
		Available:  func(ctx *ui.Context) bool { return viewedChannel(ctx) != nil },
		Run:        func(_ *ui.Context, args string) tea.Cmd { return ui.OpenRegisteredOverlay(overlayChannelPrefs, args) },
	})
}

// Registered overlay ids.
const (
	overlayChannelPrefs = "chat.channel_prefs" // args = channel id ("" = the viewed channel)
	overlayPins         = "chat.pins"          // pins.go; args = channel id
)

// Channel settings marks and limits (the texts are chat.toml keys).
const (
	checkHint = "✓"

	channelNameMax = 32
	topicMax       = 250
	groupNameMax   = 50
	memberPages    = 10  // member list pages fetched for a picker / submenu
	memberPageSize = 100 // api.md 0: list limit max
)

// channelNameRe is the channel name rule (api.md 0.3): lowercase ASCII letters, digits, Hangul syllables
// (U+AC00–U+D7A3), CJK extension A (U+3400–U+4DBF), CJK unified ideographs (U+4E00–U+9FFF), _ and -, 1..32 code
// points — the server's CHANNEL_NAME_RE (TestChannelNameRuleMatchesServer reads the same table).
var channelNameRe = regexp.MustCompile(`^[a-z0-9\x{AC00}-\x{D7A3}\x{3400}-\x{4DBF}\x{4E00}-\x{9FFF}_-]{1,32}$`)

// validChannelName is a form Validate for channel names, in c's language.
func validChannelName(c *i18n.T) func(string) string {
	return func(s string) string {
		if !channelNameRe.MatchString(s) {
			return c.Chat.BadChannelName()
		}
		return ""
	}
}

// validTopic is a form Validate for channel topics (0..250 code points), in c's language.
func validTopic(c *i18n.T) func(string) string {
	return func(s string) string {
		if len([]rune(s)) > topicMax {
			return c.Chat.TopicTooLong()
		}
		return ""
	}
}

// newChannelPrefs is the chat.channel_prefs factory: the settings menu of the
// channel args (the viewed channel when empty); nil when there is none.
func newChannelPrefs(ctx *ui.Context, args string) ui.Overlay {
	if ctx == nil || ctx.Store == nil {
		return nil
	}
	var ch *api.Channel
	if id := strings.TrimSpace(args); id != "" {
		ch = ctx.Store.Channels[id]
	} else {
		ch = viewedChannel(ctx)
	}
	if ch == nil {
		return nil
	}
	name := channelTitle(ctx, ch, false)
	title := cat(ctx).Chat.PrefsTitleChannel(name)
	if ch.Kind != api.ChannelText {
		title = cat(ctx).Chat.PrefsTitleDm(name)
	}
	return newMenu(title, channelPrefItems(ctx, ch))
}

// liveChannel is the Store's current copy of channel id, else fallback.
func liveChannel(ctx *ui.Context, id string, fallback *api.Channel) *api.Channel {
	if ctx != nil && ctx.Store != nil {
		if ch := ctx.Store.Channels[id]; ch != nil {
			return ch
		}
	}
	return fallback
}

// channelPrefItems is the settings menu of ch. Every channel: 알림: 모두/멘션만/끄기
// (the current one checked), 음소거 (해제), 고정 메시지 보기. A group DM adds 이름
// 바꾸기, 멤버 추가 (disabled when full), 멤버 내보내기 (its creator only) and 나가기.
// A text channel adds, for the space owner/admin, 이름·주제 바꾸기 and 채널 삭제
// (disabled on the default channel); a private one also 멤버 추가 (any member: I
// see it, so I am one) and, for the owner/admin, 멤버 내보내기.
func channelPrefItems(ctx *ui.Context, ch *api.Channel) []menuItem {
	if ch == nil {
		return nil
	}
	id := ch.ID
	notify := ch.My.Notify
	if notify == "" {
		notify = "all"
	}
	c := cat(ctx)
	var items []menuItem
	for _, lv := range []struct{ value, label string }{
		{"all", c.Chat.NotifyAll()}, {"mentions", c.Chat.NotifyMentions()}, {"none", c.Chat.NotifyNone()},
	} {
		it := menuItem{Label: c.Chat.ItemNotify(lv.label), Run: func(ctx *ui.Context) (tea.Cmd, ui.Overlay) {
			return setPrefsCmd(ctx, id, prefsChange{notify: &lv.value, label: lv.label}), nil
		}}
		if lv.value == notify {
			it.Hint = checkHint
		}
		items = append(items, it)
	}
	muted := !ch.My.Muted
	muteLabel := c.Chat.ItemMute()
	if ch.My.Muted {
		muteLabel = c.Chat.ItemUnmute()
	}
	items = append(items,
		menuItem{Label: muteLabel, Run: func(ctx *ui.Context) (tea.Cmd, ui.Overlay) {
			return setPrefsCmd(ctx, id, prefsChange{muted: &muted}), nil
		}},
		menuItem{Label: c.Chat.CmdPins(), Run: openRegistered(overlayPins, id)},
	)
	switch ch.Kind {
	case api.ChannelGroupDM:
		items = append(items, groupDMItems(ctx, ch)...)
	case api.ChannelText:
		items = append(items, textChannelItems(ctx, ch)...)
	}
	return items
}

// groupDMItems: 이름 바꾸기, 멤버 추가, 멤버 내보내기 (creator), 나가기.
func groupDMItems(ctx *ui.Context, ch *api.Channel) []menuItem {
	id, snap := ch.ID, *ch
	c := cat(ctx)
	items := []menuItem{{Label: c.Chat.ItemRename(), Run: func(ctx *ui.Context) (tea.Cmd, ui.Overlay) {
		return nil, renameGroupForm(cat(ctx), liveChannel(ctx, id, &snap))
	}}}
	add := menuItem{Label: c.Chat.ItemAddMembers(), Run: func(ctx *ui.Context) (tea.Cmd, ui.Overlay) {
		return nil, groupAddPicker(ctx, liveChannel(ctx, id, &snap))
	}}
	if groupSize(ch) >= groupDMLimit {
		add.Disabled, add.Hint = true, c.Chat.GroupFullHint()
	}
	items = append(items, add)
	if ch.OwnerID != nil && isMe(ctx, *ch.OwnerID) {
		items = append(items, menuItem{Label: c.Chat.ItemKick(), Run: func(ctx *ui.Context) (tea.Cmd, ui.Overlay) {
			cur := liveChannel(ctx, id, &snap)
			return nil, kickMenu(ctx, cur, cur.Members)
		}})
	}
	return append(items, menuItem{Label: c.Chat.ItemLeave(), Danger: true, Run: func(ctx *ui.Context) (tea.Cmd, ui.Overlay) {
		cur := liveChannel(ctx, id, &snap)
		label := textwidth.Sanitize(ctx.Store.ChannelLabel(id))
		c := cat(ctx)
		return nil, widgets.Confirm(c.Chat.LeaveAsk(label), c.Chat.LeaveBody(), c.Chat.ItemLeave(), c.Chat.Cancel(), leaveChannelCmd(ctx, cur))
	}})
}

// groupSize is the member count of a group DM (me included).
func groupSize(ch *api.Channel) int { return max(len(ch.Members), ch.MemberCount) }

// textChannelItems: owner/admin 이름·주제 바꾸기 and 채널 삭제; private 멤버 추가
// (+ owner/admin 멤버 내보내기).
func textChannelItems(ctx *ui.Context, ch *api.Channel) []menuItem {
	id, snap := ch.ID, *ch
	manage := ch.SpaceID != nil && canManageSpace(ctx, *ch.SpaceID)
	c := cat(ctx)
	var items []menuItem
	if manage {
		del := menuItem{Label: c.Chat.ItemDeleteChannel(), Danger: true, Run: func(ctx *ui.Context) (tea.Cmd, ui.Overlay) {
			cur := liveChannel(ctx, id, &snap)
			c := cat(ctx)
			return nil, widgets.Confirm(c.Chat.DeleteChannelAsk(channelTitle(ctx, cur, false)), c.Chat.DeleteChannelBody(), c.Chat.DeleteYes(), c.Chat.Cancel(), deleteChannelCmd(ctx, cur))
		}}
		if sd := spaceOf(ctx, ch); sd != nil && sd.DefaultChannelID == id {
			del.Disabled, del.Hint = true, c.Chat.DefaultChanHint()
		}
		items = append(items, menuItem{Label: c.Chat.ItemEditChannel(), Run: func(ctx *ui.Context) (tea.Cmd, ui.Overlay) {
			return nil, editChannelForm(cat(ctx), liveChannel(ctx, id, &snap))
		}}, del)
	}
	if ch.IsPrivate && ch.SpaceID != nil {
		space := *ch.SpaceID
		items = append(items, menuItem{Label: c.Chat.ItemAddMembers(), Run: func(ctx *ui.Context) (tea.Cmd, ui.Overlay) {
			return nil, privateAddLoader(cat(ctx), id, space)
		}})
		if manage {
			items = append(items, menuItem{Label: c.Chat.ItemKick(), Run: func(ctx *ui.Context) (tea.Cmd, ui.Overlay) {
				return nil, privateKickLoader(cat(ctx), liveChannel(ctx, id, &snap))
			}})
		}
	}
	return items
}

// ---- prefs ----

// prefsChange is one PUT /v1/channels/{id}/prefs change (label = the notify level word).
type prefsChange struct {
	notify *string
	muted  *bool
	label  string
}

// setPrefsCmd applies the change to the Store at once and sends it; a failure
// puts the old value back (when nothing changed it since) and toasts.
func setPrefsCmd(ctx *ui.Context, id string, p prefsChange) tea.Cmd {
	if ctx == nil || ctx.Store == nil {
		return nil
	}
	ch := ctx.Store.Channels[id]
	if ch == nil {
		return nil
	}
	prev := ch.My
	var req api.ChannelPrefsRequest
	if p.notify != nil {
		ch.My.Notify = *p.notify
		req.Notify = api.Some(*p.notify)
	}
	if p.muted != nil {
		ch.My.Muted = *p.muted
		req.Muted = api.Some(*p.muted)
	}
	return doAction(ctx, "chat.channel_prefs:"+id, func(c context.Context, cl *api.Client) (*api.ChannelMyState, error) {
		return cl.SetChannelPrefs(c, id, req)
	}, func(ctx *ui.Context, my *api.ChannelMyState, err error) tea.Cmd {
		cur := liveChannel(ctx, id, nil)
		if err != nil {
			if cur != nil {
				if p.notify != nil && cur.My.Notify == *p.notify {
					cur.My.Notify = prev.Notify
				}
				if p.muted != nil && cur.My.Muted == *p.muted {
					cur.My.Muted = prev.Muted
				}
			}
			return errorCmd(err, cat(ctx).Chat.PrefsFailed())
		}
		if cur != nil && my != nil {
			cur.My.Notify, cur.My.Muted = my.Notify, my.Muted
		}
		switch {
		case p.muted != nil && *p.muted:
			return ui.Toast(cat(ctx).Chat.Muted(), ui.ToastSuccess)
		case p.muted != nil:
			return ui.Toast(cat(ctx).Chat.Unmuted(), ui.ToastSuccess)
		}
		return ui.Toast(cat(ctx).Chat.NotifySet(p.label), ui.ToastSuccess)
	})
}

// ---- rename / edit ----

// updateChannelDo is PATCH /v1/channels/{id} under tag.
func updateChannelDo(ctx *ui.Context, tag, id string, r api.UpdateChannelRequest) tea.Cmd {
	return ui.Do(ctx, tag, func(c context.Context, cl *api.Client) (*api.Channel, error) {
		return cl.UpdateChannel(c, id, r)
	})
}

// channelResultDone is a form / picker OnDone observing the returned channel and toasting text.
func channelResultDone(text string) func(ctx *ui.Context, result tea.Msg) (tea.Cmd, ui.Overlay) {
	return func(ctx *ui.Context, result tea.Msg) (tea.Cmd, ui.Overlay) {
		if m, ok := result.(ui.ResultMsg[*api.Channel]); ok && m.Value != nil && ctx.Store != nil {
			ctx.Store.ObserveChannel(*m.Value)
		}
		return ui.Toast(text, ui.ToastSuccess), nil
	}
}

// renameGroupForm renames a group DM (0..50; empty = the member names show).
func renameGroupForm(c *i18n.T, ch *api.Channel) ui.Overlay {
	id, name := ch.ID, ""
	if ch.Name != nil {
		name = *ch.Name
	}
	return newForm(formSpec{
		Title: c.Chat.RenameTitle(), Submit: c.Chat.ChangeSubmit(),
		Fields: []field{{Key: "name", Label: c.Chat.RenameLabel(), Placeholder: c.Chat.RenameHolder(), Value: name, Max: groupNameMax}},
		OnSubmit: func(ctx *ui.Context, vals map[string]string) (string, tea.Cmd) {
			tag := "chat.channel_update:" + id
			return tag, updateChannelDo(ctx, tag, id, api.UpdateChannelRequest{Name: api.Some(strings.TrimSpace(vals["name"]))})
		},
		OnDone: channelResultDone(c.Chat.Renamed()),
	})
}

// editChannelForm edits a text channel's name (the name rule) and topic (0..250).
func editChannelForm(c *i18n.T, ch *api.Channel) ui.Overlay {
	id, name := ch.ID, ""
	if ch.Name != nil {
		name = *ch.Name
	}
	validName := validChannelName(c)
	return newForm(formSpec{
		Title: c.Chat.EditChannelTitle(), Submit: c.Chat.ChangeSubmit(),
		Fields: []field{
			{Key: "name", Label: c.Chat.NameLabel(), Value: name, Max: channelNameMax, Validate: func(s string) string { return validName(strings.TrimSpace(s)) }},
			{Key: "topic", Label: c.Chat.TopicLabel(), Placeholder: c.Chat.TopicHolder(), Value: ch.Topic, Max: topicMax, Validate: validTopic(c)},
		},
		OnSubmit: func(ctx *ui.Context, vals map[string]string) (string, tea.Cmd) {
			tag := "chat.channel_update:" + id
			return tag, updateChannelDo(ctx, tag, id, api.UpdateChannelRequest{
				Name: api.Some(strings.TrimSpace(vals["name"])), Topic: api.Some(strings.TrimSpace(vals["topic"])),
			})
		},
		OnDone: channelResultDone(c.Chat.Renamed()),
	})
}

// ---- members ----

// addMembersPick is a picker OnPick adding the users to channel id (POST members).
func addMembersPick(id string) func(ctx *ui.Context, users []api.UserSummary, _ string) (string, tea.Cmd) {
	return func(ctx *ui.Context, users []api.UserSummary, _ string) (string, tea.Cmd) {
		if len(users) == 0 {
			return "", nil
		}
		refs := userRefs(users)
		tag := "chat.channel_add:" + id
		return tag, ui.Do(ctx, tag, func(c context.Context, cl *api.Client) (*api.Channel, error) {
			return cl.AddChannelMembers(c, id, refs)
		})
	}
}

// memberSet is the ids of users.
func memberSet(users []api.UserSummary) map[string]bool {
	out := make(map[string]bool, len(users))
	for _, u := range users {
		out[u.ID] = true
	}
	return out
}

// groupAddPicker adds people to a group DM: friends (online first) plus a server
// search, current members and blocked users hidden, up to 10 members in all.
func groupAddPicker(ctx *ui.Context, ch *api.Channel) ui.Overlay {
	in := memberSet(ch.Members)
	c := cat(ctx)
	return newUserPicker(pickerSpec{
		Title: c.Chat.ItemAddMembers(), Submit: c.Chat.AddSubmit(), Max: max(1, groupDMLimit-groupSize(ch)), Search: true,
		Candidates: friendCandidates, Order: byPresence,
		Exclude: func(ctx *ui.Context, id string) bool { return in[id] || blockedUser(ctx, id) },
		OnPick:  addMembersPick(ch.ID), OnDone: channelResultDone(c.Chat.MembersAdded()),
		DescribeErr: describeGroupDMErr,
	})
}

// memberLists are a private channel's space members and channel members.
type memberLists struct {
	space   []api.UserSummary
	channel []api.UserSummary
}

// allPages collects up to memberPages pages of a member listing.
func allPages[T any](list func(cursor string) (*api.Page[T], error)) ([]T, error) {
	var out []T
	cursor := ""
	for range memberPages {
		p, err := list(cursor)
		if err != nil {
			return nil, err
		}
		if p == nil {
			break
		}
		out = append(out, p.Items...)
		if p.Done() {
			break
		}
		cursor = *p.NextCursor
	}
	return out, nil
}

// channelMemberUsers lists channel id's members.
func channelMemberUsers(c context.Context, cl *api.Client, id string) ([]api.UserSummary, error) {
	ms, err := allPages(func(cursor string) (*api.Page[api.ChannelMember], error) {
		return cl.ListChannelMembers(c, id, api.PageOpts{Cursor: cursor, Limit: memberPageSize})
	})
	users := make([]api.UserSummary, len(ms))
	for i, m := range ms {
		users[i] = m.User
	}
	return users, err
}

// privateAddLoader adds space members to private channel id: the candidates are
// the space members who are not in the channel yet (both lists fetched first).
func privateAddLoader(c *i18n.T, id, spaceID string) ui.Overlay {
	return newLoader(c.Chat.ItemAddMembers(), "chat.channel_members:"+id, func(c context.Context, cl *api.Client) (memberLists, error) {
		sm, err := allPages(func(cursor string) (*api.Page[api.SpaceMember], error) {
			return cl.ListSpaceMembers(c, spaceID, api.SpaceMembersQuery{PageOpts: api.PageOpts{Cursor: cursor, Limit: memberPageSize}})
		})
		if err != nil {
			return memberLists{}, err
		}
		chm, err := channelMemberUsers(c, cl, id)
		if err != nil {
			return memberLists{}, err
		}
		out := memberLists{channel: chm}
		for _, m := range sm {
			out.space = append(out.space, m.User)
		}
		return out, nil
	}, func(_ *ui.Context, v memberLists) ui.Overlay {
		in := memberSet(v.channel)
		cands := slices.DeleteFunc(slices.Clone(v.space), func(u api.UserSummary) bool { return in[u.ID] })
		return newUserPicker(pickerSpec{
			Title: c.Chat.ItemAddMembers(), Submit: c.Chat.AddSubmit(), Max: dmMaxPeers,
			Candidates: func(*ui.Context) []api.UserSummary { return cands },
			Exclude:    func(_ *ui.Context, id string) bool { return in[id] },
			OnPick:     addMembersPick(id), OnDone: channelResultDone(c.Chat.MembersAdded()),
		})
	})
}

// privateKickLoader removes a member of a private channel (owner/admin): the
// channel members are fetched, then picked from a menu.
func privateKickLoader(c *i18n.T, ch *api.Channel) ui.Overlay {
	snap := *ch
	return newLoader(c.Chat.ItemKick(), "chat.channel_members:"+ch.ID, func(c context.Context, cl *api.Client) ([]api.UserSummary, error) {
		return channelMemberUsers(c, cl, snap.ID)
	}, func(ctx *ui.Context, users []api.UserSummary) ui.Overlay {
		return kickMenu(ctx, liveChannel(ctx, snap.ID, &snap), users)
	})
}

// kickMenu lists users (never me) to remove from ch; each asks first.
func kickMenu(ctx *ui.Context, ch *api.Channel, users []api.UserSummary) ui.Overlay {
	var items []menuItem
	for _, u := range users {
		if u.ID == "" || isMe(ctx, u.ID) {
			continue
		}
		name := textwidth.Sanitize(displayName(u))
		items = append(items, menuItem{Label: name, Run: func(ctx *ui.Context) (tea.Cmd, ui.Overlay) {
			c := cat(ctx)
			return nil, widgets.Confirm(c.Chat.KickAsk(name), c.Chat.KickBody(), c.Chat.KickYes(), c.Chat.Cancel(), kickMemberCmd(ctx, ch.ID, u))
		}})
	}
	c := cat(ctx)
	if len(items) == 0 {
		items = []menuItem{{Label: c.Chat.NoKickable(), Disabled: true}}
	}
	return newMenu(c.Chat.ItemKick(), items)
}

// kickMemberCmd removes u from channel id; a group DM drops u from its member
// list at once (the member_removed event that follows is then a no-op).
func kickMemberCmd(ctx *ui.Context, id string, u api.UserSummary) tea.Cmd {
	name := textwidth.Sanitize(displayName(u))
	return doAction(ctx, "chat.channel_kick:"+id+":"+u.ID, func(c context.Context, cl *api.Client) (struct{}, error) {
		return struct{}{}, cl.RemoveChannelMember(c, id, userRef(u))
	}, func(ctx *ui.Context, _ struct{}, err error) tea.Cmd {
		if err != nil {
			return errorCmd(err, cat(ctx).Chat.KickFailed())
		}
		if ch := liveChannel(ctx, id, nil); ch != nil && ch.Kind == api.ChannelGroupDM {
			if i := slices.IndexFunc(ch.Members, func(m api.UserSummary) bool { return m.ID == u.ID }); i >= 0 {
				ch.Members = slices.Delete(slices.Clone(ch.Members), i, i+1)
				ch.MemberCount = max(0, ch.MemberCount-1)
			}
		}
		return ui.Toast(cat(ctx).Chat.Kicked(name), ui.ToastSuccess)
	})
}

// ---- leave / delete ----

// channelGoneCmd delivers a local channel.deleted event (no seq) so the channel
// leaves the Store and every list at once, through the same Store.Apply path as
// the server's own event (which then repeats it harmlessly).
func channelGoneCmd(ctx *ui.Context, ch *api.Channel, reason string) tea.Cmd {
	data, err := json.Marshal(realtime.ChannelDeleted{ChannelID: ch.ID, SpaceID: ch.SpaceID, Reason: reason})
	if err != nil {
		return nil
	}
	ev := realtime.Event{Type: "channel.deleted", TS: ctx.Now(), Data: data}
	return func() tea.Msg { return ui.EventMsg{Event: ev} }
}

// leaveChannelCmd leaves ch (DELETE members/@me); on success it is gone and home opens.
func leaveChannelCmd(ctx *ui.Context, ch *api.Channel) tea.Cmd {
	snap := *ch
	return doAction(ctx, "chat.channel_leave:"+snap.ID, func(c context.Context, cl *api.Client) (struct{}, error) {
		return struct{}{}, cl.RemoveChannelMember(c, snap.ID, api.MeRef)
	}, func(ctx *ui.Context, _ struct{}, err error) tea.Cmd {
		if err != nil {
			return errorCmd(err, cat(ctx).Chat.LeaveFailed())
		}
		return tea.Batch(channelGoneCmd(ctx, &snap, "removed"), goHome(ctx), ui.Toast(cat(ctx).Chat.LeftConversation(), ui.ToastSuccess))
	})
}

// deleteChannelCmd deletes text channel ch; on success it is gone and the space's
// default channel opens.
func deleteChannelCmd(ctx *ui.Context, ch *api.Channel) tea.Cmd {
	snap := *ch
	title := channelTitle(ctx, ch, false)
	return doAction(ctx, "chat.channel_delete:"+snap.ID, func(c context.Context, cl *api.Client) (struct{}, error) {
		return struct{}{}, cl.DeleteChannel(c, snap.ID)
	}, func(ctx *ui.Context, _ struct{}, err error) tea.Cmd {
		if err != nil {
			return errorCmd(err, cat(ctx).Chat.DeleteChannelFailed())
		}
		var open tea.Cmd
		if snap.SpaceID != nil {
			open = goSpace(ctx, *snap.SpaceID)
		}
		return tea.Batch(channelGoneCmd(ctx, &snap, "deleted"), open, ui.Toast(cat(ctx).Chat.ChannelDeleted(title), ui.ToastSuccess))
	})
}

// ---- loader ----

// loaded is one loader fetch result (owner = the loader that asked).
type loaded[T any] struct {
	owner any
	v     T
}

// loader is a small modal that fetches T, then morphs into then(ctx, v) (nil
// closes it); a failure stays shown until esc.
type loader[T any] struct {
	morph
	title, tag string
	fetch      func(context.Context, *api.Client) (T, error)
	then       func(ctx *ui.Context, v T) ui.Overlay
	errMsg     string
	spinner    *widgets.Spinner
}

var (
	_ ui.Overlay       = (*loader[int])(nil)
	_ ui.KeyHandler    = (*loader[int])(nil)
	_ ui.MouseHandler  = (*loader[int])(nil)
	_ ui.Titled        = (*loader[int])(nil)
	_ ui.Helper        = (*loader[int])(nil)
	_ ui.Cursorer      = (*loader[int])(nil)
	_ ui.InputCapturer = (*loader[int])(nil)
)

func newLoader[T any](title, tag string, fetch func(context.Context, *api.Client) (T, error), then func(ctx *ui.Context, v T) ui.Overlay) ui.Overlay {
	return &loader[T]{title: title, tag: tag, fetch: fetch, then: then, spinner: widgets.NewSpinner()}
}

func (l *loader[T]) Init(ctx *ui.Context) tea.Cmd {
	return tea.Batch(ui.Do(ctx, l.tag, func(c context.Context, cl *api.Client) (loaded[T], error) {
		v, err := l.fetch(c, cl)
		return loaded[T]{owner: l, v: v}, err
	}), l.spinner.Start(ctx))
}

func (l *loader[T]) Update(ctx *ui.Context, msg tea.Msg) tea.Cmd {
	if l.next != nil {
		return l.next.Update(ctx, msg)
	}
	if m, ok := msg.(ui.ResultMsg[loaded[T]]); ok && m.Value.owner == l {
		l.spinner.Stop()
		if m.Err != nil {
			l.errMsg = errtext.Describe(ctx.T, m.Err)
			return nil
		}
		o := l.then(ctx, m.Value.v)
		if o == nil {
			return closeOverlay
		}
		return l.become(ctx, o)
	}
	return l.spinner.Update(ctx, msg)
}

func (l *loader[T]) Resize(ctx *ui.Context, size ui.Size) {
	l.size = size
	if l.next != nil {
		l.next.Resize(ctx, size)
	}
}

func (l *loader[T]) Bounds(body layout.Rect) layout.Rect {
	if l.next != nil {
		return l.next.Bounds(body)
	}
	return centered(body, min(body.W, menuMaxW), 3)
}

func (l *loader[T]) Modal() bool {
	if l.next != nil {
		return l.next.Modal()
	}
	return true
}

func (l *loader[T]) Title(ctx *ui.Context) (string, ui.Badge) {
	if l.next != nil {
		return l.morph.title(ctx)
	}
	return l.title, ui.Badge{}
}

func (l *loader[T]) Help(ctx *ui.Context) []key.Binding {
	if l.next != nil {
		return l.morph.help(ctx)
	}
	return []key.Binding{key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", cat(ctx).Common.HelpClose()))}
}

func (l *loader[T]) Cursor() *tea.Cursor {
	if l.next != nil {
		return l.morph.cursor()
	}
	return nil
}

func (l *loader[T]) CapturesInput() bool { return l.next != nil && l.captures() }

func (l *loader[T]) HandleKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	if l.next != nil {
		return l.morph.key(ctx, msg)
	}
	return false, nil // esc: the app closes it
}

func (l *loader[T]) HandleMouse(ctx *ui.Context, msg tea.MouseMsg, local ui.Point) tea.Cmd {
	if l.next != nil {
		return l.morph.mouse(ctx, msg, local)
	}
	return nil
}

func (l *loader[T]) View(ctx *ui.Context, focused bool) string {
	if l.next != nil {
		return l.next.View(ctx, focused)
	}
	if l.size.W <= 0 || l.size.H <= 0 {
		return ""
	}
	t := themeOf(ctx)
	cw := max(1, l.size.W-2*overlayPad(l.size.W))
	var row string
	if l.errMsg != "" {
		row = cell(t.S.Error.Render, "✕ "+textwidth.Sanitize(l.errMsg), cw)
	} else {
		row = l.spinner.View(t) + cell(t.S.Dim.Render, " "+cat(ctx).Chat.Loading(), cw-1)
	}
	rows := []string{row}
	if l.size.H >= 3 { // degraded to the whole body (no border): the title goes inside
		rows = []string{cell(t.S.Title.Render, textwidth.Sanitize(l.title), cw), "", row}
	}
	return renderOverlayRows(t, rows, l.size)
}
