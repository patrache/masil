package uitest

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/patrache/masil/internal/ui/app"
	"github.com/patrache/masil/internal/ui/layout"
)

// TestAllSizes lives in sizes_test.go (Task 2); the -run filters of Task 20 still include it.

var t0 = time.Date(2026, 11, 5, 2, 5, 0, 0, time.UTC)

func TestFakeClock(t *testing.T) {
	c := NewFakeClock(t0)
	if !c.Now().Equal(t0) {
		t.Fatalf("Now() = %v, want %v", c.Now(), t0)
	}
	cmd := c.After(3*time.Second, "a")
	if got := c.Pending(); len(got) != 0 {
		t.Fatalf("After must record only when the Cmd runs; Pending() = %v", got)
	}
	if msg := cmd(); msg != nil {
		t.Fatalf("After Cmd must not block or deliver; got %v", msg)
	}
	if msg := c.After(time.Second, "b")(); msg != nil {
		t.Fatalf("got %v", msg)
	}
	if msg := c.After(time.Second, "c")(); msg != nil {
		t.Fatalf("got %v", msg)
	}
	want := []time.Duration{time.Second, time.Second, 3 * time.Second}
	if got := c.Pending(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Pending() = %v, want %v", got, want)
	}

	until := t0.Add(2 * time.Second)
	msg, ok := c.nextDue(until)
	if !ok || msg != "b" {
		t.Fatalf("first nextDue = (%v, %v), want (b, true)", msg, ok)
	}
	if !c.Now().Equal(t0.Add(time.Second)) {
		t.Fatalf("Now() after first due = %v, want t0+1s", c.Now())
	}
	msg, ok = c.nextDue(until)
	if !ok || msg != "c" {
		t.Fatalf("second nextDue = (%v, %v), want (c, true) (same deadline => registration order)", msg, ok)
	}
	msg, ok = c.nextDue(until)
	if ok || msg != nil {
		t.Fatalf("third nextDue = (%v, %v), want (nil, false)", msg, ok)
	}
	if !c.Now().Equal(t0.Add(time.Second)) {
		t.Fatalf("a miss must not move the clock; Now() = %v", c.Now())
	}
	// Remaining delays are relative to the moved clock.
	if got := c.Pending(); !reflect.DeepEqual(got, []time.Duration{2 * time.Second}) {
		t.Fatalf("Pending() after deliveries = %v, want [2s]", got)
	}
	// A Cmd run later records relative to the clock at RUN time.
	late := c.After(500*time.Millisecond, "d")
	if _, ok := c.nextDue(t0.Add(10 * time.Second)); !ok { // delivers "a" at t0+3s
		t.Fatal("expected a")
	}
	late()
	if got := c.Pending(); !reflect.DeepEqual(got, []time.Duration{500 * time.Millisecond}) {
		t.Fatalf("Pending() = %v, want [500ms]", got)
	}
	msg, ok = c.nextDue(t0.Add(10 * time.Second))
	if !ok || msg != "d" || !c.Now().Equal(t0.Add(3500*time.Millisecond)) {
		t.Fatalf("nextDue = (%v, %v) at %v, want d at t0+3.5s", msg, ok, c.Now())
	}
}

func TestParseKey(t *testing.T) {
	for _, s := range []string{
		"ctrl+k", "shift+tab", "shift+enter", "alt+enter", "enter", "esc", "tab", "space",
		"up", "down", "left", "right", "home", "end", "pgup", "pgdown", "backspace", "delete", "insert",
		"f1", "f12", "ctrl+j", "ctrl+u", "alt+x", "G", "?", "[", "+", "j", "한", "ㄱ", "👍🏽",
	} {
		k := parseKey(s)
		if got := k.String(); got != s {
			t.Errorf("parseKey(%q).String() = %q (%+v)", s, got, k)
		}
	}
	// Printable keys carry Text; named keys and modifier combos do not.
	for _, s := range []string{"G", "?", "[", "j", "한", "ㄱ", "👍🏽"} {
		if k := parseKey(s); k.Text != s || k.Mod != 0 {
			t.Errorf("parseKey(%q) = %+v, want Text %q and no Mod", s, k, s)
		}
	}
	for _, s := range []string{"enter", "esc", "tab", "up", "f1", "ctrl+k", "alt+enter"} {
		if k := parseKey(s); k.Text != "" {
			t.Errorf("parseKey(%q).Text = %q, want empty", s, k.Text)
		}
	}
	checks := []struct {
		s    string
		code rune
		mod  tea.KeyMod
		text string
	}{
		{"ctrl+k", 'k', tea.ModCtrl, ""},
		{"shift+tab", tea.KeyTab, tea.ModShift, ""},
		{"alt+enter", tea.KeyEnter, tea.ModAlt, ""},
		{"ctrl+alt+shift+up", tea.KeyUp, tea.ModCtrl | tea.ModAlt | tea.ModShift, ""},
		{"space", tea.KeySpace, 0, " "},
		{"esc", tea.KeyEscape, 0, ""},
		{"pgdown", tea.KeyPgDown, 0, ""},
		{"f1", tea.KeyF1, 0, ""},
		{"한", '한', 0, "한"},
		{"👍🏽", tea.KeyExtended, 0, "👍🏽"},
	}
	for _, c := range checks {
		k := parseKey(c.s)
		if k.Code != c.code || k.Mod != c.mod || k.Text != c.text {
			t.Errorf("parseKey(%q) = {Code:%U Mod:%v Text:%q}, want {Code:%U Mod:%v Text:%q}",
				c.s, k.Code, k.Mod, k.Text, c.code, c.mod, c.text)
		}
	}
	// The real keymap matcher sees the same string.
	if got := parseKey("ctrl+k").Keystroke(); got != "ctrl+k" {
		t.Errorf("Keystroke() = %q", got)
	}
}

// recTB records Error/Errorf/Fatal calls instead of failing the test.
type recTB struct {
	testing.TB
	errs   []string
	fatals int
}

func (r *recTB) Helper()           {}
func (r *recTB) Error(args ...any) { r.errs = append(r.errs, fmt.Sprint(args...)) }
func (r *recTB) Errorf(format string, args ...any) {
	r.errs = append(r.errs, fmt.Sprintf(format, args...))
}
func (r *recTB) Fatal(args ...any)                 { r.fatals++; r.Error(args...) }
func (r *recTB) Fatalf(format string, args ...any) { r.fatals++; r.Errorf(format, args...) }
func (r *recTB) Fail()                             { r.errs = append(r.errs, "Fail()") }
func (r *recTB) FailNow()                          { r.fatals++; r.Fail() }

// screen20x6 is a 20x6 frame: a double-bordered box {0,0,20,5} and a status row, styled with SGR.
func screen20x6() []string {
	return []string{
		"╔" + strings.Repeat("═", 18) + "╗",
		"║" + strings.Repeat(" ", 18) + "║",
		"║" + strings.Repeat(" ", 18) + "║",
		"║" + strings.Repeat(" ", 18) + "║",
		"╚" + strings.Repeat("═", 18) + "╝",
		"상태줄 ✓ 연결됨" + strings.Repeat(" ", 5),
	}
}

func boxes20x6() []Box {
	return []Box{
		{Rect: layout.Rect{X: 0, Y: 0, W: 20, H: 5}, Bordered: true, Z: 0},
		{Rect: layout.Rect{X: 0, Y: 5, W: 20, H: 1}, Bordered: false, Z: 0},
	}
}

func styled(lines []string) string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = "\x1b[38;2;255;176;0m" + l + "\x1b[0m"
	}
	return strings.Join(out, "\n")
}

// replaceCell replaces the n-th rune of line i.
func replaceCell(lines []string, i, n int, s string) {
	r := []rune(lines[i])
	lines[i] = string(r[:n]) + s + string(r[n+1:])
}

func TestIntegrityPasses(t *testing.T) {
	rec := &recTB{TB: t}
	AssertIntegrity(rec, styled(screen20x6()), 20, 6, boxes20x6(), lipgloss.DoubleBorder())
	if len(rec.errs) != 0 {
		t.Fatalf("clean frame reported: %q", rec.errs)
	}
}

func TestIntegrityDetects(t *testing.T) {
	type mut struct {
		name   string
		edit   func(lines []string) []string
		boxes  func(b []Box) []Box
		want   string // substring of at least one reported error; "" => no error expected
		border lipgloss.Border
	}
	cases := []mut{
		{name: "short line", want: "width", edit: func(l []string) []string {
			l[5] = strings.TrimSuffix(l[5], " ")
			return l
		}},
		{name: "wc width mismatch", want: "StringWidthWc", edit: func(l []string) []string {
			l[2] = "║❄️" + strings.Repeat(" ", 16) + "║" // grapheme width 2, wcwidth 1
			return l
		}},
		{name: "missing corner", want: "corner", edit: func(l []string) []string {
			replaceCell(l, 0, 19, "═")
			return l
		}},
		{name: "left edge replaced", want: "edge", edit: func(l []string) []string {
			replaceCell(l, 2, 0, "|")
			return l
		}},
		{name: "bottom edge replaced", want: "edge", edit: func(l []string) []string {
			replaceCell(l, 4, 7, "-")
			return l
		}},
		{name: "top edge broken after title", want: "edge", edit: func(l []string) []string {
			l[0] = "╔═ 제목 ════x══════╗"
			return l
		}},
		{name: "replacement char", want: "U+FFFD", edit: func(l []string) []string {
			replaceCell(l, 5, 12, "�")
			return l
		}},
		{name: "control char", want: "control", edit: func(l []string) []string {
			replaceCell(l, 3, 5, "\x07 ")
			return l
		}},
		{name: "C1 control", want: "control", edit: func(l []string) []string {
			replaceCell(l, 3, 5, "\u0085 ")
			return l
		}},
		{name: "invalid utf8", want: "UTF-8", edit: func(l []string) []string {
			replaceCell(l, 3, 5, "\xed\xa0\x80") // CESU-8 encoded lone surrogate
			return l
		}},
		{name: "seven lines", want: "lines", edit: func(l []string) []string {
			return append(l, strings.Repeat(" ", 20))
		}},
		{name: "overlay covers status row", want: "status", boxes: func(b []Box) []Box {
			return append(b, Box{Rect: layout.Rect{X: 2, Y: 3, W: 10, H: 3}, Bordered: false, Z: 10})
		}},
		{name: "toast covers status row", want: "status", boxes: func(b []Box) []Box {
			return append(b, Box{Rect: layout.Rect{X: 2, Y: 5, W: 10, H: 1}, Bordered: false, Z: 100})
		}},
		{name: "box out of bounds", want: "bounds", boxes: func(b []Box) []Box {
			return append(b, Box{Rect: layout.Rect{X: 15, Y: 1, W: 10, H: 2}, Bordered: false, Z: 10})
		}},
		{name: "negative box", want: "bounds", boxes: func(b []Box) []Box {
			return append(b, Box{Rect: layout.Rect{X: -1, Y: 1, W: 3, H: 2}, Bordered: false, Z: 10})
		}},
		{name: "bordered box on status row", want: "status", edit: func(l []string) []string {
			l[4] = "║" + strings.Repeat(" ", 18) + "║"
			l[5] = "╚" + strings.Repeat("═", 18) + "╝"
			return l
		}, boxes: func(b []Box) []Box {
			return []Box{{Rect: layout.Rect{X: 0, Y: 0, W: 20, H: 6}, Bordered: true, Z: 0}}
		}},
		{name: "wrong border set", want: "corner", border: lipgloss.RoundedBorder()},
		// Not violations:
		{name: "covered pane border", want: "", edit: func(l []string) []string {
			for i := 1; i <= 2; i++ {
				for x := 0; x < 5; x++ {
					replaceCell(l, i, x, "x")
				}
			}
			return l
		}, boxes: func(b []Box) []Box {
			return append(b, Box{Rect: layout.Rect{X: 0, Y: 1, W: 5, H: 2}, Bordered: false, Z: 10})
		}},
		{name: "covered corner", want: "", edit: func(l []string) []string {
			replaceCell(l, 0, 19, "▒")
			return l
		}, boxes: func(b []Box) []Box {
			return append(b, Box{Rect: layout.Rect{X: 19, Y: 0, W: 1, H: 1}, Bordered: false, Z: 100})
		}},
		{name: "title segment", want: "", edit: func(l []string) []string {
			l[0] = "╔═ 제목 ═══════════╗"
			return l
		}},
		{name: "title and badge right after corner", want: "", edit: func(l []string) []string {
			l[0] = "╔#개발 ● 3 ════════╗"
			return l
		}},
		{name: "bordered overlay", want: "", edit: func(l []string) []string {
			l[1] = "║  ╔═ 확인 ══╗     ║"
			l[2] = "║  ║ 지울까요║     ║"
			l[3] = "║  ╚═════════╝     ║"
			return l
		}, boxes: func(b []Box) []Box {
			return append(b, Box{Rect: layout.Rect{X: 3, Y: 1, W: 11, H: 3}, Bordered: true, Z: 10})
		}},
		{name: "bordered overlay edge broken", want: "edge", edit: func(l []string) []string {
			l[1] = "║  ╔═ 확인 ══╗     ║"
			l[2] = "║  ║ 지울까요?     ║"
			l[3] = "║  ╚═════════╝     ║"
			return l
		}, boxes: func(b []Box) []Box {
			return append(b, Box{Rect: layout.Rect{X: 3, Y: 1, W: 11, H: 3}, Bordered: true, Z: 10})
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			lines := screen20x6()
			if c.edit != nil {
				lines = c.edit(lines)
			}
			bx := boxes20x6()
			if c.boxes != nil {
				bx = c.boxes(bx)
			}
			border := lipgloss.DoubleBorder()
			if c.border.Top != "" {
				border = c.border
			}
			rec := &recTB{TB: t}
			AssertIntegrity(rec, styled(lines), 20, 6, bx, border)
			if rec.fatals != 0 {
				t.Errorf("AssertIntegrity must use Errorf, not Fatal: %q", rec.errs)
			}
			if c.want == "" {
				if len(rec.errs) != 0 {
					t.Fatalf("unexpected reports: %q", rec.errs)
				}
				return
			}
			if len(rec.errs) == 0 {
				t.Fatalf("mutation %q not reported", c.name)
			}
			for _, e := range rec.errs {
				if strings.Contains(e, c.want) {
					return
				}
			}
			t.Fatalf("no report mentions %q: %q", c.want, rec.errs)
		})
	}
}

func TestIntegrityCapsReports(t *testing.T) {
	lines := make([]string, 30)
	for i := range lines {
		lines[i] = strings.Repeat(" ", 40)
	}
	bx := []Box{
		{Rect: layout.Rect{X: 0, Y: 0, W: 40, H: 29}, Bordered: true, Z: 0},
		{Rect: layout.Rect{X: 0, Y: 29, W: 40, H: 1}, Z: 0},
	}
	rec := &recTB{TB: t}
	AssertIntegrity(rec, strings.Join(lines, "\n"), 40, 30, bx, lipgloss.DoubleBorder())
	if len(rec.errs) != maxIntegrityReports+1 {
		t.Fatalf("got %d reports, want %d (cap + summary)", len(rec.errs), maxIntegrityReports+1)
	}
	if last := rec.errs[len(rec.errs)-1]; !strings.Contains(last, "more") {
		t.Fatalf("last report %q is not a summary", last)
	}
}

func TestIntegrityTooSmallHasNoStatusRule(t *testing.T) {
	// TooSmall (19x10): a single unbordered main pane; no status-line requirement.
	lines := make([]string, 10)
	for i := range lines {
		lines[i] = strings.Repeat(" ", 19)
	}
	bx := []Box{{Rect: layout.Rect{X: 0, Y: 0, W: 19, H: 10}, Z: 0}}
	rec := &recTB{TB: t}
	AssertIntegrity(rec, strings.Join(lines, "\n"), 19, 10, bx, lipgloss.DoubleBorder())
	if len(rec.errs) != 0 {
		t.Fatalf("unexpected reports: %q", rec.errs)
	}
}

func TestBoxesOf(t *testing.T) {
	r := layout.Rect{X: 10, Y: 5, W: 30, H: 8}
	r2 := layout.Rect{X: 50, Y: 1, W: 20, H: 3}
	f := app.FrameInfo{
		Layout: layout.Layout{Mode: layout.ModeWidget, Cols: 80, Rows: 24, Panes: []layout.Pane{
			{Slot: layout.SlotMain, Rect: layout.Rect{X: 0, Y: 0, W: 80, H: 23}, Bordered: true},
			{Slot: layout.SlotStatus, Rect: layout.Rect{X: 0, Y: 23, W: 80, H: 1}},
		}},
		Overlays: []app.Box{{Rect: r, Bordered: true}},
		Toast:    &app.Box{Rect: r2, Bordered: true},
	}
	want := []Box{
		{Rect: layout.Rect{X: 0, Y: 0, W: 80, H: 23}, Bordered: true, Z: 0},
		{Rect: layout.Rect{X: 0, Y: 23, W: 80, H: 1}, Bordered: false, Z: 0},
		{Rect: r, Bordered: true, Z: 10},
		{Rect: r2, Bordered: true, Z: 100},
	}
	if got := BoxesOf(f); !reflect.DeepEqual(got, want) {
		t.Fatalf("BoxesOf = %+v, want %+v", got, want)
	}

	two := app.FrameInfo{Overlays: []app.Box{{Rect: r}, {Rect: r2, Bordered: true}}}
	got := BoxesOf(two)
	if len(got) != 2 || got[0].Z != 10 || got[1].Z != 11 || got[0].Bordered || !got[1].Bordered {
		t.Fatalf("overlays => Z 10+i: %+v", got)
	}
	if got := BoxesOf(app.FrameInfo{}); len(got) != 0 {
		t.Fatalf("empty frame => no boxes, got %+v", got)
	}
}
