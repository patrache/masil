package chat

import (
	"strings"
	"testing"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/ui"
)

// blankNames are display names the screen shows as nothing: only runes
// textwidth.Sanitize drops (Hangul fillers, zero-width, bidi, variation
// selectors, noncharacters) or graphic runes drawn as an empty cell (U+2800
// BRAILLE PATTERN BLANK, which JS trim keeps, so the server accepts it).
var blankNames = []string{"", "\u3164", "\u3164\uffa0\u115f\u1160", "\u200b\u202e", "\u034f\U000e0100",
	"\ufffe", "\uffff\ufdd0", "\u2800", "\u2800 \u2800", "\U0001d159"}

// A display name the screen shows as nothing falls back to the handle, so a
// message never looks like it came from a nameless sender (pending-fixes 1).
func TestDisplayNameInvisibleFallsBack(t *testing.T) {
	for _, name := range blankNames {
		if got := displayName(api.UserSummary{Handle: "sam", DisplayName: name}); got != "sam" {
			t.Errorf("displayName(%q) = %q, want the handle", name, got)
		}
	}
	if got := displayName(api.UserSummary{Handle: "sam", DisplayName: "민지"}); got != "민지" {
		t.Errorf("displayName = %q, want 민지", got)
	}
}

// The direct-invite toast names the invitee by handle when the display name
// shows nothing, never "님을 방에 초대했어요" with no name (pending-fixes 1).
func TestDirectInviteDoneBlankNameFallsBack(t *testing.T) {
	for _, name := range blankNames {
		inv := &api.DirectInvite{Invitee: api.UserSummary{ID: "u1", Handle: "sam", DisplayName: name}}
		inv.Space.Name = "노바"
		msg := directInviteDone(nil, ui.ResultMsg[*api.DirectInvite]{Value: inv})()
		tm, ok := msg.(ui.ToastMsg)
		if !ok {
			t.Fatalf("name %q: got %T, want a toast", name, msg)
		}
		if !strings.HasPrefix(tm.Text, "@sam님을 ") {
			t.Errorf("name %q: toast %q does not start with the handle", name, tm.Text)
		}
	}
}

// A mention candidate whose display name shows nothing is labeled with its
// handle, not an empty row (pending-fixes 1).
func TestMentionCandidateBlankNameLabel(t *testing.T) {
	for _, name := range blankNames {
		ch := &api.Channel{Kind: api.ChannelDM, Members: []api.UserSummary{{ID: "u1", Handle: "sam", DisplayName: name}}}
		got := mentionCandidates(nil, ch, nil, "sa")
		if len(got) != 1 || got[0].Label != "sam" {
			t.Errorf("name %q: completions %+v, want one labeled with the handle", name, got)
		}
	}
}
