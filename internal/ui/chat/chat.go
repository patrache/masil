// Package chat is the chat UI (plan 04): the rail, channel/DM list, member
// panes, the conversation, thread, home, friends, invite, space-settings and
// search routes, and the chat overlays. Each file registers its own pieces from
// init() (ui.RegisterPane / RegisterRoute / RegisterOverlay, palette.Register);
// placement is the app's (CA 5.3.3), components only read ctx.Density() and the
// size they are resized to.
package chat

import (
	"reflect"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/store"
	"github.com/patrache/masil/internal/store/chatstore"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
)

// chatOf is the chat store of ctx with its clock set to ctx.Now (typing expiry
// follows the UI clock: the FakeClock in tests). Chat code always uses this, never
// chatstore.Get directly.
func chatOf(ctx *ui.Context) *chatstore.ChatStore {
	if ctx == nil || ctx.Store == nil {
		return nil
	}
	c := chatstore.Get(ctx.Store)
	if c != nil && ctx.Now != nil {
		c.SetClock(ctx.Now)
	}
	return c
}

// dmPeer is the other member of a 1:1 DM (nil for a DM with myself or no members).
func dmPeer(s *store.Store, ch *api.Channel) *api.UserSummary {
	if s == nil || ch == nil {
		return nil
	}
	for i := range ch.Members {
		if !s.IsMe(ch.Members[i].ID) {
			if u := s.User(ch.Members[i].ID); u != nil {
				return u
			}
			return &ch.Members[i]
		}
	}
	return nil
}

// visibleDMs is Store.DMOrder without the 1:1 DMs whose other member I blocked
// (api.md 3.2: hidden on the blocker's side, also before a resync drops it from
// GET /v1/dms). Group DMs stay (only the messages fold). The sidebar, home and the
// rail's home unread sum use only this.
func visibleDMs(ctx *ui.Context) []*api.Channel {
	if ctx == nil || ctx.Store == nil {
		return nil
	}
	s := ctx.Store
	var out []*api.Channel
	for _, id := range s.DMOrder {
		ch := s.Channels[id]
		if ch == nil {
			continue
		}
		if ch.Kind == api.ChannelDM {
			if p := dmPeer(s, ch); p != nil && s.IsBlocked(p.ID) {
				continue
			}
		}
		out = append(out, ch)
	}
	return out
}

// pendingFor is the channel's pending sends of one list: rootID "" = the channel's
// top-level list (ThreadRootID == nil), else that thread's replies. Pending is per
// channel, so lists must always filter through this before building rows.
func pendingFor(ctx *ui.Context, channelID, rootID string) []chatstore.PendingMessage {
	c := chatOf(ctx)
	if c == nil {
		return nil
	}
	var out []chatstore.PendingMessage
	for _, p := range c.Pending(channelID) {
		switch {
		case rootID == "" && p.ThreadRootID == nil:
		case rootID != "" && p.ThreadRootID != nil && *p.ThreadRootID == rootID:
		default:
			continue
		}
		out = append(out, p)
	}
	return out
}

// viewedChannel is the active channel when it is visible, else the visible channel
// with the smallest id, else nil.
func viewedChannel(ctx *ui.Context) *api.Channel {
	if ctx == nil || ctx.Store == nil {
		return nil
	}
	s := ctx.Store
	if c := chatOf(ctx); c != nil {
		if id := c.ActiveChannel(); id != "" && s.Viewing(id) && s.Channels[id] != nil {
			return s.Channels[id]
		}
	}
	ids := make([]string, 0, len(s.Channels))
	for id := range s.Channels {
		if s.Viewing(id) {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	slices.Sort(ids)
	return s.Channels[ids[0]]
}

// spaceOf is the space of a text channel (nil for DMs and unknown spaces).
func spaceOf(ctx *ui.Context, ch *api.Channel) *api.SpaceDetail {
	if ctx == nil || ctx.Store == nil || ch == nil || ch.SpaceID == nil {
		return nil
	}
	return ctx.Store.Spaces[*ch.SpaceID]
}

// currentSpaceID is the viewed channel's space, else the rail selection, else the
// first space, else "".
func currentSpaceID(ctx *ui.Context) string {
	if ctx == nil || ctx.Store == nil {
		return ""
	}
	s := ctx.Store
	if sd := spaceOf(ctx, viewedChannel(ctx)); sd != nil {
		return sd.ID
	}
	if c := chatOf(ctx); c != nil {
		if id := c.SelectedSpace(); id != "" && s.Spaces[id] != nil {
			return id
		}
	}
	if len(s.SpaceOrder) > 0 {
		return s.SpaceOrder[0]
	}
	return ""
}

// myRole is my role in a space ("" when unknown).
func myRole(ctx *ui.Context, spaceID string) api.SpaceRole {
	if ctx == nil || ctx.Store == nil {
		return ""
	}
	if sd := ctx.Store.Spaces[spaceID]; sd != nil {
		return sd.MyRole
	}
	return ""
}

// canManageSpace: owner or admin.
func canManageSpace(ctx *ui.Context, spaceID string) bool {
	r := myRole(ctx, spaceID)
	return r == api.RoleOwner || r == api.RoleAdmin
}

// canPin: text channels owner|admin, DMs and group DMs anyone (api.md 3.7).
func canPin(ctx *ui.Context, ch *api.Channel) bool {
	if ch == nil {
		return false
	}
	switch ch.Kind {
	case api.ChannelDM, api.ChannelGroupDM:
		return true
	case api.ChannelText:
		return ch.SpaceID != nil && canManageSpace(ctx, *ch.SpaceID)
	}
	return false
}

// canDelete: my own message, or any message of a text channel for owner|admin.
func canDelete(ctx *ui.Context, ch *api.Channel, m api.Message) bool {
	if ctx == nil || ctx.Store == nil {
		return false
	}
	if ctx.Store.IsMe(m.Author.ID) {
		return true
	}
	return ch != nil && ch.Kind == api.ChannelText && ch.SpaceID != nil && canManageSpace(ctx, *ch.SpaceID)
}

// adoptSpace stores a space I just created or joined: Spaces[id] = d, appended to
// SpaceOrder when missing, every channel observed. Idempotent.
func adoptSpace(s *store.Store, d *api.SpaceDetail) {
	if s == nil || d == nil || d.ID == "" {
		return
	}
	s.Spaces[d.ID] = d
	if !slices.Contains(s.SpaceOrder, d.ID) {
		s.SpaceOrder = append(s.SpaceOrder, d.ID)
	}
	for _, ch := range slices.Clone(d.Channels) {
		s.ObserveChannel(ch)
	}
}

// markReadLocal applies a read result to the channel's My state (unknown channel = no-op).
func markReadLocal(s *store.Store, st api.UnreadState) {
	if s == nil {
		return
	}
	ch := s.Channels[st.ChannelID]
	if ch == nil {
		return
	}
	ch.My.UnreadCount = st.UnreadCount
	ch.My.MentionCount = st.MentionCount
	if st.LastReadMessageID != nil {
		id := *st.LastReadMessageID
		ch.My.LastReadMessageID = &id
	}
}

// presenceRune is the presence mark of a status (●◐⊘○).
func presenceRune(st api.PresenceStatus) string {
	switch st {
	case api.PresenceOnline:
		return "●"
	case api.PresenceAway:
		return "◐"
	case api.PresenceDND:
		return "⊘"
	default:
		return "○"
	}
}

// channelTitle is the display title (sanitized): "# 개발", "# 개발 · 주제", private
// "🔒개발", DM "민지 ●" (presence mark), group DM Store.ChannelLabel.
func channelTitle(ctx *ui.Context, ch *api.Channel, withTopic bool) string {
	if ctx == nil || ctx.Store == nil || ch == nil {
		return ""
	}
	s := ctx.Store
	switch ch.Kind {
	case api.ChannelText:
		name := ""
		if ch.Name != nil {
			name = textwidth.Sanitize(*ch.Name)
		}
		title := "# " + name
		if ch.IsPrivate {
			title = "🔒" + name
		}
		if topic := strings.TrimSpace(textwidth.Sanitize(ch.Topic)); withTopic && topic != "" {
			title += " · " + topic
		}
		return title
	case api.ChannelDM:
		return textwidth.Sanitize(s.ChannelLabel(ch.ID)) + " " + presenceRune(dmPresence(ctx, ch))
	default:
		return textwidth.Sanitize(s.ChannelLabel(ch.ID))
	}
}

// resultPkg is the package of ui.ResultMsg.
var resultPkg = reflect.TypeFor[ui.ResultMsg[struct{}]]().PkgPath()

// resultOf reports whether msg is a ui.ResultMsg[T] (any T) and returns its Tag and Err.
func resultOf(msg tea.Msg) (tag string, err error, ok bool) {
	if msg == nil {
		return "", nil, false
	}
	v := reflect.ValueOf(msg)
	t := v.Type()
	if t.Kind() != reflect.Struct || t.PkgPath() != resultPkg || !strings.HasPrefix(t.Name(), "ResultMsg[") {
		return "", nil, false
	}
	tf, ef := v.FieldByName("Tag"), v.FieldByName("Err")
	if !tf.IsValid() || tf.Kind() != reflect.String || !ef.IsValid() {
		return "", nil, false
	}
	if e, isErr := ef.Interface().(error); isErr {
		err = e
	}
	return tf.String(), err, true
}
