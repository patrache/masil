package keymap_test

import (
	"reflect"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/ui/keymap"
)

// allActions lists every Action constant (CA 5.5 table order).
var allActions = []keymap.Action{
	keymap.AppQuit, keymap.PaletteOpen, keymap.SearchGlobal, keymap.NotificationsOpen, keymap.WindowPrefix,
	keymap.FocusNext, keymap.FocusPrev, keymap.HelpToggle, keymap.FeedToggle, keymap.TabPrev, keymap.TabNext,
	keymap.NavBack, keymap.ListUp, keymap.ListDown, keymap.ListTop, keymap.ListBottom, keymap.ListPageUp,
	keymap.ListPageDown, keymap.ListOpen, keymap.ComposeFocus, keymap.ComposeSend, keymap.ComposeNewline,
	keymap.ComposeComplete, keymap.ComposeEditLast, keymap.ComposeLeave, keymap.MsgThread, keymap.MsgReact,
	keymap.MsgEdit, keymap.MsgDelete, keymap.MsgPin, keymap.MsgCopy, keymap.MsgActions, keymap.ChannelSearch,
	keymap.PostLike, keymap.PostRepost, keymap.PostQuote, keymap.PostReply, keymap.PostOpenMedia,
	keymap.PostAuthor, keymap.PostCompose,
}

func TestDefaultBindings(t *testing.T) {
	want := map[keymap.Action][]string{
		keymap.AppQuit:           {"ctrl+c"},
		keymap.PaletteOpen:       {"ctrl+k"},
		keymap.SearchGlobal:      {"ctrl+f"},
		keymap.NotificationsOpen: {"ctrl+n"},
		keymap.WindowPrefix:      {"ctrl+w"},
		keymap.FocusNext:         {"tab"},
		keymap.FocusPrev:         {"shift+tab"},
		keymap.HelpToggle:        {"?", "f1"},
		keymap.FeedToggle:        {"f"},
		keymap.TabPrev:           {"["},
		keymap.TabNext:           {"]"},
		keymap.NavBack:           {"esc"},
		keymap.ListUp:            {"k", "up"},
		keymap.ListDown:          {"j", "down"},
		keymap.ListTop:           {"home"}, // "g g" is a two-key sequence handled by widgets.List
		keymap.ListBottom:        {"G", "end"},
		keymap.ListPageUp:        {"ctrl+u", "pgup"},
		keymap.ListPageDown:      {"ctrl+d", "pgdown"},
		keymap.ListOpen:          {"enter"},
		keymap.ComposeFocus:      {"i", "a"},
		keymap.ComposeSend:       {"enter"},
		keymap.ComposeNewline:    {"shift+enter", "alt+enter", "ctrl+j"},
		keymap.ComposeComplete:   {"tab"},
		keymap.ComposeEditLast:   {"up"},
		keymap.ComposeLeave:      {"esc"},
		keymap.MsgThread:         {"t", "enter"},
		keymap.MsgReact:          {"r"},
		keymap.MsgEdit:           {"e"},
		keymap.MsgDelete:         {"d"},
		keymap.MsgPin:            {"p"},
		keymap.MsgCopy:           {"y"},
		keymap.MsgActions:        {"m"},
		keymap.ChannelSearch:     {"/"},
		keymap.PostLike:          {"l"},
		keymap.PostRepost:        {"b"},
		keymap.PostQuote:         {"q"},
		keymap.PostReply:         {"r"},
		keymap.PostOpenMedia:     {"o"},
		keymap.PostAuthor:        {"u"},
		keymap.PostCompose:       {"c"},
	}
	if len(want) != len(allActions) {
		t.Fatalf("table covers %d actions, want %d", len(want), len(allActions))
	}
	km := keymap.Default()
	if km == nil {
		t.Fatal("Default() is nil")
	}
	for _, a := range allActions {
		got := km.Binding(a).Keys()
		if !reflect.DeepEqual(got, want[a]) {
			t.Errorf("%s keys = %q, want %q", a, got, want[a])
		}
		if !km.Binding(a).Enabled() {
			t.Errorf("%s binding disabled", a)
		}
	}
	if h := km.Binding(keymap.ListTop).Help().Key; h != "gg/home" {
		t.Errorf("list.top help key = %q, want gg/home", h)
	}
	if h := km.Binding(keymap.ListUp).Help().Key; h != "k/up" {
		t.Errorf("list.up help key = %q, want k/up", h)
	}
	// Default returns an independent copy.
	a, b := keymap.Default(), keymap.Default()
	if a == b {
		t.Error("Default() must return a fresh Keymap")
	}
}

func press(code rune, text string, mod tea.KeyMod) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: code, Text: text, Mod: mod}
}

func TestMatches(t *testing.T) {
	km := keymap.Default()
	ctrlK := press('k', "", tea.ModCtrl)
	if !km.Matches(ctrlK, keymap.PaletteOpen) {
		t.Error("ctrl+k must match palette.open")
	}
	if km.Matches(ctrlK, keymap.ListUp) {
		t.Error("ctrl+k must not match list.up")
	}
	if !km.Matches(press('k', "k", 0), keymap.ListUp) || !km.Matches(press(tea.KeyUp, "", 0), keymap.ListUp) {
		t.Error("k and up must match list.up")
	}
	if !km.Matches(press('g', "G", tea.ModShift), keymap.ListBottom) {
		t.Error("G must match list.bottom")
	}
	if !km.Matches(press(tea.KeyEnter, "", tea.ModShift), keymap.ComposeNewline) {
		t.Error("shift+enter must match compose.newline")
	}
	if !km.Matches(press(tea.KeyF1, "", 0), keymap.HelpToggle) || !km.Matches(press('?', "?", 0), keymap.HelpToggle) {
		t.Error("? and f1 must match help.toggle")
	}
	if km.Matches(press('x', "x", 0), keymap.Action("nope")) {
		t.Error("unknown action must never match")
	}
	// A nil Keymap behaves like Default (defensive: ctx.Keys unset).
	var nilKM *keymap.Keymap
	if !nilKM.Matches(ctrlK, keymap.PaletteOpen) {
		t.Error("nil Keymap must fall back to defaults")
	}
}

func TestLoadOverrides(t *testing.T) {
	km, errs := keymap.Load(map[string][]string{"palette.open": {"ctrl+p"}})
	if len(errs) != 0 {
		t.Fatalf("errs = %v", errs)
	}
	if !km.Matches(press('p', "", tea.ModCtrl), keymap.PaletteOpen) {
		t.Error("ctrl+p must match after override")
	}
	if km.Matches(press('k', "", tea.ModCtrl), keymap.PaletteOpen) {
		t.Error("ctrl+k must not match after override")
	}
	if h := km.Binding(keymap.PaletteOpen).Help(); h.Key != "ctrl+p" || h.Desc != "이동" {
		t.Errorf("override help = %+v", h)
	}
	// Other actions keep their defaults.
	if !km.Matches(press('k', "k", 0), keymap.ListUp) {
		t.Error("list.up default lost")
	}
	// Overriding never mutates a later Default().
	if !keymap.Default().Matches(press('k', "", tea.ModCtrl), keymap.PaletteOpen) {
		t.Error("Load mutated the defaults")
	}

	km, errs = keymap.Load(map[string][]string{"nope": {"x"}})
	if len(errs) != 1 {
		t.Fatalf("unknown action: errs = %v, want 1", errs)
	}
	if !reflect.DeepEqual(km.Binding(keymap.PaletteOpen).Keys(), []string{"ctrl+k"}) {
		t.Error("defaults not kept after unknown action")
	}

	km, errs = keymap.Load(map[string][]string{"help.toggle": {"ctrl+"}})
	if len(errs) != 1 {
		t.Fatalf("invalid key: errs = %v, want 1", errs)
	}
	if !reflect.DeepEqual(km.Binding(keymap.HelpToggle).Keys(), []string{"?", "f1"}) {
		t.Errorf("help.toggle default not kept: %q", km.Binding(keymap.HelpToggle).Keys())
	}

	// Valid key forms accepted; one bad key rejects that action's whole override.
	valid := []string{"x", "G", "?", "한", "ctrl+alt+shift+a", "alt+enter", "f12", "pgdown", "space", "delete", "insert", "backspace", "ctrl+left"}
	if _, errs := keymap.Load(map[string][]string{"feed.toggle": valid}); len(errs) != 0 {
		t.Errorf("valid keys rejected: %v", errs)
	}
	invalid := []string{"", "ctrl+", "ctrl", "f13", "f0", "pagedown", "shift+ctrl+a", "ctrl+ctrl+a", "ab", "ctrl+ab", " ", "meta+a"}
	for _, k := range invalid {
		km, errs := keymap.Load(map[string][]string{"feed.toggle": {"x", k}})
		if len(errs) != 1 {
			t.Errorf("key %q: errs = %v, want 1", k, errs)
			continue
		}
		if !reflect.DeepEqual(km.Binding(keymap.FeedToggle).Keys(), []string{"f"}) {
			t.Errorf("key %q: default not kept", k)
		}
	}
	// Empty key list is an error too (default kept).
	km, errs = keymap.Load(map[string][]string{"feed.toggle": {}})
	if len(errs) != 1 || !reflect.DeepEqual(km.Binding(keymap.FeedToggle).Keys(), []string{"f"}) {
		t.Errorf("empty list: errs = %v", errs)
	}
	// Multiple problems => one error each, deterministic.
	_, errs = keymap.Load(map[string][]string{"a.b": {"x"}, "c.d": {"y"}, "list.up": {"ctrl+"}})
	if len(errs) != 3 {
		t.Errorf("errs = %v, want 3", errs)
	}
	if km, errs := keymap.Load(nil); km == nil || len(errs) != 0 {
		t.Errorf("Load(nil) = %v, %v", km, errs)
	}
}

func TestHelpKorean(t *testing.T) {
	km := keymap.Default()
	hs := km.Help(keymap.PaletteOpen)
	if len(hs) != 1 || hs[0].Help().Desc != "이동" {
		t.Fatalf("Help(PaletteOpen) = %+v", hs)
	}
	want := map[keymap.Action]string{
		keymap.AppQuit: "종료", keymap.PaletteOpen: "이동", keymap.SearchGlobal: "검색", keymap.NotificationsOpen: "알림",
		keymap.WindowPrefix: "창", keymap.FocusNext: "다음 패널", keymap.FocusPrev: "이전 패널", keymap.HelpToggle: "도움말",
		keymap.FeedToggle: "피드", keymap.TabPrev: "이전 탭", keymap.TabNext: "다음 탭", keymap.NavBack: "뒤로",
		keymap.ListUp: "위", keymap.ListDown: "아래", keymap.ListTop: "맨 위", keymap.ListBottom: "맨 아래",
		keymap.ListPageUp: "한 쪽 위", keymap.ListPageDown: "한 쪽 아래", keymap.ListOpen: "열기",
		keymap.ComposeFocus: "입력", keymap.ComposeSend: "보내기", keymap.ComposeNewline: "줄바꿈",
		keymap.ComposeComplete: "자동완성", keymap.ComposeEditLast: "마지막 글 고치기", keymap.ComposeLeave: "나가기",
		keymap.MsgThread: "스레드", keymap.MsgReact: "리액션", keymap.MsgEdit: "수정", keymap.MsgDelete: "삭제",
		keymap.MsgPin: "고정", keymap.MsgCopy: "복사", keymap.MsgActions: "메뉴", keymap.ChannelSearch: "채널 검색",
		keymap.PostLike: "좋아요", keymap.PostRepost: "리포스트", keymap.PostQuote: "인용", keymap.PostReply: "답글",
		keymap.PostOpenMedia: "이미지", keymap.PostAuthor: "작성자", keymap.PostCompose: "글쓰기",
	}
	all := km.Help(allActions...)
	if len(all) != len(allActions) {
		t.Fatalf("Help(all) len = %d", len(all))
	}
	for i, a := range allActions {
		h := all[i].Help()
		if h.Desc == "" || h.Key == "" {
			t.Errorf("%s: empty help %+v", a, h)
		}
		if h.Desc != want[a] {
			t.Errorf("%s desc = %q, want %q", a, h.Desc, want[a])
		}
	}
	// Unknown actions are skipped.
	if hs := km.Help(keymap.Action("nope"), keymap.HelpToggle); len(hs) != 1 {
		t.Errorf("Help with unknown = %d bindings, want 1", len(hs))
	}
}

func TestBound(t *testing.T) {
	km := keymap.Default()
	if !km.Bound(press('j', "j", 0)) || !km.Bound(press('k', "", tea.ModCtrl)) || !km.Bound(press(tea.KeyEnter, "", 0)) {
		t.Error("j, ctrl+k and enter are bound by default")
	}
	if km.Bound(press('ㅓ', "ㅓ", 0)) || km.Bound(press('x', "x", 0)) {
		t.Error("ㅓ and x are unbound by default")
	}
	km, errs := keymap.Load(map[string][]string{"list.down": {"ㅓ"}})
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	if !km.Bound(press('ㅓ', "ㅓ", 0)) || km.Bound(press('j', "j", 0)) {
		t.Error("an override binds ㅓ and frees j")
	}
	var nilKM *keymap.Keymap
	if !nilKM.Bound(press('j', "j", 0)) || nilKM.Bound(press('ㅓ', "ㅓ", 0)) {
		t.Error("nil Keymap must fall back to defaults")
	}
}
