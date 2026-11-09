package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/patrache/masil/internal/config"
	"github.com/patrache/masil/internal/ids"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
)

// SavedState is state.json (CA 3.5).
type SavedState struct {
	Version     int        `json:"version"` // 1
	UserID      string     `json:"user_id"`
	OpenTabs    []SavedTab `json:"open_tabs"`
	ActiveTab   int        `json:"active_tab"`
	Front       string     `json:"front"` // "tab" | "dm" (the DM surface) | "feed" | "notifications"
	LastSpaceID string     `json:"last_space_id"`
	FeedTab     string     `json:"feed_tab"`    // Feed[0].Arg
	PinnedMode  string     `json:"pinned_mode"` // layout Mode.ID() or "auto"
}

// SavedTab is one tab root.
type SavedTab struct {
	Kind string `json:"kind"`
	ID   string `json:"id,omitempty"`
}

// stateVersion is the only state.json version this build reads and writes.
const stateVersion = 1

// pinAuto is SavedState.PinnedMode without a runtime pin.
const pinAuto = "auto"

// maxStateBytes bounds what readState loads (a hand-edited or hostile file).
const maxStateBytes = 1 << 20

var frontNames = map[Front]string{FrontTab: "tab", FrontFeed: "feed", FrontNotifications: "notifications"}

// frontDM is SavedState.Front for FrontTab on the DM surface (InDM).
const frontDM = "dm"

// feedTabs are the feed surface roots state.json may name (CA 5.3.1 RouteFeed Arg).
var feedTabs = map[string]bool{"following": true, "discover": true}

// savedState is what state.json remembers of m (CA 3.5): the tab roots only (stack
// tops, threads and the DM surface's open DM are not saved), the active tab, Front
// ("dm" when the DM surface is the chat surface in front), the feed root's Arg, the
// runtime pin, and the space of the most recently seen channel tab (else the one
// restored at start).
func (m *Model) savedState() SavedState {
	n := m.nav.clone()
	s := SavedState{
		Version:     stateVersion,
		ActiveTab:   n.Active,
		Front:       frontNames[n.Front],
		LastSpaceID: m.lastSpaceID,
		FeedTab:     n.Feed[0].Arg,
		PinnedMode:  pinAuto,
		OpenTabs:    make([]SavedTab, 0, len(n.Tabs)),
	}
	if m.creds != nil {
		s.UserID = m.creds.UserID
	}
	if s.Front == "" {
		s.Front = frontNames[FrontTab]
	}
	if n.Front == FrontTab && n.InDM {
		s.Front = frontDM
	}
	if m.pin != nil {
		s.PinnedMode = m.pin.ID()
	}
	var seen uint64
	for _, t := range n.Tabs {
		root := t.Stack[0]
		s.OpenTabs = append(s.OpenTabs, SavedTab{Kind: string(root.Kind), ID: root.ID})
		if root.Kind != ui.RouteChannel || t.seen < seen {
			continue
		}
		if ch := m.ctx.Store.Channels[root.ID]; ch != nil && ch.SpaceID != nil && *ch.SpaceID != "" {
			s.LastSpaceID, seen = *ch.SpaceID, t.seen
		}
	}
	return s
}

// restore applies a state.json read for the logged-in user: valid tab roots (home,
// channel with a valid ULID; duplicates and other kinds dropped, at most maxTabs),
// the active tab (0 when it pointed at a dropped entry), Front, the feed root and
// the remembered space. The pin is applied by New (startPin). Anything unusable
// leaves the default navigation.
func (m *Model) restore(s SavedState) {
	n := newNavState()
	var tabs []TabState
	active := 0
	keys := map[string]bool{}
	for i, st := range s.OpenTabs {
		var r ui.Route
		switch ui.RouteKind(st.Kind) {
		case ui.RouteHome:
			r = homeRoute
		case ui.RouteChannel:
			if !ids.Valid(st.ID) {
				continue
			}
			r = ui.Route{Kind: ui.RouteChannel, ID: st.ID}
		default:
			continue
		}
		if keys[r.Key()] || len(tabs) == maxTabs {
			continue
		}
		keys[r.Key()] = true
		if i == s.ActiveTab {
			active = len(tabs)
		}
		tabs = append(tabs, TabState{Stack: []ui.Route{r}, seen: uint64(len(tabs) + 1)})
	}
	if len(tabs) > 0 {
		n.Tabs, n.Active = tabs, active
		n.touch()
	}
	for f, name := range frontNames {
		if s.Front == name {
			n.Front = f
		}
	}
	if s.Front == frontDM {
		n.Front, n.InDM = FrontTab, true
	}
	if feedTabs[s.FeedTab] {
		n.Feed = []ui.Route{{Kind: ui.RouteFeed, Arg: s.FeedTab}}
	}
	m.nav = n
	if ids.Valid(s.LastSpaceID) {
		m.lastSpaceID = s.LastSpaceID
	}
}

// startPin is the pin a session starts with: state.json's pinned_mode (≠ "auto") >
// config layout.pin (≠ "auto") > none. TooSmall and unknown ids are not pins.
func startPin(saved *SavedState, cfgPin string) *layout.Mode {
	for _, id := range []string{savedPin(saved), cfgPin} {
		if pm, ok := layout.ParseMode(id); ok && pm != layout.ModeTooSmall {
			return &pm
		}
	}
	return nil
}

func savedPin(s *SavedState) string {
	if s == nil {
		return ""
	}
	return s.PinnedMode
}

// readState loads state.json for userID. It reports false when the file is missing,
// unreadable, oversized, not one JSON object of this shape, another version, or another
// user's (Review Focus 3: the app then starts with the defaults, no toast).
func readState(p config.Paths, userID string) (SavedState, bool) {
	if p.State == "" || userID == "" {
		return SavedState{}, false
	}
	f, err := os.Open(p.State)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			slog.Warn("state.json unreadable; using defaults", "err", err)
		}
		return SavedState{}, false
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxStateBytes+1))
	if err != nil || len(b) > maxStateBytes {
		slog.Warn("state.json unreadable or too large; using defaults", "err", err, "bytes", len(b))
		return SavedState{}, false
	}
	var s SavedState
	if err := json.Unmarshal(b, &s); err != nil {
		slog.Warn("state.json is not valid; using defaults", "err", err)
		return SavedState{}, false
	}
	if s.Version != stateVersion || s.UserID != userID {
		slog.Debug("state.json ignored", "version", s.Version, "other_user", s.UserID != userID)
		return SavedState{}, false
	}
	return s, true
}

// writeState writes s to state.json atomically (0600). Concurrent writers (two TUIs)
// each rename a complete file: the last write wins.
func writeState(p config.Paths, s SavedState) error {
	if p.State == "" {
		return errors.New("app: state.json path not set")
	}
	b, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("app: encode state.json: %w", err)
	}
	return config.WriteFileAtomic(p.State, append(b, '\n'), 0o600)
}
