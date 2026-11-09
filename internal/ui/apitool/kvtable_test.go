package apitool

import (
	"reflect"
	"strings"
	"testing"

	"github.com/patrache/masil/internal/apitool"
	"github.com/patrache/masil/internal/textwidth"
)

func press(t *testing.T, tb *KVTable, keys ...string) (changed bool) {
	t.Helper()
	ctx, _ := testCtx()
	for _, k := range keys {
		if len([]rune(k)) > 1 && testKeyCodes[k] == 0 && k != "shift+tab" {
			for _, r := range k { // typed text
				_, ch, _ := tb.HandleKey(ctx, key(string(r)))
				changed = changed || ch
			}
			continue
		}
		_, ch, _ := tb.HandleKey(ctx, key(k))
		changed = changed || ch
	}
	return changed
}

func TestKVTableEditCycle(t *testing.T) {
	tb := NewKVTable(KVCols{Key: "Name", Value: "Value"})
	if press(t, tb, "a"); !tb.CapturesInput() || tb.Cursor() == nil {
		t.Fatal("a does not start editing the key")
	}
	press(t, tb, "X-Trace", "tab", "abc", "backspace", "d", "enter")
	if tb.CapturesInput() {
		t.Fatal("enter did not confirm")
	}
	if got := tb.Rows(); !reflect.DeepEqual(got, []apitool.KV{{Key: "X-Trace", Value: "abd"}}) {
		t.Fatalf("rows = %+v", got)
	}
	if !press(t, tb, "space") || !tb.Rows()[0].Off {
		t.Fatal("space did not turn the row off")
	}
	press(t, tb, "d")
	if len(tb.Rows()) != 0 {
		t.Fatalf("d left %+v", tb.Rows())
	}
	press(t, tb, "u")
	if got := tb.Rows(); len(got) != 1 || got[0].Key != "X-Trace" || !got[0].Off {
		t.Fatalf("u restored %+v", got)
	}

	// enter edits the focused cell; esc cancels the edit.
	press(t, tb, "space", "tab", "enter", "zzz", "esc")
	if got := tb.Rows()[0]; got.Value != "abd" || tb.CapturesInput() {
		t.Fatalf("esc kept %+v", got)
	}
	press(t, tb, "enter", "!", "enter")
	if got := tb.Rows()[0].Value; got != "abd!" {
		t.Fatalf("value = %q", got)
	}
	// a adds below the cursor; j/k move.
	press(t, tb, "a", "B", "enter", "k", "a", "A", "enter")
	keys := []string{}
	for _, r := range tb.Rows() {
		keys = append(keys, r.Key)
	}
	if strings.Join(keys, ",") != "X-Trace,A,B" {
		t.Fatalf("order = %v", keys)
	}
	// Row ids survive SetRows/Rows (new rows get theirs when saved).
	tb.SetRows([]apitool.KV{{ID: "k1", Key: "q", Value: "1"}})
	if got := tb.Rows(); !reflect.DeepEqual(got, []apitool.KV{{ID: "k1", Key: "q", Value: "1"}}) {
		t.Fatalf("SetRows/Rows = %+v", got)
	}
}

func TestKVTableWideView(t *testing.T) {
	ctx, _ := testCtx()
	tb := NewKVTable(KVCols{Key: "Name", Value: "Value"})
	if got := plain(tb.View(ctx, 80, 4, true)); !strings.Contains(got[1], ctx.T.Apitest.KvEmpty()) {
		t.Fatalf("empty view = %q", got)
	}
	tb.SetRows([]apitool.KV{{Key: "Accept", Value: "application/json"}, {Key: "X-Off", Value: "1", Off: true}, {Key: "Bad\x1b[31m", Value: "v‮"}})
	lines := tb.View(ctx, 80, 6, true)
	if len(lines) != 6 {
		t.Fatalf("%d lines", len(lines))
	}
	for i, l := range lines {
		if w := textwidth.Width(l); w != 80 {
			t.Fatalf("line %d width %d: %q", i, w, l)
		}
	}
	p := plain(lines)
	if !strings.Contains(p[0], "Name") || !strings.Contains(p[0], "Value") || !strings.Contains(p[1], "Accept") || !strings.Contains(p[1], "application/json") {
		t.Fatalf("view = %q", p)
	}
	if strings.Contains(strings.Join(p, "\n"), "\x1b") || strings.Contains(strings.Join(p, "\n"), "‮") {
		t.Fatalf("unsanitized: %q", p)
	}
}

func TestKVTableNarrowList(t *testing.T) {
	ctx, _ := testCtx()
	tb := NewKVTable(KVCols{Key: "Name", Value: "Value"})
	tb.SetRows([]apitool.KV{{Key: "Accept", Value: "application/json"}, {Key: "X-Long", Value: strings.Repeat("v", 200)}})
	lines := tb.View(ctx, 50, 5, true)
	p := plain(lines)
	if !strings.Contains(p[0], "Accept = application/json") || !strings.Contains(p[1], "X-Long = vvv") {
		t.Fatalf("narrow = %q", p)
	}
	for i, l := range lines {
		if w := textwidth.Width(l); w != 50 {
			t.Fatalf("line %d width %d", i, w)
		}
	}
}

func TestKVTableSecretMasked(t *testing.T) {
	ctx, rt := testCtx()
	tb := NewKVTable(KVCols{Key: "Name", Value: "Value", Secretable: true})
	tb.SetSecretNames(func(name string) bool { return name == "token" })
	tb.SetRows([]apitool.KV{{Key: "token", Value: "sk-live-abcdef1234"}, {Key: "user", Value: "me"}, {Key: "token2", Value: "short"}})
	view := func() string { return strings.Join(plain(tb.View(ctx, 80, 5, true)), "\n") }
	v := view()
	if strings.Contains(v, "sk-live") || !strings.Contains(v, "••••1234") || !strings.Contains(v, "me") {
		t.Fatalf("masked view = %s", v)
	}
	_, _, cmd := tb.HandleKey(ctx, key("V"))
	if !strings.Contains(view(), "sk-live-abcdef1234") {
		t.Fatalf("V did not reveal: %s", view())
	}
	if cmd == nil || len(rt.delays) != 1 || rt.delays[0].Seconds() != 5 {
		t.Fatalf("reveal timer = %v %v", cmd, rt.delays)
	}
	msg := cmd()
	if !tb.HandleMsg(msg) || strings.Contains(view(), "sk-live") {
		t.Fatalf("timer did not hide: %s", view())
	}
	// Another key hides at once; a stale timer does nothing.
	_, _, cmd = tb.HandleKey(ctx, key("V"))
	tb.HandleKey(ctx, key("j"))
	if strings.Contains(view(), "sk-live") {
		t.Fatal("other key did not hide")
	}
	_, _, cmd2 := tb.HandleKey(ctx, key("k"))
	_ = cmd2
	tb.HandleKey(ctx, key("V"))
	if tb.HandleMsg(cmd()) || !strings.Contains(view(), "sk-live") {
		t.Fatal("a stale timer hid the new reveal")
	}
	// A short secret shows no characters.
	tb.SetSecretNames(func(name string) bool { return strings.HasPrefix(name, "token") })
	if strings.Contains(view(), "short") {
		t.Fatalf("short secret shown: %s", view())
	}
	// Editing a secret value shows what is typed.
	tb.SetRows([]apitool.KV{{Key: "token", Value: ""}})
	press(t, tb, "tab", "enter", "typed-secret")
	if !strings.Contains(view(), "typed-secret") {
		t.Fatalf("editing hides input: %s", view())
	}
}

// Editing the key of a secret row keeps its value masked.
func TestKVTableSecretMaskedWhileEditingKey(t *testing.T) {
	ctx, _ := testCtx()
	tb := NewKVTable(KVCols{Key: "Name", Value: "Value", Secretable: true})
	tb.SetSecretNames(func(name string) bool { return name == "token" })
	tb.SetRows([]apitool.KV{{Key: "token", Value: "sk-live-abcdef1234"}})
	press(t, tb, "enter")
	if !tb.CapturesInput() {
		t.Fatal("enter did not edit the key")
	}
	if v := strings.Join(plain(tb.View(ctx, 80, 3, true)), "\n"); strings.Contains(v, "sk-live") {
		t.Fatalf("editing the key shows the secret: %s", v)
	}
}

// A cell being edited never puts control sequences in the view.
func TestKVTableEditSanitized(t *testing.T) {
	ctx, _ := testCtx()
	tb := NewKVTable(KVCols{Key: "Name", Value: "Value"})
	tb.SetRows([]apitool.KV{{Key: "X", Value: "a\x1b]0;pwned\x07b\x1b]52;c;ZXZpbA==\x07"}})
	press(t, tb, "tab", "enter")
	if !tb.CapturesInput() {
		t.Fatal("not editing")
	}
	for _, w := range []int{80, 40} {
		v := strings.Join(tb.View(ctx, w, 3, true), "\n")
		if strings.Contains(v, "\x1b]") || strings.Contains(v, "\x07") || strings.Contains(v, "pwned") && strings.Contains(v, "\x1b]0") {
			t.Fatalf("width %d: control sequence in view: %q", w, v)
		}
	}
	if c := tb.Cursor(); c == nil {
		t.Fatal("no cursor")
	}
}
