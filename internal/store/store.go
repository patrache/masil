// Package store is the shared app state, mutated only on the Bubble Tea Update goroutine (CA 4.5).
package store

import (
	"sort"
	"strings"
	"time"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/realtime"
	"github.com/patrache/masil/internal/textwidth"
)

// Store holds core entities. Domain stores plug in as reducers.
type Store struct {
	Me               *api.Me
	Self             api.PresenceState // presence.self / PUT presence
	Focus            *api.Focus
	LastFocusSummary *api.FocusSummary // set by focus.ended or the EndFocus caller (FocusEnded); read by the focus-summary overlay (9.1)
	Settings         api.Settings
	Users            map[string]*api.UserSummary // every embedded UserSummary seen (ObserveUser)
	Presence         map[string]PresenceEntry
	Spaces           map[string]*api.SpaceDetail
	SpaceOrder       []string                // GET /v1/spaces order (join order)
	Channels         map[string]*api.Channel // text + dm + group_dm; Channel.My kept authoritative here
	DMOrder          []string                // last_message_at desc
	Friends          map[string]api.Friend   // by user id
	FriendReqIn      []api.FriendRequest
	DirectInvites    []api.DirectInvite
	Blocks           map[string]time.Time // blocked user id -> since
	NotifUnread      int
	Sync             SyncState
	Alerts           *Ring[api.Alert] // last 50 alerts (in-app panel / toasts)

	reducers     map[string]Reducer
	reducerOrder []Reducer

	viewing    map[string]bool
	counted    *idSet // message.created IDs already applied to unread/mention counters
	mentioned  *idSet // message IDs whose mention was already counted (direct or @here notification)
	removedInv *idSet // direct invite IDs already dropped by space.invite_removed (a late invite_received is ignored)
	now        func() time.Time

	flight       *flight           // open while a bootstrap is in flight (flight.go)
	replaying    bool              // replayFlight is re-applying the journal
	applying     bool              // inside Apply (ObserveChannel calls are journaled by the event)
	snapshotLast map[string]string // during a replay: channel id -> snapshot last_message_id
}

// PresenceEntry is a cached presence value.
type PresenceEntry struct {
	Status     api.PresenceStatus
	LastSeenAt *time.Time
	FetchedAt  time.Time
}

// SyncState tracks realtime/bootstrap progress.
type SyncState struct {
	Phase        realtime.Phase
	Ready        bool // at least one ready on current process
	Bootstrapped bool // step-1 REST done at least once
	LastResync   time.Time
	Err          error
	Stale        bool // the last ApplyBootstrap could not re-apply its flight journal (too many events): bootstrap again
}

const (
	alertRingSize = 50
	dedupeIDs     = 1000 // recent message IDs remembered for counter de-duplication
	maxCount      = 999  // ChannelMyState counters are capped at 999 (api.md 2)
)

// New creates a Store and instantiates every registered reducer (duplicate Name => panic).
func New() *Store {
	s := &Store{now: time.Now}
	s.clear()
	s.buildReducers()
	return s
}

// clear resets every core field to its empty, usable state.
func (s *Store) clear() {
	s.Me = nil
	s.Self = api.PresenceState{}
	s.Focus = nil
	s.LastFocusSummary = nil
	s.Settings = api.Settings{}
	s.Users = map[string]*api.UserSummary{}
	s.Presence = map[string]PresenceEntry{}
	s.Spaces = map[string]*api.SpaceDetail{}
	s.SpaceOrder = nil
	s.Channels = map[string]*api.Channel{}
	s.DMOrder = nil
	s.Friends = map[string]api.Friend{}
	s.FriendReqIn = nil
	s.DirectInvites = nil
	s.Blocks = map[string]time.Time{}
	s.NotifUnread = 0
	s.Sync = SyncState{}
	s.Alerts = NewRing[api.Alert](alertRingSize)
	s.viewing = map[string]bool{}
	s.counted = newIDSet(dedupeIDs)
	s.mentioned = newIDSet(dedupeIDs)
	s.removedInv = newIDSet(dedupeIDs)
	s.flight, s.replaying, s.applying, s.snapshotLast = nil, false, false, nil
}

// ObserveUser updates the user cache, keeping the newest (last observed value wins).
// An existing entry is updated in place so held pointers see the change.
func (s *Store) ObserveUser(u api.UserSummary) {
	if u.ID == "" {
		return
	}
	if cur := s.Users[u.ID]; cur != nil {
		*cur = u
		return
	}
	cp := u
	s.Users[u.ID] = &cp
}

// observeChannelUsers observes every embedded user of a channel.
func (s *Store) observeChannelUsers(ch *api.Channel) {
	for _, m := range ch.Members {
		s.ObserveUser(m)
	}
}

// ObserveChannel merges ch; never overwrites per-viewer fields from event payloads (events.md 4.4 rule).
// Called outside Apply while a bootstrap is in flight, it is journaled and re-applied over the snapshot.
// A new channel is stored as given; an existing one keeps My, ReadOnly, ReadOnlyReason and
// the newer of the two last-message pointers.
func (s *Store) ObserveChannel(ch api.Channel) {
	if ch.ID == "" {
		return
	}
	if !s.applying {
		s.record(func() { s.ObserveChannel(ch) })
	}
	s.observeChannelUsers(&ch)
	cur := s.Channels[ch.ID]
	if cur == nil {
		cp := ch
		s.Channels[ch.ID] = &cp
		s.syncSpaceChannel(&cp)
		s.insertDM(&cp)
		return
	}
	merged := ch
	merged.My = cur.My
	merged.ReadOnly = cur.ReadOnly
	merged.ReadOnlyReason = cur.ReadOnlyReason
	if cur.LastMessageID != nil && (merged.LastMessageID == nil || *cur.LastMessageID > *merged.LastMessageID) {
		merged.LastMessageID, merged.LastMessageAt = cur.LastMessageID, cur.LastMessageAt
	}
	*cur = merged
	s.syncSpaceChannel(cur)
}

// putChannel stores ch exactly as given (payloads addressed only to me: space.joined, bootstrap).
func (s *Store) putChannel(ch api.Channel) {
	s.observeChannelUsers(&ch)
	if cur := s.Channels[ch.ID]; cur != nil {
		*cur = ch
		s.syncSpaceChannel(cur)
		return
	}
	cp := ch
	s.Channels[ch.ID] = &cp
	s.syncSpaceChannel(&cp)
	s.insertDM(&cp)
}

func isDM(ch *api.Channel) bool {
	return ch.Kind == api.ChannelDM || ch.Kind == api.ChannelGroupDM
}

// dmKey is the DMOrder sort key: last_message_at, else created_at.
func dmKey(ch *api.Channel) time.Time {
	if ch.LastMessageAt != nil {
		return *ch.LastMessageAt
	}
	return ch.CreatedAt
}

// insertDM places a DM not yet in DMOrder by last_message_at desc (ties go first).
func (s *Store) insertDM(ch *api.Channel) {
	if !isDM(ch) {
		return
	}
	for _, id := range s.DMOrder {
		if id == ch.ID {
			return
		}
	}
	key := dmKey(ch)
	i := 0
	for ; i < len(s.DMOrder); i++ {
		o := s.Channels[s.DMOrder[i]]
		if o == nil || !dmKey(o).After(key) {
			break
		}
	}
	s.DMOrder = append(s.DMOrder, "")
	copy(s.DMOrder[i+1:], s.DMOrder[i:])
	s.DMOrder[i] = ch.ID
}

// moveDMFront moves id to DMOrder[0].
func (s *Store) moveDMFront(id string) {
	s.DMOrder = removeString(s.DMOrder, id)
	s.DMOrder = append([]string{id}, s.DMOrder...)
}

func removeString(list []string, v string) []string {
	out := make([]string, 0, len(list))
	for _, x := range list {
		if x != v {
			out = append(out, x)
		}
	}
	return out
}

// syncSpaceChannel mirrors a text channel into its SpaceDetail.Channels copy.
func (s *Store) syncSpaceChannel(ch *api.Channel) {
	if ch.SpaceID == nil {
		return
	}
	sd := s.Spaces[*ch.SpaceID]
	if sd == nil {
		return
	}
	for i := range sd.Channels {
		if sd.Channels[i].ID == ch.ID {
			sd.Channels[i] = *ch
			return
		}
	}
	sd.Channels = append(sd.Channels, *ch)
}

// dropChannel removes a channel everywhere.
func (s *Store) dropChannel(id string) {
	ch := s.Channels[id]
	delete(s.Channels, id)
	s.DMOrder = removeString(s.DMOrder, id)
	if ch != nil && ch.SpaceID != nil {
		if sd := s.Spaces[*ch.SpaceID]; sd != nil {
			out := make([]api.Channel, 0, len(sd.Channels))
			for _, c := range sd.Channels {
				if c.ID != id {
					out = append(out, c)
				}
			}
			sd.Channels = out
		}
	}
}

// dropSpace removes a space and its channels.
func (s *Store) dropSpace(id string) {
	for cid, ch := range s.Channels {
		if ch.SpaceID != nil && *ch.SpaceID == id {
			delete(s.Channels, cid)
		}
	}
	delete(s.Spaces, id)
	s.SpaceOrder = removeString(s.SpaceOrder, id)
}

// User returns the cached user or nil.
func (s *Store) User(id string) *api.UserSummary {
	return s.Users[id]
}

// IsBlocked reports whether I blocked userID.
func (s *Store) IsBlocked(userID string) bool {
	_, ok := s.Blocks[userID]
	return ok
}

// IsMe reports whether userID is me.
func (s *Store) IsMe(userID string) bool {
	return userID != "" && s.Me != nil && s.Me.ID == userID
}

// displayName prefers the cached (newest) user over the embedded copy, and
// falls back to the handle when the screen would show nothing of the name
// (textwidth.Blank: only runes Sanitize drops, e.g. Hangul fillers, zero-width
// runes, noncharacters, or blank-cell runes like U+2800, pending-fixes 1).
func (s *Store) displayName(u api.UserSummary) string {
	if c := s.Users[u.ID]; c != nil {
		u = *c
	}
	if textwidth.Blank(u.DisplayName) {
		return u.Handle
	}
	return u.DisplayName
}

// ChannelLabel returns "#개발", "민지", "점심팟", "민지, 준호, 하나".
// Unknown channels yield "". The result is not sanitized (renderers do that).
func (s *Store) ChannelLabel(id string) string {
	ch := s.Channels[id]
	if ch == nil {
		return ""
	}
	switch ch.Kind {
	case api.ChannelText:
		if ch.Name == nil {
			return "#"
		}
		return "#" + *ch.Name
	case api.ChannelGroupDM:
		if ch.Name != nil && *ch.Name != "" {
			return *ch.Name
		}
	}
	var names []string
	for _, m := range ch.Members {
		if !s.IsMe(m.ID) {
			names = append(names, s.displayName(m))
		}
	}
	if len(names) == 0 && s.Me != nil { // DM with myself
		return s.displayName(s.Me.UserSummary)
	}
	return strings.Join(names, ", ")
}

// SpaceChannels returns the visible text channels of a space, by position (ties by ID).
func (s *Store) SpaceChannels(spaceID string) []*api.Channel {
	var out []*api.Channel
	for _, ch := range s.Channels {
		if ch.Kind == api.ChannelText && ch.SpaceID != nil && *ch.SpaceID == spaceID {
			out = append(out, ch)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Position != out[j].Position {
			return out[i].Position < out[j].Position
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// TotalUnread sums unread and mention counts over every channel (text and DM). Muted
// channels add no unread, but their mentions count. The UI badges use ChannelUnread
// (채팅) and DMUnread (DM) instead (CA 4.5).
func (s *Store) TotalUnread() (unread, mentions int) {
	for _, ch := range s.Channels {
		if !ch.My.Muted {
			unread += ch.My.UnreadCount
		}
		mentions += ch.My.MentionCount
	}
	return unread, mentions
}

// IsDM reports whether id is a known 1:1 or group DM channel.
func (s *Store) IsDM(id string) bool {
	ch := s.Channels[id]
	return ch != nil && (ch.Kind == api.ChannelDM || ch.Kind == api.ChannelGroupDM)
}

// HiddenDM reports a 1:1 DM whose other member I blocked: it is hidden on my side
// (api.md 3.2), also before a resync drops it. Group DMs are never hidden.
func (s *Store) HiddenDM(ch *api.Channel) bool {
	if ch == nil || ch.Kind != api.ChannelDM {
		return false
	}
	for _, m := range ch.Members {
		if !s.IsMe(m.ID) {
			return s.IsBlocked(m.ID)
		}
	}
	return false
}

// DMUnread is TotalUnread over the DMs and group DMs that are not hidden
// (HiddenDM): the DM tab / rail entry badge.
func (s *Store) DMUnread() (unread, mentions int) {
	for _, ch := range s.Channels {
		if (ch.Kind != api.ChannelDM && ch.Kind != api.ChannelGroupDM) || s.HiddenDM(ch) {
			continue
		}
		if !ch.My.Muted {
			unread += ch.My.UnreadCount
		}
		mentions += ch.My.MentionCount
	}
	return unread, mentions
}

// ChannelUnread is TotalUnread over the text channels only (the 채팅 tab badge;
// DMs count on the DM tab).
func (s *Store) ChannelUnread() (unread, mentions int) {
	for _, ch := range s.Channels {
		if ch.Kind == api.ChannelDM || ch.Kind == api.ChannelGroupDM {
			continue
		}
		if !ch.My.Muted {
			unread += ch.My.UnreadCount
		}
		mentions += ch.My.MentionCount
	}
	return unread, mentions
}

// FocusEnded sets Focus=nil, LastFocusSummary=&sum (focus.ended event and EndFocus result both call this; never opens UI).
func (s *Store) FocusEnded(sum api.FocusSummary) {
	s.Focus = nil
	s.Self.Focus = nil
	s.LastFocusSummary = &sum
	for _, a := range sum.Items {
		if a.Actor != nil {
			s.ObserveUser(*a.Actor)
		}
	}
}

// Reset clears everything on logout and notifies reducers (reducer instances are kept).
func (s *Store) Reset() {
	s.clear()
	for _, r := range s.reducerOrder {
		r.Reset()
	}
}

// SetViewing is set by the app each frame from visible routes; used for the unread increment rule.
// It replaces the whole set.
func (s *Store) SetViewing(channelIDs ...string) {
	s.viewing = make(map[string]bool, len(channelIDs))
	for _, id := range channelIDs {
		s.viewing[id] = true
	}
}

// Viewing reports whether channelID is currently visible.
func (s *Store) Viewing(channelID string) bool {
	return s.viewing[channelID]
}
