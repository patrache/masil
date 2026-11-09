package chat

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/realtime"
	"github.com/patrache/masil/internal/store"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
	"github.com/patrache/masil/internal/ui/widgets"
)

// popupItems is the full candidate list of the channel composer's popup.
func popupItems(t testing.TB, h *uitest.Harness) []widgets.Completion {
	t.Helper()
	return channelOf(t, h).composer.comp.items
}

// hasDetail reports whether some item's Detail is d.
func hasDetail(items []widgets.Completion, d string) bool {
	for _, it := range items {
		if it.Detail == d {
			return true
		}
	}
	return false
}

func TestCompletionTabAccept(t *testing.T) { // requires CR-1 (ui.Completer) in core
	for _, sz := range []uitest.Size{{W: 120, H: 40}, {W: 80, H: 24}} { // multi-pane ring and single-pane (tab.next) modes
		h := devChannel(t, sz)
		h.Press("i")
		h.Type("@mi")
		f0 := h.Model.Focus()
		h.Press("tab")
		if v := channelOf(t, h).composer.c.Value(); v != "@minji " {
			t.Fatalf("%v: got %q", sz, v)
		}
		if f := h.Model.Focus(); f != f0 {
			t.Fatalf("%v: tab accepting the popup must not move focus: %+v -> %+v", sz, f0, f)
		}
		h.Press("tab") // popup closed now → hard global again (focus.next / tab.next), composer value unchanged
		if channelOf(t, h).composer.c.Value() != "@minji " {
			t.Fatal("tab without popup must not touch the draft")
		}
		if f := h.Model.Focus(); f == f0 {
			t.Fatalf("%v: tab without popup is the hard global (focus.next / tab.next), focus stayed %+v", sz, f)
		}
	}
}

func TestMentionCompletion(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 120, H: 40})
	h.Press("i")
	h.Type("@mi")
	mustContain(t, h.Screen(), "› 민지", "@minji") // chat prefixes the selected Label with "› "
	if !channelOf(t, h).Completing() {
		t.Fatal("Completing() reports the open popup (CR-1)")
	}
	h.Press("enter") // accept with enter: tab only reaches the composer once CR-1 lands (see TestCompletionTabAccept)
	if v := channelOf(t, h).composer.c.Value(); v != "@minji " || channelOf(t, h).Completing() {
		t.Fatalf("got %q", v)
	}
	if n := len(reqs(h.API, "POST", "/v1/channels/"+devID(t, h)+"/messages")); n != 0 {
		t.Fatalf("accepting must not send, got %d POST", n)
	}
	// the text channel's member list is fetched once and cached
	h.Type("@ju")
	if !hasDetail(popupItems(t, h), "@junho") {
		t.Fatalf("second mention offers junho: %+v", popupItems(t, h))
	}
	if n := len(reqs(h.API, "GET", "/v1/channels/"+devID(t, h)+"/members")); n != 1 {
		t.Fatalf("members fetched %d times, want 1", n)
	}
}

func TestMentionByDisplayNameAndChosung(t *testing.T) {
	for _, in := range []string{"@민", "@ㅁㅈ", "@MI"} {
		h := devChannel(t, uitest.Size{W: 120, H: 40})
		h.Press("i")
		h.Type(in)
		items := popupItems(t, h)
		if len(items) == 0 || items[0].Detail != "@minji" || items[0].Insert != "@minji " {
			t.Fatalf("%q: want @minji first, got %+v", in, items)
		}
	}
	// handle prefix matches sort before display-name matches
	ctx := devChannel(t, uitest.Size{W: 120, H: 40}).Context()
	ch := ctx.Store.Channels[fixtures.ChDev]
	members := []api.UserSummary{
		{ID: "01JAX0000000000000000000A1", Handle: "zed", DisplayName: "가나"},
		{ID: "01JAX0000000000000000000A2", Handle: "ga", DisplayName: "하늘"},
	}
	got := mentionCandidates(ctx, ch, members, "ga")
	if len(got) < 1 || got[0].Detail != "@ga" {
		t.Fatalf("handle prefix first: %+v", got)
	}
}

func TestMentionExcludesBlocked(t *testing.T) {
	seed := func(s *store.Store) {
		bootstrappedSeed(s)
		s.Blocks[fixtures.UserJunho] = uitest.Start
	}
	h := open(t, uitest.Size{W: 120, H: 40}, ui.Route{Kind: ui.RouteChannel, ID: fixtures.ChDev}, uitest.WithSeed(seed))
	h.Press("i")
	h.Type("@ju")
	if hasDetail(popupItems(t, h), "@junho") || channelOf(t, h).Completing() {
		t.Fatalf("blocked junho offered: %+v", popupItems(t, h))
	}
	h.Press("backspace", "backspace")
	items := popupItems(t, h)
	if hasDetail(items, "@junho") || hasDetail(items, "@sam") || !hasDetail(items, "@minji") {
		t.Fatalf("\"@\" offers everyone but me and the blocked: %+v", items)
	}
}

func TestHereCompletion(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 120, H: 40})
	h.Press("i")
	h.Type("@he")
	items := popupItems(t, h)
	if len(items) == 0 || items[0].Label != "@here" || items[0].Insert != "@here " || items[0].Detail != "지금 접속한 사람 모두" {
		t.Fatalf("text channel offers @here: %+v", items)
	}

	dm := dmWith(t, seeded(), "minji")
	d := open(t, uitest.Size{W: 120, H: 40}, ui.Route{Kind: ui.RouteChannel, ID: dm}, uitest.WithSeed(bootstrappedSeed))
	d.Press("i")
	d.Type("@he")
	if len(popupItems(t, d)) != 0 {
		t.Fatalf("1:1 DM offers no @here: %+v", popupItems(t, d))
	}
	d.Press("backspace", "backspace")
	if items := popupItems(t, d); len(items) != 1 || items[0].Detail != "@minji" {
		t.Fatalf("1:1 DM offers its other member only: %+v", items)
	}
	if n := len(reqs(d.API, "GET", "/v1/channels/"+dm+"/members")); n != 0 {
		t.Fatalf("a DM uses Channel.Members, got %d member requests", n)
	}
}

func TestEmojiCompletion(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 120, H: 40})
	h.Press("i")
	h.Type(":thu")
	h.Press("enter")
	if v := channelOf(t, h).composer.c.Value(); v != "👍 " {
		t.Fatalf("enter accepts while popup open, got %q", v)
	}
	if len(reqs(h.API, "POST", "/v1/channels/"+devID(t, h)+"/messages")) != 0 {
		t.Fatal("must not send")
	}
	// Korean names match by containment; one character after ':' is not enough
	if got := emojiCandidates("축하"); len(got) == 0 || got[0].Insert != "🎉 " {
		t.Fatalf("축하: %+v", got)
	}
	h.Type("밥 :t")
	if channelOf(t, h).Completing() {
		t.Fatal(":t (one character) must not open the popup")
	}
}

func TestEmojiTable(t *testing.T) {
	if len(emojiTable) < 40 {
		t.Fatalf("emojiTable has %d entries, want >= 40", len(emojiTable))
	}
	seen := map[string]bool{}
	for _, e := range emojiTable {
		if e.Name == "" || e.Ko == "" || e.Name != strings.ToLower(e.Name) || seen[e.Name] {
			t.Fatalf("bad or duplicate name: %+v", e)
		}
		seen[e.Name] = true
		if textwidth.SanitizeInput(e.Emoji) != e.Emoji || textwidth.Width(e.Emoji) != 2 {
			t.Fatalf("%+v: emoji must survive input normalization and be 2 cells wide", e)
		}
	}
}

func TestCommandCompletion(t *testing.T) {
	var labels []string
	for _, c := range commandCandidates("") {
		labels = append(labels, c.Label)
		if c.Detail == "" || c.Insert != c.Label+" " {
			t.Fatalf("command item: %+v", c)
		}
	}
	if strings.Join(labels, " ") != "/me /shrug /poll /status /focus /roll /invite" {
		t.Fatalf("\"/\" lists %q", labels)
	}
	h := devChannel(t, uitest.Size{W: 120, H: 40})
	h.Press("i")
	h.Type("/")
	if n := len(popupItems(t, h)); n != 7 {
		t.Fatalf("\"/\" popup has %d items, want 7", n)
	}
	h.Type("ro")
	h.Press("enter")
	if v := channelOf(t, h).composer.c.Value(); v != "/roll " {
		t.Fatalf("got %q", v)
	}
	if len(reqs(h.API, "POST", "/v1/channels/"+devID(t, h)+"/messages")) != 0 {
		t.Fatal("accepting a command must not send")
	}
	// a slash after a space is no command
	h.Type("2d6 /m")
	if channelOf(t, h).Completing() {
		t.Fatal("\"/m\" after text is not a command")
	}
}

func TestCompletionNavigateAndEsc(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 120, H: 40})
	h.Press("i")
	h.Type("@")
	v := channelOf(t, h)
	shown := v.composer.comp.shown
	if len(shown) < 2 || !strings.HasPrefix(shown[0].Label, "› ") {
		t.Fatalf("first item selected: %+v", shown)
	}
	for _, it := range shown[1:] {
		if !strings.HasPrefix(it.Label, "  ") {
			t.Fatalf("other items are indented: %+v", shown)
		}
	}
	second := strings.TrimPrefix(shown[1].Label, "  ")
	h.Press("down")
	shown = v.composer.comp.shown
	if !strings.HasPrefix(shown[1].Label, "› ") || !strings.HasPrefix(shown[0].Label, "  ") {
		t.Fatalf("down moves the marker to the 2nd item: %+v", shown)
	}
	mustContain(t, h.Screen(), "› "+second)
	h.Press("up", "up") // clamps at the first item
	if !strings.HasPrefix(v.composer.comp.shown[0].Label, "› ") {
		t.Fatalf("up clamps: %+v", v.composer.comp.shown)
	}
	h.Press("esc")
	if v.Completing() || !v.CapturesInput() {
		t.Fatalf("esc closes the popup (Completing %v) and keeps the focus (CapturesInput %v)", v.Completing(), v.CapturesInput())
	}
	if v.composer.c.Value() != "@" {
		t.Fatalf("esc keeps the draft, got %q", v.composer.c.Value())
	}
	if strings.Contains(h.Screen(), "› ") && strings.Contains(h.Screen(), "@dev_kim") {
		t.Fatal("popup still drawn after esc")
	}
	h.Press("esc")
	if v.CapturesInput() {
		t.Fatal("second esc leaves the composer")
	}
}

func TestCompletionFitsSmallPane(t *testing.T) {
	// every size: the popup never pushes the composer box out of the pane, and
	// the real cursor stays on the input row.
	forEachSize(t, uitest.AllSizes(), devChannel, func(t *testing.T, h *uitest.Harness) {
		if h.Context().Mode == layout.ModeTooSmall {
			return
		}
		h.Press("i")
		h.Type("@")
		h.Check()
		v := channelOf(t, h)
		if v.size.H <= 0 {
			return
		}
		h.Screen()
		if got := v.composer.height(v.size.W); got > v.size.H-1 {
			t.Fatalf("composer+popup height %d leaves no list line in a %d-row pane", got, v.size.H)
		}
		lines := strings.Split(h.Screen(), "\n")
		c := h.Cursor()
		if c == nil || c.Y < 0 || c.Y >= len(lines) || c.X < 1 || ansi.Cut(lines[c.Y], c.X-1, c.X) != "@" {
			t.Fatalf("cursor %+v is not right after the typed \"@\"", c)
		}
	})
}

func TestCompletionGolden(t *testing.T) {
	forEachSize(t, uitest.Required, devChannel, func(t *testing.T, h *uitest.Harness) {
		h.Press("i")
		h.Type("@")
		h.Golden("mention_popup")
	})
}

func TestCompletionEnterOnExactTokenSends(t *testing.T) {
	// accepting a fully typed token would only add a space: enter sends instead
	h := devChannel(t, uitest.Size{W: 120, H: 40})
	h.Press("i")
	h.Type("@minji")
	if !channelOf(t, h).Completing() {
		t.Fatal("the @mention popup is open")
	}
	h.Press("enter")
	r := reqs(h.API, "POST", "/v1/channels/"+devID(t, h)+"/messages")
	if len(r) != 1 || jsonField(r[0].Body, "body") != "@minji" {
		t.Fatalf("enter on a full @handle sends it: %+v", r)
	}
	h.Type("@mi") // a prefix still completes
	h.Press("enter")
	if v := channelOf(t, h).composer.c.Value(); v != "@minji " {
		t.Fatalf("got %q", v)
	}
}

func TestMentionMembersFollowMembership(t *testing.T) {
	srv := newAPI(t)
	dev := chanID(t, seeded(), "개발")
	users := seededMembers(t)
	liveMembers(srv, dev, &users)
	h := openWith(t, uitest.Size{W: 120, H: 40}, ui.Route{Kind: ui.RouteChannel, ID: dev}, srv, uitest.WithSeed(bootstrappedSeed))
	membersGets := func() int { return len(reqs(srv, "GET", "/v1/channels/"+dev+"/members")) }
	h.Press("i")
	h.Type("@mi")
	if !hasDetail(popupItems(t, h), "@minji") || membersGets() != 1 {
		t.Fatalf("precondition: %+v (%d GETs)", popupItems(t, h), membersGets())
	}

	// 민지 is kicked from the space while the popup is open: she leaves it
	users = withoutID(users, fixtures.UserMinji)
	h.Event("space.member_left", realtime.SpaceMemberLeft{SpaceID: fixtures.SpaceNova, UserID: fixtures.UserMinji, Reason: "kicked"})
	if hasDetail(popupItems(t, h), "@minji") {
		t.Fatalf("a departed member is still offered: %+v", popupItems(t, h))
	}

	// a newcomer joins the channel: the next mention offers them
	users = append(users, newbie)
	h.Event("channel.member_added", realtime.ChannelMemberAdded{ChannelID: dev, User: newbie})
	h.Type(" @ne")
	if !hasDetail(popupItems(t, h), "@newbie") {
		t.Fatalf("a member who joined cannot be mentioned: %+v", popupItems(t, h))
	}
	n := membersGets()
	h.Type("w")
	if membersGets() != n {
		t.Fatal("the refetched list is cached again")
	}
}

func TestEmojiCompletionSmoking(t *testing.T) {
	for _, q := range []string{"smok", "흡연"} {
		found := false
		for _, c := range emojiCandidates(q) {
			if strings.Contains(c.Insert, "🚬") {
				found = true
			}
		}
		if !found {
			t.Fatalf(":%s does not offer 🚬", q)
		}
	}
}
