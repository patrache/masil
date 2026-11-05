// Package store is the shared app state, mutated only on the Bubble Tea Update goroutine (CA 4.5).
package store

import (
	"time"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/realtime"
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
}

// New creates a Store and instantiates every registered reducer (duplicate Name => panic).
func New() *Store {
	s := &Store{}
	s.buildReducers()
	// TODO(core): Task 14 — initialize maps and Alerts ring
	return s
}

// ObserveUser updates the user cache, keeping the newest.
func (s *Store) ObserveUser(u api.UserSummary) {
	// TODO(core): Task 14
}

// ObserveChannel merges ch; never overwrites per-viewer fields from event payloads (events.md 4.4 rule).
func (s *Store) ObserveChannel(ch api.Channel) {
	// TODO(core): Task 14
}

// User returns the cached user or nil.
func (s *Store) User(id string) *api.UserSummary {
	// TODO(core): Task 14
	return nil
}

// IsBlocked reports whether I blocked userID.
func (s *Store) IsBlocked(userID string) bool {
	// TODO(core): Task 14
	return false
}

// IsMe reports whether userID is me.
func (s *Store) IsMe(userID string) bool {
	// TODO(core): Task 14
	return false
}

// ChannelLabel returns "#개발", "민지", "점심팟", "민지, 준호, 하나".
func (s *Store) ChannelLabel(id string) string {
	// TODO(core): Task 14
	return ""
}

// SpaceChannels returns the visible channels of a space, by position.
func (s *Store) SpaceChannels(spaceID string) []*api.Channel {
	// TODO(core): Task 14
	return nil
}

// TotalUnread sums unread and mention counts.
func (s *Store) TotalUnread() (unread, mentions int) {
	// TODO(core): Task 14
	return 0, 0
}

// FocusEnded sets Focus=nil, LastFocusSummary=&sum (focus.ended event and EndFocus result both call this; never opens UI).
func (s *Store) FocusEnded(sum api.FocusSummary) {
	// TODO(core): Task 14
}

// Reset clears everything on logout and notifies reducers.
func (s *Store) Reset() {
	// TODO(core): Task 14
}

// SetViewing is set by the app each frame from visible routes; used for the unread increment rule.
func (s *Store) SetViewing(channelIDs ...string) {
	// TODO(core): Task 14
}

// Viewing reports whether channelID is currently visible.
func (s *Store) Viewing(channelID string) bool {
	// TODO(core): Task 14
	return false
}
