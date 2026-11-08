// Package chatstore is the chat domain store (CA 6.1): per-channel message
// windows, threads, optimistic sends and typing indicators. It plugs into the
// core store as the reducer named "chat" and, like the core store, is mutated
// only on the Bubble Tea Update goroutine.
package chatstore

import (
	"time"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/realtime"
	"github.com/patrache/masil/internal/store"
)

// name is the reducer name ("chat").
const name = "chat"

func init() {
	store.RegisterReducer(func(s *store.Store) store.Reducer { return newChatStore(s) })
}

// ChatStore holds chat state for one core store.
type ChatStore struct {
	s   *store.Store     // me / blocked checks
	now func() time.Time // local receipt clock for typing expiry

	windows map[string]*store.Window[api.Message] // channel id -> top-level message window
	loaded  map[string]bool                       // channel id -> a REST page landed in the window
	threads map[string]*threadState               // root message id -> thread
	open    map[string]openState                  // channel id -> open window (resync refetch target)

	pending map[string][]PendingMessage          // channel id -> optimistic sends, in add order
	polls   map[string]map[string]*api.PollInput // channel id -> nonce -> poll input of a pending /poll

	typing map[typingKey]*typers

	pins       map[string][]api.Message // channel id -> pinned messages, newest pin first
	pinsLoaded map[string]bool          // channel id -> pins came from REST (SetPins) and are kept live
	missing    map[string][]string      // channel id -> in-range message ids we hold no copy of (TakeMissing)
	deleted    map[string]*deletedSet   // channel id -> recently deleted message ids, held or not (deleted.go)

	selectedSpace string
	activeChannel string

	// Resync generations (resync.go). gen is never reset, so results of a
	// refetch started before Reset are ignored.
	gen, applied uint64

	// Page replay log and window generations (pages.go). logSeq and genSeq are
	// never reset, so tickets taken before Reset never match the new session.
	log       []logEntry
	logSeq    uint64
	replaying bool
	wgen      map[string]uint64 // channel id -> window generation
	genSeq    uint64
	stale     map[string]bool // channel id -> a refetch was started and its result has not landed
}

type threadState struct {
	root      *api.Message
	replies   *store.Window[api.Message]
	gen       uint64 // reply window generation (pages.go)
	open      bool
	channelID string // learned from the root or a reply ("" until then)
}

type openState struct {
	anchor   string
	atBottom bool
}

func newChatStore(s *store.Store) *ChatStore {
	c := &ChatStore{s: s, now: time.Now}
	c.clear()
	return c
}

// clear resets every piece of state (the clock is injected, not state, and is kept).
func (c *ChatStore) clear() {
	c.windows = map[string]*store.Window[api.Message]{}
	c.loaded = map[string]bool{}
	c.threads = map[string]*threadState{}
	c.open = map[string]openState{}
	c.pending = map[string][]PendingMessage{}
	c.polls = map[string]map[string]*api.PollInput{}
	c.typing = map[typingKey]*typers{}
	c.pins = map[string][]api.Message{}
	c.pinsLoaded = map[string]bool{}
	c.missing = map[string][]string{}
	c.deleted = map[string]*deletedSet{}
	c.selectedSpace = ""
	c.activeChannel = ""
	c.log = nil
	c.replaying = false
	c.wgen = map[string]uint64{}
	c.stale = map[string]bool{}
	c.applied = c.gen
}

// Get returns the chat reducer registered on s (s.Reducer("chat")).
func Get(s *store.Store) *ChatStore {
	c, _ := s.Reducer(name).(*ChatStore)
	return c
}

// Name is the reducer name.
func (c *ChatStore) Name() string { return name }

// Apply applies a realtime event to chat state (events.md 4.2-4.4; rules in apply.go).
// The core store has already applied the event when reducers run.
func (c *ChatStore) Apply(s *store.Store, ev realtime.Event) {
	c.record(ev)
	c.apply(s, ev)
}

// Reset clears everything (logout / user switch); the instance is kept.
func (c *ChatStore) Reset() { c.clear() }

// SetClock sets the clock used for local typing receipt times (default time.Now).
func (c *ChatStore) SetClock(now func() time.Time) {
	if now == nil {
		now = time.Now
	}
	c.now = now
}

func newMessageWindow() *store.Window[api.Message] {
	return store.NewWindow(
		func(m api.Message) string { return m.ID },
		func(m api.Message) time.Time { return m.CreatedAt },
	)
}

// Window returns the channel's message window, creating an empty one if needed.
func (c *ChatStore) Window(channelID string) *store.Window[api.Message] {
	w := c.windows[channelID]
	if w == nil {
		w = newMessageWindow()
		c.windows[channelID] = w
	}
	return w
}

// HasWindow reports whether a window exists for the channel.
func (c *ChatStore) HasWindow(channelID string) bool {
	_, ok := c.windows[channelID]
	return ok
}

// Loaded reports whether a REST page has landed in the channel window.
func (c *ChatStore) Loaded(channelID string) bool { return c.loaded[channelID] }

// thread returns the thread state for rootID, creating an empty closed one if needed.
func (c *ChatStore) thread(rootID string) *threadState {
	th := c.threads[rootID]
	if th == nil {
		th = &threadState{replies: newMessageWindow()}
		c.threads[rootID] = th
	}
	return th
}

// Thread returns the thread root (nil until loaded) and its reply window (empty if never opened).
func (c *ChatStore) Thread(rootID string) (root *api.Message, replies *store.Window[api.Message]) {
	th := c.thread(rootID)
	return th.root, th.replies
}

// SetOpen marks the channel window open; resync refetches the latest page when
// anchorMessageID is "" and atBottom, else the page around the anchor.
func (c *ChatStore) SetOpen(channelID, anchorMessageID string, atBottom bool) {
	c.open[channelID] = openState{anchor: anchorMessageID, atBottom: atBottom}
}

// SetClosed marks the channel window not open (its cache is dropped on resync);
// the conversation view calls it when the app releases the instance.
func (c *ChatStore) SetClosed(channelID string) { delete(c.open, channelID) }

// SetThreadOpen marks a thread open or closed (replies are only kept live while open).
func (c *ChatStore) SetThreadOpen(rootID string, open bool) {
	if !open {
		if th := c.threads[rootID]; th != nil {
			th.open = false
		}
		return
	}
	c.thread(rootID).open = true
}

// SelectedSpace is the space chosen in the rail ("" = home / DMs).
func (c *ChatStore) SelectedSpace() string { return c.selectedSpace }

// SetSelectedSpace sets the rail selection.
func (c *ChatStore) SetSelectedSpace(id string) { c.selectedSpace = id }

// ActiveChannel is the channel last shown in the main conversation view.
func (c *ChatStore) ActiveChannel() string { return c.activeChannel }

// SetActiveChannel sets the active channel.
func (c *ChatStore) SetActiveChannel(id string) { c.activeChannel = id }
