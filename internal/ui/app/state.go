package app

// SavedState is state.json (CA 3.5).
type SavedState struct {
	Version     int        `json:"version"` // 1
	UserID      string     `json:"user_id"`
	OpenTabs    []SavedTab `json:"open_tabs"`
	ActiveTab   int        `json:"active_tab"`
	Front       string     `json:"front"` // "tab" | "feed" | "notifications"
	LastSpaceID string     `json:"last_space_id"`
	FeedTab     string     `json:"feed_tab"`    // Feed[0].Arg
	PinnedMode  string     `json:"pinned_mode"` // layout Mode.ID() or "auto"
}

// SavedTab is one tab root.
type SavedTab struct {
	Kind string `json:"kind"`
	ID   string `json:"id,omitempty"`
}
