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
		ID: "channel.prefs", Title: "채널 설정", Keywords: []string{"channel", "settings", "notify", "mute", "알림", "음소거", "대화 설정"}, Group: palette.GroupCommand,
		Available: func(ctx *ui.Context) bool { return viewedChannel(ctx) != nil },
		Run:       func(_ *ui.Context, args string) tea.Cmd { return ui.OpenRegisteredOverlay(overlayChannelPrefs, args) },
	})
}

// Registered overlay ids.
const (
	overlayChannelPrefs = "chat.channel_prefs" // args = channel id ("" = the viewed channel)
	overlayPins         = "chat.pins"          // pins.go; args = channel id
)

// Channel settings texts and limits.
const (
	prefsTitleChannel = "채널 설정 · "
	prefsTitleDM      = "대화 설정 · "
	itemNotifyAll     = "알림: 모두"
	itemNotifyMention = "알림: 멘션만"
	itemNotifyNone    = "알림: 끄기"
	itemMute          = "음소거"
	itemUnmute        = "음소거 해제"
	itemPins          = "고정 메시지 보기"
	itemRename        = "이름 바꾸기"
	itemEditChannel   = "이름·주제 바꾸기"
	itemAddMembers    = "멤버 추가"
	itemKick          = "멤버 내보내기"
	itemLeave         = "나가기"
	itemDeleteChannel = "채널 삭제"
	checkHint         = "✓"
	defaultChanHint   = "기본 채널"
	groupFullHint     = "최대 10명"
	noKickable        = "내보낼 멤버가 없어요"

	renameTitle      = "대화 이름 바꾸기"
	renameLabel      = "대화 이름"
	renameHolder     = "비우면 멤버 이름으로 보여요"
	editTitle        = "채널 이름·주제"
	nameLabel        = "채널 이름"
	topicLabel       = "주제"
	topicHolder      = "채널 주제(선택)"
	changeSubmit     = "바꾸기"
	addSubmit        = "추가"
	kickAskSuffix    = "님을 내보낼까요?"
	kickBody         = "다시 들어오려면 누군가 추가해야 해요"
	kickYes          = "내보내기"
	leaveAskSuffix   = "에서 나갈까요?"
	leaveBody        = "다시 들어오려면 멤버가 추가해 줘야 해요"
	deleteAskSuffix  = " 채널을 삭제할까요?"
	deleteChanBody   = "메시지도 함께 사라지고 되돌릴 수 없어요"
	prefsFailed      = "알림 설정을 바꾸지 못했어요"
	kickFailed       = "멤버를 내보내지 못했어요"
	leaveFailed      = "대화에서 나가지 못했어요"
	deleteChanFailed = "채널을 삭제하지 못했어요"
	notifySetPrefix  = "알림 설정: "
	mutedText        = "음소거했어요"
	unmutedText      = "음소거를 해제했어요"
	renamedText      = "이름을 바꿨어요"
	membersAddedText = "멤버를 추가했어요"
	kickedSuffix     = "님을 내보냈어요"
	leftText         = "대화에서 나갔어요"
	deletedSuffix    = " 채널을 삭제했어요"
	badChannelName   = "채널 이름은 영문 소문자·숫자·한글·_·- 1~32자예요"
	topicTooLong     = "주제는 250자까지예요"
	loaderText       = " 불러오는 중…"

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

// validChannelName is a form Validate for channel names.
func validChannelName(s string) string {
	if !channelNameRe.MatchString(s) {
		return badChannelName
	}
	return ""
}

// validTopic is a form Validate for channel topics (0..250 code points).
func validTopic(s string) string {
	if len([]rune(s)) > topicMax {
		return topicTooLong
	}
	return ""
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
	title := prefsTitleChannel
	if ch.Kind != api.ChannelText {
		title = prefsTitleDM
	}
	return newMenu(title+channelTitle(ctx, ch, false), channelPrefItems(ctx, ch))
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
	var items []menuItem
	for _, lv := range []struct{ value, label string }{
		{"all", itemNotifyAll}, {"mentions", itemNotifyMention}, {"none", itemNotifyNone},
	} {
		it := menuItem{Label: lv.label, Run: func(ctx *ui.Context) (tea.Cmd, ui.Overlay) {
			return setPrefsCmd(ctx, id, prefsChange{notify: &lv.value, label: lv.label}), nil
		}}
		if lv.value == notify {
			it.Hint = checkHint
		}
		items = append(items, it)
	}
	muted := !ch.My.Muted
	muteLabel := itemMute
	if ch.My.Muted {
		muteLabel = itemUnmute
	}
	items = append(items,
		menuItem{Label: muteLabel, Run: func(ctx *ui.Context) (tea.Cmd, ui.Overlay) {
			return setPrefsCmd(ctx, id, prefsChange{muted: &muted}), nil
		}},
		menuItem{Label: itemPins, Run: openRegistered(overlayPins, id)},
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
	items := []menuItem{{Label: itemRename, Run: func(ctx *ui.Context) (tea.Cmd, ui.Overlay) {
		return nil, renameGroupForm(liveChannel(ctx, id, &snap))
	}}}
	add := menuItem{Label: itemAddMembers, Run: func(ctx *ui.Context) (tea.Cmd, ui.Overlay) {
		return nil, groupAddPicker(ctx, liveChannel(ctx, id, &snap))
	}}
	if groupSize(ch) >= groupDMLimit {
		add.Disabled, add.Hint = true, groupFullHint
	}
	items = append(items, add)
	if ch.OwnerID != nil && isMe(ctx, *ch.OwnerID) {
		items = append(items, menuItem{Label: itemKick, Run: func(ctx *ui.Context) (tea.Cmd, ui.Overlay) {
			cur := liveChannel(ctx, id, &snap)
			return nil, kickMenu(ctx, cur, cur.Members)
		}})
	}
	return append(items, menuItem{Label: itemLeave, Danger: true, Run: func(ctx *ui.Context) (tea.Cmd, ui.Overlay) {
		cur := liveChannel(ctx, id, &snap)
		label := textwidth.Sanitize(ctx.Store.ChannelLabel(id))
		return nil, widgets.Confirm(label+leaveAskSuffix, leaveBody, itemLeave, cancelText, leaveChannelCmd(ctx, cur))
	}})
}

// groupSize is the member count of a group DM (me included).
func groupSize(ch *api.Channel) int { return max(len(ch.Members), ch.MemberCount) }

// textChannelItems: owner/admin 이름·주제 바꾸기 and 채널 삭제; private 멤버 추가
// (+ owner/admin 멤버 내보내기).
func textChannelItems(ctx *ui.Context, ch *api.Channel) []menuItem {
	id, snap := ch.ID, *ch
	manage := ch.SpaceID != nil && canManageSpace(ctx, *ch.SpaceID)
	var items []menuItem
	if manage {
		del := menuItem{Label: itemDeleteChannel, Danger: true, Run: func(ctx *ui.Context) (tea.Cmd, ui.Overlay) {
			cur := liveChannel(ctx, id, &snap)
			return nil, widgets.Confirm(channelTitle(ctx, cur, false)+deleteAskSuffix, deleteChanBody, deleteYes, cancelText, deleteChannelCmd(ctx, cur))
		}}
		if sd := spaceOf(ctx, ch); sd != nil && sd.DefaultChannelID == id {
			del.Disabled, del.Hint = true, defaultChanHint
		}
		items = append(items, menuItem{Label: itemEditChannel, Run: func(ctx *ui.Context) (tea.Cmd, ui.Overlay) {
			return nil, editChannelForm(liveChannel(ctx, id, &snap))
		}}, del)
	}
	if ch.IsPrivate && ch.SpaceID != nil {
		space := *ch.SpaceID
		items = append(items, menuItem{Label: itemAddMembers, Run: func(*ui.Context) (tea.Cmd, ui.Overlay) {
			return nil, privateAddLoader(id, space)
		}})
		if manage {
			items = append(items, menuItem{Label: itemKick, Run: func(ctx *ui.Context) (tea.Cmd, ui.Overlay) {
				return nil, privateKickLoader(liveChannel(ctx, id, &snap))
			}})
		}
	}
	return items
}

// ---- prefs ----

// prefsChange is one PUT /v1/channels/{id}/prefs change (label = the notify item).
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
			return errorCmd(err, prefsFailed)
		}
		if cur != nil && my != nil {
			cur.My.Notify, cur.My.Muted = my.Notify, my.Muted
		}
		switch {
		case p.muted != nil && *p.muted:
			return ui.Toast(mutedText, ui.ToastSuccess)
		case p.muted != nil:
			return ui.Toast(unmutedText, ui.ToastSuccess)
		}
		return ui.Toast(notifySetPrefix+strings.TrimPrefix(p.label, "알림: "), ui.ToastSuccess)
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
func renameGroupForm(ch *api.Channel) ui.Overlay {
	id, name := ch.ID, ""
	if ch.Name != nil {
		name = *ch.Name
	}
	return newForm(formSpec{
		Title: renameTitle, Submit: changeSubmit,
		Fields: []field{{Key: "name", Label: renameLabel, Placeholder: renameHolder, Value: name, Max: groupNameMax}},
		OnSubmit: func(ctx *ui.Context, vals map[string]string) (string, tea.Cmd) {
			tag := "chat.channel_update:" + id
			return tag, updateChannelDo(ctx, tag, id, api.UpdateChannelRequest{Name: api.Some(strings.TrimSpace(vals["name"]))})
		},
		OnDone: channelResultDone(renamedText),
	})
}

// editChannelForm edits a text channel's name (the name rule) and topic (0..250).
func editChannelForm(ch *api.Channel) ui.Overlay {
	id, name := ch.ID, ""
	if ch.Name != nil {
		name = *ch.Name
	}
	return newForm(formSpec{
		Title: editTitle, Submit: changeSubmit,
		Fields: []field{
			{Key: "name", Label: nameLabel, Value: name, Max: channelNameMax, Validate: func(s string) string { return validChannelName(strings.TrimSpace(s)) }},
			{Key: "topic", Label: topicLabel, Placeholder: topicHolder, Value: ch.Topic, Max: topicMax, Validate: validTopic},
		},
		OnSubmit: func(ctx *ui.Context, vals map[string]string) (string, tea.Cmd) {
			tag := "chat.channel_update:" + id
			return tag, updateChannelDo(ctx, tag, id, api.UpdateChannelRequest{
				Name: api.Some(strings.TrimSpace(vals["name"])), Topic: api.Some(strings.TrimSpace(vals["topic"])),
			})
		},
		OnDone: channelResultDone(renamedText),
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
func groupAddPicker(_ *ui.Context, ch *api.Channel) ui.Overlay {
	in := memberSet(ch.Members)
	return newUserPicker(pickerSpec{
		Title: itemAddMembers, Submit: addSubmit, Max: max(1, groupDMLimit-groupSize(ch)), Search: true,
		Candidates: friendCandidates, Order: byPresence,
		Exclude: func(ctx *ui.Context, id string) bool { return in[id] || blockedUser(ctx, id) },
		OnPick:  addMembersPick(ch.ID), OnDone: channelResultDone(membersAddedText),
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
func privateAddLoader(id, spaceID string) ui.Overlay {
	return newLoader(itemAddMembers, "chat.channel_members:"+id, func(c context.Context, cl *api.Client) (memberLists, error) {
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
			Title: itemAddMembers, Submit: addSubmit, Max: dmMaxPeers,
			Candidates: func(*ui.Context) []api.UserSummary { return cands },
			Exclude:    func(_ *ui.Context, id string) bool { return in[id] },
			OnPick:     addMembersPick(id), OnDone: channelResultDone(membersAddedText),
		})
	})
}

// privateKickLoader removes a member of a private channel (owner/admin): the
// channel members are fetched, then picked from a menu.
func privateKickLoader(ch *api.Channel) ui.Overlay {
	snap := *ch
	return newLoader(itemKick, "chat.channel_members:"+ch.ID, func(c context.Context, cl *api.Client) ([]api.UserSummary, error) {
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
			return nil, widgets.Confirm(name+kickAskSuffix, kickBody, kickYes, cancelText, kickMemberCmd(ctx, ch.ID, u))
		}})
	}
	if len(items) == 0 {
		items = []menuItem{{Label: noKickable, Disabled: true}}
	}
	return newMenu(itemKick, items)
}

// kickMemberCmd removes u from channel id; a group DM drops u from its member
// list at once (the member_removed event that follows is then a no-op).
func kickMemberCmd(ctx *ui.Context, id string, u api.UserSummary) tea.Cmd {
	name := textwidth.Sanitize(displayName(u))
	return doAction(ctx, "chat.channel_kick:"+id+":"+u.ID, func(c context.Context, cl *api.Client) (struct{}, error) {
		return struct{}{}, cl.RemoveChannelMember(c, id, userRef(u))
	}, func(ctx *ui.Context, _ struct{}, err error) tea.Cmd {
		if err != nil {
			return errorCmd(err, kickFailed)
		}
		if ch := liveChannel(ctx, id, nil); ch != nil && ch.Kind == api.ChannelGroupDM {
			if i := slices.IndexFunc(ch.Members, func(m api.UserSummary) bool { return m.ID == u.ID }); i >= 0 {
				ch.Members = slices.Delete(slices.Clone(ch.Members), i, i+1)
				ch.MemberCount = max(0, ch.MemberCount-1)
			}
		}
		return ui.Toast(name+kickedSuffix, ui.ToastSuccess)
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
			return errorCmd(err, leaveFailed)
		}
		return tea.Batch(channelGoneCmd(ctx, &snap, "removed"), goHome(ctx), ui.Toast(leftText, ui.ToastSuccess))
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
			return errorCmd(err, deleteChanFailed)
		}
		var open tea.Cmd
		if snap.SpaceID != nil {
			open = goSpace(ctx, *snap.SpaceID)
		}
		return tea.Batch(channelGoneCmd(ctx, &snap, "deleted"), open, ui.Toast(title+deletedSuffix, ui.ToastSuccess))
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
			l.errMsg = api.Describe(m.Err)
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
	return []key.Binding{key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "닫기"))}
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
		row = l.spinner.View(t) + cell(t.S.Dim.Render, loaderText, cw-1)
	}
	rows := []string{row}
	if l.size.H >= 3 { // degraded to the whole body (no border): the title goes inside
		rows = []string{cell(t.S.Title.Render, textwidth.Sanitize(l.title), cw), "", row}
	}
	return renderOverlayRows(t, rows, l.size)
}
