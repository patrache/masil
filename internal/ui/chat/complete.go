package chat

import (
	"context"
	"slices"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/slash"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/keymap"
	"github.com/patrache/masil/internal/ui/palette"
	"github.com/patrache/masil/internal/ui/widgets"
)

// Completion constants.
const (
	completionRows    = 5 // popup height cap (the Composer's own cap)
	maxMentionItems   = 8 // @mention candidates, @here included
	maxEmojiItems     = 8 // :emoji candidates
	minEmojiQuery     = 2 // characters after ':' before the popup opens
	memberFetchLimit  = 100
	selectedMark      = "› "
	unselectedMark    = "  "
	hereHandle        = "here"
	hereDetail        = "지금 접속한 사람 모두"
	completionMention = '@'
	completionEmoji   = ':'
	completionCommand = '/'
)

// completer is the composer's completion popup state. chat owns the selection
// (idx): widgets.Composer only gets the visible window, the selected Label
// prefixed with "› " and the others with "  ".
type completer struct {
	kind  byte   // 0 (closed), '@', ':' or '/'
	token string // the completed token, trigger included ("@mi")
	items []widgets.Completion
	idx   int

	members       []api.UserSummary // text channel members (ListChannelMembers, cached)
	membersLoaded bool
	membersBusy   bool

	seen  string               // the value the popup was last computed for
	rows  int                  // popup rows the owner has room for (0 = none)
	shown []widgets.Completion // the window last given to SetCompletion
}

// emojiEntry is one :emoji completion: English name, emoji, Korean name.
type emojiEntry struct{ Name, Emoji, Ko string }

// emojiTable is the :emoji completion set (2-cell emoji without VS16, so the
// inserted text survives input normalization unchanged).
var emojiTable = []emojiEntry{
	{"thumbsup", "👍", "좋아요"},
	{"thumbsdown", "👎", "싫어요"},
	{"eyes", "👀", "눈"},
	{"heart", "💖", "하트"},
	{"tada", "🎉", "축하"},
	{"fire", "🔥", "불"},
	{"pray", "🙏", "부탁"},
	{"joy", "😂", "웃음"},
	{"rocket", "🚀", "로켓"},
	{"rice", "🍚", "밥"},
	{"coffee", "☕", "커피"},
	{"check", "✅", "확인"},
	{"x", "❌", "아니"},
	{"smile", "😀", "미소"},
	{"sweat", "😅", "머쓱"},
	{"sob", "😭", "엉엉"},
	{"angry", "😡", "화남"},
	{"thinking", "🤔", "생각"},
	{"clap", "👏", "박수"},
	{"raised", "🙌", "만세"},
	{"muscle", "💪", "힘내"},
	{"handshake", "🤝", "악수"},
	{"wave", "👋", "안녕"},
	{"ok", "👌", "오케이"},
	{"hundred", "💯", "백점"},
	{"star", "⭐", "별"},
	{"sparkles", "✨", "반짝"},
	{"bulb", "💡", "아이디어"},
	{"bug", "🐛", "버그"},
	{"pin", "📌", "고정"},
	{"speech", "💬", "말풍선"},
	{"beer", "🍺", "맥주"},
	{"cake", "🎂", "생일"},
	{"gift", "🎁", "선물"},
	{"cool", "😎", "멋짐"},
	{"love", "😍", "반함"},
	{"wink", "😉", "윙크"},
	{"cry", "😢", "슬픔"},
	{"sleepy", "😴", "졸림"},
	{"party", "🥳", "파티"},
	{"hug", "🤗", "포옹"},
	{"zap", "⚡", "번개"},
	{"siren", "🚨", "비상"},
	{"hourglass", "⏳", "기다림"},
	{"memo", "📝", "메모"},
	{"calendar", "📅", "일정"},
	{"chicken", "🍗", "치킨"},
	{"pizza", "🍕", "피자"},
}

// completionToken is the token the popup completes: '/' when the whole value is
// "/…" without whitespace, '@' for an "@…" last word, ':' for a ":…" last word
// with at least two characters after the colon; 0 otherwise. Only the end of the
// value completes (SetValue moves the cursor to the end).
func completionToken(v string) (byte, string) {
	if strings.HasPrefix(v, "/") && !strings.ContainsAny(v, " \t\n") {
		return completionCommand, v
	}
	tok := v[tokenStart(v):]
	switch {
	case strings.HasPrefix(tok, "@"):
		return completionMention, tok
	case strings.HasPrefix(tok, ":") && utf8.RuneCountInString(tok[1:]) >= minEmojiQuery:
		return completionEmoji, tok
	}
	return 0, ""
}

// tokenStart is the byte offset of the value's last word (after the last space,
// tab or newline).
func tokenStart(v string) int {
	return strings.LastIndexAny(v, " \t\n") + 1
}

// mentionCandidates are the @mention items for prefix (without '@'): a 1:1 or
// group DM offers Channel.Members, a text channel the fetched members. Me and
// blocked users are left out. A user matches by handle prefix, display name
// substring or display-name chosung prefix (case-insensitive); handle-prefix
// matches come first, then by display name. A text channel or group DM adds
// @here on a prefix match. At most maxMentionItems.
func mentionCandidates(ctx *ui.Context, ch *api.Channel, members []api.UserSummary, prefix string) []widgets.Completion {
	if ch == nil {
		return nil
	}
	pool := members
	if ch.Kind == api.ChannelDM || ch.Kind == api.ChannelGroupDM {
		pool = ch.Members
	}
	p := strings.ToLower(prefix)
	type cand struct {
		u      api.UserSummary
		handle bool
	}
	var cands []cand
	seen := map[string]bool{}
	for _, u := range pool {
		if u.ID == "" || seen[u.ID] {
			continue
		}
		seen[u.ID] = true
		if ctx != nil && ctx.Store != nil {
			if ctx.Store.IsMe(u.ID) || ctx.Store.IsBlocked(u.ID) {
				continue
			}
			if cu := ctx.Store.User(u.ID); cu != nil {
				u = *cu
			}
		}
		handle := strings.HasPrefix(strings.ToLower(u.Handle), p)
		name := strings.ToLower(u.DisplayName)
		if !handle && !strings.Contains(name, p) && !strings.HasPrefix(strings.ToLower(palette.Chosung(u.DisplayName)), p) {
			continue
		}
		cands = append(cands, cand{u, handle})
	}
	slices.SortStableFunc(cands, func(a, b cand) int {
		if a.handle != b.handle {
			if a.handle {
				return -1
			}
			return 1
		}
		if c := strings.Compare(a.u.DisplayName, b.u.DisplayName); c != 0 {
			return c
		}
		return strings.Compare(a.u.Handle, b.u.Handle)
	})
	here := ch.Kind != api.ChannelDM && strings.HasPrefix(hereHandle, p)
	limit := maxMentionItems
	if here {
		limit--
	}
	out := make([]widgets.Completion, 0, min(len(cands), limit)+1)
	for _, c := range cands[:min(len(cands), limit)] {
		out = append(out, widgets.Completion{Label: c.u.DisplayName, Detail: "@" + c.u.Handle, Insert: "@" + c.u.Handle + " "})
	}
	if here {
		out = append(out, widgets.Completion{Label: "@" + hereHandle, Detail: hereDetail, Insert: "@" + hereHandle + " "})
	}
	return out
}

// emojiCandidates are the :emoji items for prefix (without ':'): English name
// prefix or Korean name substring, table order, at most maxEmojiItems.
func emojiCandidates(prefix string) []widgets.Completion {
	p := strings.ToLower(prefix)
	if p == "" {
		return nil
	}
	var out []widgets.Completion
	for _, e := range emojiTable {
		if !strings.HasPrefix(e.Name, p) && !strings.Contains(e.Ko, prefix) {
			continue
		}
		out = append(out, widgets.Completion{Label: e.Emoji + " " + e.Name, Detail: e.Ko, Insert: e.Emoji + " "})
		if len(out) == maxEmojiItems {
			break
		}
	}
	return out
}

// commandCandidates are the /command items whose name starts with prefix
// (without '/'), in slash.Catalog order.
func commandCandidates(prefix string) []widgets.Completion {
	p := strings.ToLower(prefix)
	var out []widgets.Completion
	for _, s := range slash.Catalog() {
		name := string(s.Name)
		if !strings.HasPrefix(name, p) {
			continue
		}
		out = append(out, widgets.Completion{Label: "/" + name, Detail: s.Desc, Insert: "/" + name + " "})
	}
	return out
}

// completing: the composer has the focus and the popup shows at least one item.
func (cc *chatComposer) completing() bool {
	return cc.focused() && len(cc.comp.shown) > 0
}

// recomplete refreshes the popup from the value's end token when the value
// changed since the last time. A text channel's members are fetched once
// (chat.members:<ch>) when a mention starts before they are loaded.
func (cc *chatComposer) recomplete(ctx *ui.Context) tea.Cmd {
	v := cc.c.Value()
	if v == cc.comp.seen {
		return nil
	}
	cc.comp.seen = v
	kind, token := completionToken(v)
	if kind != cc.comp.kind || token != cc.comp.token {
		cc.comp.idx = 0
	}
	cc.comp.kind, cc.comp.token = kind, token
	var cmd tea.Cmd
	if kind == completionMention {
		cmd = cc.fetchMembers(ctx)
	}
	cc.setItems(cc.candidates(ctx))
	return cmd
}

// candidates are the items of the current token.
func (cc *chatComposer) candidates(ctx *ui.Context) []widgets.Completion {
	switch cc.comp.kind {
	case completionMention:
		return mentionCandidates(ctx, cc.channel(ctx), cc.comp.members, cc.comp.token[1:])
	case completionEmoji:
		return emojiCandidates(cc.comp.token[1:])
	case completionCommand:
		return commandCandidates(cc.comp.token[1:])
	}
	return nil
}

// channel is the composer's channel in the Store (nil when unknown).
func (cc *chatComposer) channel(ctx *ui.Context) *api.Channel {
	if ctx == nil || ctx.Store == nil {
		return nil
	}
	return ctx.Store.Channels[cc.channelID]
}

// fetchMembers requests a text channel's members once (a DM has Channel.Members).
func (cc *chatComposer) fetchMembers(ctx *ui.Context) tea.Cmd {
	ch := cc.channel(ctx)
	if ch == nil || ch.Kind != api.ChannelText || cc.comp.membersLoaded || cc.comp.membersBusy {
		return nil
	}
	cc.comp.membersBusy = true
	id := cc.channelID
	return ui.Do(ctx, "chat.members:"+id, func(c context.Context, cl *api.Client) (*api.Page[api.ChannelMember], error) {
		return cl.ListChannelMembers(c, id, api.PageOpts{Limit: memberFetchLimit})
	})
}

// onMembers lands the member list: cached, and the open @mention popup is
// refreshed. A failure leaves the list unloaded (the next mention retries).
func (cc *chatComposer) onMembers(ctx *ui.Context, m ui.ResultMsg[*api.Page[api.ChannelMember]]) {
	if m.Tag != "chat.members:"+cc.channelID {
		return
	}
	cc.comp.membersBusy = false
	if m.Err != nil || m.Value == nil {
		return
	}
	cc.comp.members = cc.comp.members[:0]
	for _, it := range m.Value.Items {
		cc.comp.members = append(cc.comp.members, it.User)
	}
	cc.comp.membersLoaded = true
	if cc.comp.kind == completionMention {
		cc.setItems(cc.candidates(ctx))
	}
}

// setItems replaces the candidates (the selection is clamped) and redraws the popup.
func (cc *chatComposer) setItems(items []widgets.Completion) {
	cc.comp.items = items
	cc.comp.idx = min(max(cc.comp.idx, 0), max(len(items)-1, 0))
	cc.pushCompletion()
}

// closeCompletion closes the popup; the current value counts as seen, so the
// popup stays closed until the text changes.
func (cc *chatComposer) closeCompletion() {
	cc.comp.kind, cc.comp.token, cc.comp.idx = 0, "", 0
	cc.comp.items = nil
	cc.comp.seen = cc.c.Value()
	cc.pushCompletion()
}

// fitPopup sets the popup rows the owner has room for and redraws the popup.
func (cc *chatComposer) fitPopup(room int) {
	room = min(max(room, 0), completionRows)
	if room == cc.comp.rows && len(cc.comp.shown) == min(len(cc.comp.items), room) {
		return
	}
	cc.comp.rows = room
	cc.pushCompletion()
}

// pushCompletion gives the Composer the visible window around the selection,
// the selected Label prefixed "› " and the others "  ".
func (cc *chatComposer) pushCompletion() {
	n := min(len(cc.comp.items), cc.comp.rows)
	if n == 0 {
		cc.comp.shown = nil
		cc.c.SetCompletion(nil)
		return
	}
	start := min(max(cc.comp.idx-n+1, 0), len(cc.comp.items)-n)
	shown := make([]widgets.Completion, 0, n)
	for i := start; i < start+n; i++ {
		it := cc.comp.items[i]
		mark := unselectedMark
		if i == cc.comp.idx {
			mark = selectedMark
		}
		it.Label = mark + it.Label
		shown = append(shown, it)
	}
	cc.comp.shown = shown
	cc.c.SetCompletion(shown)
}

// popupKey handles a key while the popup is open, before the Composer sees it:
// up/down (shift+tab) move the selection, enter/tab accept, esc closes. Other
// keys are not consumed, nor is enter on a fully typed token (it is sent).
func (cc *chatComposer) popupKey(ctx *ui.Context, msg tea.KeyPressMsg) bool {
	km := ctx.Keys
	switch {
	case msg.String() == "up" || km.Matches(msg, keymap.FocusPrev):
		cc.comp.idx = max(cc.comp.idx-1, 0)
	case msg.String() == "down":
		cc.comp.idx = min(cc.comp.idx+1, len(cc.comp.items)-1)
	case km.Matches(msg, keymap.ComposeSend) && cc.exactToken():
		// the token is typed in full already ("/shrug", "/invite @hana"):
		// enter sends instead of only adding a space
		cc.closeCompletion()
		return false
	case km.Matches(msg, keymap.ComposeSend) || km.Matches(msg, keymap.ComposeComplete):
		cc.acceptCompletion()
		return true
	case km.Matches(msg, keymap.ComposeLeave):
		cc.closeCompletion()
		return true
	default:
		return false
	}
	cc.pushCompletion()
	return true
}

// exactToken: the popup's token is exactly the selected item's text, so
// accepting would only add a space (a fully typed /command or @handle).
func (cc *chatComposer) exactToken() bool {
	if cc.comp.kind == 0 || cc.comp.idx < 0 || cc.comp.idx >= len(cc.comp.items) {
		return false
	}
	return strings.EqualFold(cc.comp.items[cc.comp.idx].Insert, cc.comp.token+" ")
}

// acceptCompletion replaces the end token with the selected item's Insert.
func (cc *chatComposer) acceptCompletion() {
	if cc.comp.idx < 0 || cc.comp.idx >= len(cc.comp.items) {
		cc.closeCompletion()
		return
	}
	ins := cc.comp.items[cc.comp.idx].Insert
	v := cc.c.Value()
	start := tokenStart(v)
	if cc.comp.kind == completionCommand {
		start = 0
	}
	cc.c.SetValue(v[:start] + ins)
	cc.closeCompletion()
}
