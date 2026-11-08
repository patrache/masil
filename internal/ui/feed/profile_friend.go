package feed

import (
	"context"
	"encoding/json"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/widgets"
)

// Profile friend actions (v0.1.1 C2): the menu item of the relationship over the
// api.md 5.3 endpoints.
const (
	textMenuFriendAdd     = "친구 요청"
	textMenuFriendCancel  = "친구 요청 취소"
	textMenuFriendAccept  = "친구 요청 수락"
	textMenuFriendDecline = "친구 요청 거절"
	textMenuUnfriend      = "친구 끊기"

	textUnfriendAsk  = "님과 친구를 끊을까요?"
	textUnfriendBody = "지난 DM은 남지만 읽기 전용이 될 수 있어요"
	textUnfriendYes  = "끊기"

	textFriendSentSfx     = "님에게 친구 요청을 보냈어요"
	textFriendNowSfx      = "님과 이제 친구예요"
	textFriendAcceptedSfx = "님과 친구가 되었어요"
	textFriendCanceled    = "친구 요청을 취소했어요"
	textFriendDeclined    = "친구 요청을 거절했어요"
	textUnfriendedSfx     = "님과 친구를 끊었어요"

	errContextFriend = "친구"

	tagFriend = "feed.friend:" // + action + ":" + user id
)

// friendAction is one profile friend action.
type friendAction string

const (
	friendSend    friendAction = "send"
	friendCancel  friendAction = "cancel"
	friendAccept  friendAction = "accept"
	friendDecline friendAction = "decline"
	friendRemove  friendAction = "remove"
)

// friendOutcome is a friend action's answer for userID.
type friendOutcome struct {
	claim
	userID string
	action friendAction
	sent   *api.FriendRequestResult // friendSend
}

// friendCmd runs action on the profile's user (reqID: the pending request).
func friendCmd(ctx *ui.Context, action friendAction, userID, reqID string) tea.Cmd {
	o := &friendOutcome{userID: userID, action: action}
	return ui.Do(ctx, tagFriend+string(action)+":"+userID, func(c context.Context, a *api.Client) (*friendOutcome, error) {
		if a == nil {
			return o, errNoClient
		}
		var err error
		switch action {
		case friendSend:
			o.sent, err = a.SendFriendRequest(c, api.UserID(userID))
		case friendCancel:
			err = a.CancelFriendRequest(c, reqID)
		case friendAccept:
			_, err = a.AcceptFriendRequest(c, reqID)
		case friendDecline:
			err = a.DeclineFriendRequest(c, reqID)
		case friendRemove:
			err = a.RemoveFriend(c, api.UserID(userID))
		}
		return o, err
	})
}

// friendMenuItems is the friend item(s) of someone else's profile: 친구 끊기
// (friends), 친구 요청 수락 · 거절 (received), 친구 요청 취소 (sent), else 친구
// 요청. None on my profile, for a blocked user, while an action is in flight,
// and no new request / accept to a suspended user (the server refuses them).
func (p *profile) friendMenuItems(ctx *ui.Context) []menuItem {
	r := p.rel()
	if p.prof == nil || r == nil || p.isMe(ctx) || p.blocked(ctx) || p.friending {
		return nil
	}
	id, name := p.prof.ID, displayName(p.prof.UserSummary)
	run := func(a friendAction, reqID string) func(*ui.Context) tea.Cmd {
		return func(ctx *ui.Context) tea.Cmd {
			p.friending = true
			return friendCmd(ctx, a, id, reqID)
		}
	}
	switch fr := r.FriendRequest; {
	case r.IsFriend:
		return []menuItem{{label: textMenuUnfriend, open: func(ctx *ui.Context) ui.Overlay {
			// no in-flight mark here: the confirm may be cancelled
			return widgets.Confirm(name+textUnfriendAsk, textUnfriendBody, textUnfriendYes, textCancel, friendCmd(ctx, friendRemove, id, ""))
		}}}
	case fr != nil && fr.Direction == "incoming":
		var out []menuItem
		if !p.prof.Suspended {
			out = append(out, menuItem{label: textMenuFriendAccept, run: run(friendAccept, fr.ID)})
		}
		return append(out, menuItem{label: textMenuFriendDecline, run: run(friendDecline, fr.ID)})
	case fr != nil && fr.Direction == "outgoing":
		return []menuItem{{label: textMenuFriendCancel, run: run(friendCancel, fr.ID)}}
	case p.prof.Suspended:
		return nil
	}
	return []menuItem{{label: textMenuFriendAdd, run: run(friendSend, "")}}
}

// onFriend applies a friend action's answer to this profile's relationship
// (the events that follow only confirm it).
func (p *profile) onFriend(m ui.ResultMsg[*friendOutcome]) tea.Cmd {
	o := m.Value
	if o == nil || p.prof == nil || o.userID != p.prof.ID {
		return nil
	}
	p.friending = false
	first := o.take()
	if m.Err != nil {
		if first {
			return profileErr(m.Err, errContextFriend)
		}
		return nil
	}
	r := p.rel()
	if r == nil {
		return nil
	}
	name := textwidth.Sanitize(displayName(p.prof.UserSummary))
	var toast string
	switch o.action {
	case friendSend:
		if o.sent != nil && o.sent.AutoAccepted {
			r.IsFriend, r.FriendRequest, r.CanDM = true, nil, true
			toast = name + textFriendNowSfx
		} else {
			r.FriendRequest = nil
			if o.sent != nil && o.sent.Request.ID != "" {
				r.FriendRequest = &api.FriendRequestRef{ID: o.sent.Request.ID, Direction: "outgoing"}
			}
			toast = name + textFriendSentSfx
		}
	case friendAccept:
		r.IsFriend, r.FriendRequest, r.CanDM = true, nil, true
		toast = name + textFriendAcceptedSfx
	case friendCancel:
		r.FriendRequest, toast = nil, textFriendCanceled
	case friendDecline:
		r.FriendRequest, toast = nil, textFriendDeclined
	case friendRemove:
		r.IsFriend, toast = false, name+textUnfriendedSfx
	}
	if !first {
		return nil
	}
	return ui.Toast(toast, ui.ToastSuccess)
}

// onFriendRequestEvent follows friend.request_received / _accepted / _removed
// for this profile's user (the friend item of the menu changes with them).
func (p *profile) onFriendRequestEvent(typ string, data json.RawMessage) {
	r := p.rel()
	if r == nil {
		return
	}
	switch typ {
	case "friend.request_received":
		var d struct {
			Request api.FriendRequest `json:"request"`
		}
		if json.Unmarshal(data, &d) == nil && d.Request.From.ID == p.prof.ID && !r.IsFriend {
			r.FriendRequest = &api.FriendRequestRef{ID: d.Request.ID, Direction: "incoming"}
		}
	case "friend.request_accepted":
		var d struct {
			Request api.FriendRequest `json:"request"`
		}
		if json.Unmarshal(data, &d) == nil && r.FriendRequest != nil && r.FriendRequest.ID == d.Request.ID {
			r.FriendRequest = nil // friend.added makes the friendship
		}
	case "friend.request_removed":
		var d struct {
			RequestID string `json:"request_id"`
		}
		if json.Unmarshal(data, &d) == nil && r.FriendRequest != nil && r.FriendRequest.ID == d.RequestID {
			r.FriendRequest = nil
		}
	}
}
