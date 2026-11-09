// Package keymap maps stable action ids to key bindings (CA 5.5).
package keymap

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// Action is a stable action id, used in config [keys].
type Action string

// Actions (values = CA 5.5 ids).
const (
	AppQuit           Action = "app.quit"
	PaletteOpen       Action = "palette.open"
	SearchGlobal      Action = "search.global"
	NotificationsOpen Action = "notifications.open"
	WindowPrefix      Action = "window.prefix"
	FocusNext         Action = "focus.next"
	FocusPrev         Action = "focus.prev"
	HelpToggle        Action = "help.toggle"
	FeedToggle        Action = "feed.toggle"
	TabPrev           Action = "tab.prev"
	TabNext           Action = "tab.next"
	NavBack           Action = "nav.back"
	NavHistoryBack    Action = "nav.history_back"
	NavHistoryForward Action = "nav.history_forward"
	ListUp            Action = "list.up"
	ListDown          Action = "list.down"
	ListTop           Action = "list.top"
	ListBottom        Action = "list.bottom"
	ListPageUp        Action = "list.pageup"
	ListPageDown      Action = "list.pagedown"
	ListOpen          Action = "list.open"
	ComposeFocus      Action = "compose.focus"
	ComposeSend       Action = "compose.send"
	ComposeNewline    Action = "compose.newline"
	ComposeComplete   Action = "compose.complete"
	ComposeEditLast   Action = "compose.edit_last"
	ComposeLeave      Action = "compose.leave"
	MsgThread         Action = "msg.thread"
	MsgReact          Action = "msg.react"
	MsgEdit           Action = "msg.edit"
	MsgDelete         Action = "msg.delete"
	MsgPin            Action = "msg.pin"
	MsgCopy           Action = "msg.copy"
	MsgActions        Action = "msg.actions"
	ChannelSearch     Action = "channel.search"
	ThreadChannel     Action = "thread.channel"
	PostLike          Action = "post.like"
	PostRepost        Action = "post.repost"
	PostQuote         Action = "post.quote"
	PostReply         Action = "post.reply"
	PostOpenMedia     Action = "post.open_media"
	PostAuthor        Action = "post.author"
	PostCompose       Action = "post.compose"
)

// Keymap maps actions to bindings.
type Keymap struct {
	b map[Action]key.Binding // action -> key.Binding
}

// spec is one default row: keys, help key label ("" = keys joined by "/"), Korean description.
type spec struct {
	keys    []string
	helpKey string
	desc    string
}

// defaults is the CA 5.5 table. Help descriptions are this plan's decision (Task 19).
var defaults = map[Action]spec{
	AppQuit:           {[]string{"ctrl+c"}, "", "종료"},
	PaletteOpen:       {[]string{"ctrl+k"}, "", "이동"},
	SearchGlobal:      {[]string{"ctrl+f"}, "", "검색"},
	NotificationsOpen: {[]string{"ctrl+n"}, "", "알림"},
	WindowPrefix:      {[]string{"ctrl+w"}, "", "창"},
	FocusNext:         {[]string{"tab"}, "", "다음 패널"},
	FocusPrev:         {[]string{"shift+tab"}, "", "이전 패널"},
	HelpToggle:        {[]string{"?", "f1"}, "", "도움말"},
	FeedToggle:        {[]string{"f"}, "", "피드"},
	TabPrev:           {[]string{"["}, "", "이전 탭"},
	TabNext:           {[]string{"]"}, "", "다음 탭"},
	NavBack:           {[]string{"esc"}, "", "뒤로"},
	// ctrl+o is the same byte in every terminal and no composer edit key, so it also
	// works while a chat message is being typed; alt+left is CSI 1;3D (iTerm2, kitty,
	// Ghostty, WezTerm), alt+b what macOS Terminal.app sends for option+left (ESC b).
	NavHistoryBack:    {[]string{"ctrl+o", "alt+left", "alt+b"}, "ctrl+o/alt+←", "이전 위치"},
	NavHistoryForward: {[]string{"alt+right", "alt+f"}, "alt+→", "다음 위치"},
	ListUp:            {[]string{"k", "up"}, "", "위"},
	ListDown:          {[]string{"j", "down"}, "", "아래"},
	ListTop:           {[]string{"home"}, "gg/home", "맨 위"}, // "g g" is a sequence handled by widgets.List
	ListBottom:        {[]string{"G", "end"}, "", "맨 아래"},
	ListPageUp:        {[]string{"ctrl+u", "pgup"}, "", "한 쪽 위"},
	ListPageDown:      {[]string{"ctrl+d", "pgdown"}, "", "한 쪽 아래"},
	ListOpen:          {[]string{"enter"}, "", "열기"},
	ComposeFocus:      {[]string{"i", "a"}, "", "입력"},
	ComposeSend:       {[]string{"enter"}, "", "보내기"},
	ComposeNewline:    {[]string{"shift+enter", "alt+enter", "ctrl+j"}, "", "줄바꿈"},
	ComposeComplete:   {[]string{"tab"}, "", "자동완성"},
	ComposeEditLast:   {[]string{"up"}, "", "마지막 글 고치기"},
	ComposeLeave:      {[]string{"esc"}, "", "나가기"},
	MsgThread:         {[]string{"t", "enter"}, "", "스레드"},
	MsgReact:          {[]string{"r"}, "", "리액션"},
	MsgEdit:           {[]string{"e"}, "", "수정"},
	MsgDelete:         {[]string{"d"}, "", "삭제"},
	MsgPin:            {[]string{"p"}, "", "고정"},
	MsgCopy:           {[]string{"y"}, "", "복사"},
	MsgActions:        {[]string{"m"}, "", "메뉴"},
	ChannelSearch:     {[]string{"/"}, "", "채널 검색"},
	ThreadChannel:     {[]string{"c"}, "", "스레드에서 채널로"},
	PostLike:          {[]string{"l"}, "", "좋아요"},
	PostRepost:        {[]string{"b"}, "", "리포스트"},
	PostQuote:         {[]string{"q"}, "", "인용"},
	PostReply:         {[]string{"r"}, "", "답글"},
	PostOpenMedia:     {[]string{"o"}, "", "이미지"},
	PostAuthor:        {[]string{"u"}, "", "작성자"},
	PostCompose:       {[]string{"c"}, "", "글쓰기"},
}

func newBinding(keys []string, helpKey, desc string) key.Binding {
	if helpKey == "" {
		helpKey = strings.Join(keys, "/")
	}
	return key.NewBinding(key.WithKeys(slices.Clone(keys)...), key.WithHelp(helpKey, desc))
}

// Default returns the CA 5.5 default bindings (a fresh Keymap each call).
func Default() *Keymap {
	k := &Keymap{b: make(map[Action]key.Binding, len(defaults))}
	for a, s := range defaults {
		k.b[a] = newBinding(s.keys, s.helpKey, s.desc)
	}
	return k
}

// Load applies overrides; unknown action/invalid key => error, default kept.
// An override replaces all keys of its action; the help label becomes the
// new keys joined by "/" and the Korean description is kept. One invalid key
// (or an empty list) rejects that action's whole override. Errors are sorted
// by action id.
func Load(overrides map[string][]string) (*Keymap, []error) {
	k := Default()
	ids := make([]string, 0, len(overrides))
	for id := range overrides {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var errs []error
	for _, id := range ids {
		a := Action(id)
		s, ok := defaults[a]
		if !ok {
			errs = append(errs, fmt.Errorf("keymap: unknown action %q", id))
			continue
		}
		keys := overrides[id]
		if len(keys) == 0 {
			errs = append(errs, fmt.Errorf("keymap: %s: no keys given (default kept)", id))
			continue
		}
		var bad []string
		for _, kn := range keys {
			if !validKey(kn) {
				bad = append(bad, strconv.Quote(kn))
			}
		}
		if len(bad) > 0 {
			errs = append(errs, fmt.Errorf("keymap: %s: invalid key %s (default kept)", id, strings.Join(bad, ", ")))
			continue
		}
		k.b[a] = newBinding(keys, "", s.desc)
	}
	return k, errs
}

// namedKeys are the non-character key names tea.KeyPressMsg.String() produces.
var namedKeys = func() map[string]bool {
	m := map[string]bool{}
	for _, n := range []string{"enter", "esc", "tab", "space", "backspace", "up", "down", "left", "right",
		"home", "end", "pgup", "pgdown", "delete", "insert"} {
		m[n] = true
	}
	for i := 1; i <= 12; i++ {
		m["f"+strconv.Itoa(i)] = true
	}
	return m
}()

// validKey reports whether name is a key string tea.KeyPressMsg.String() can
// produce: [ctrl+][alt+][shift+]<key>, modifiers in that order, <key> = one
// printable grapheme (not a space) or a named key (enter, esc, tab, space,
// backspace, arrows, home, end, pgup, pgdown, delete, insert, f1..f12).
func validKey(name string) bool {
	rest := name
	for _, m := range []string{"ctrl+", "alt+", "shift+"} {
		rest = strings.TrimPrefix(rest, m)
	}
	if rest == "" {
		return false
	}
	if namedKeys[rest] {
		return true
	}
	// "+" itself is a valid single-character key ("ctrl++" is not produced by
	// tea, but a bare "+" is); anything else containing "+" is malformed.
	if rest != "+" && strings.Contains(rest, "+") {
		return false
	}
	if ansi.StringWidth(rest) == 0 || strings.ContainsAny(rest, " \t\r\n") {
		return false
	}
	return !hasMultipleGraphemes(rest)
}

func hasMultipleGraphemes(s string) bool {
	g, _ := ansi.FirstGraphemeCluster(s, ansi.GraphemeWidth)
	return len(g) != len(s)
}

// lookup returns the binding for a; a nil Keymap behaves like Default.
func (k *Keymap) lookup(a Action) (key.Binding, bool) {
	if k == nil || k.b == nil {
		s, ok := defaults[a]
		if !ok {
			return key.Binding{}, false
		}
		return newBinding(s.keys, s.helpKey, s.desc), true
	}
	b, ok := k.b[a]
	return b, ok
}

// Binding returns the binding for a (zero, disabled Binding for an unknown action).
func (k *Keymap) Binding(a Action) key.Binding {
	b, _ := k.lookup(a)
	return b
}

// Matches reports whether msg triggers a.
func (k *Keymap) Matches(msg tea.KeyPressMsg, a Action) bool {
	b, ok := k.lookup(a)
	return ok && key.Matches(msg, b)
}

// Bound reports whether msg triggers any action (a nil Keymap behaves like Default).
func (k *Keymap) Bound(msg tea.KeyPressMsg) bool {
	if k == nil || k.b == nil {
		k = Default()
	}
	for _, b := range k.b {
		if key.Matches(msg, b) {
			return true
		}
	}
	return false
}

// Help returns the bindings for as, for help/status hints (unknown actions skipped).
func (k *Keymap) Help(as ...Action) []key.Binding {
	out := make([]key.Binding, 0, len(as))
	for _, a := range as {
		if b, ok := k.lookup(a); ok {
			out = append(out, b)
		}
	}
	return out
}
