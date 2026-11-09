package chat

import (
	"errors"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/patrache/masil/internal/clipboard"
	"github.com/patrache/masil/internal/realtime"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

// posOf is the screen cell of the first occurrence of text inside slot's
// inner rect.
func posOf(t testing.TB, h *uitest.Harness, slot layout.SlotID, text string) (x, y int) {
	t.Helper()
	screen := h.Screen()
	p, ok := h.Model.Frame().Layout.Pane(slot)
	if !ok {
		t.Fatalf("no %s pane", slot)
	}
	in := p.Rect
	if p.Bordered {
		in = in.Inner()
	}
	for i, row := range cropRect(screen, in) {
		if j := strings.Index(row, text); j >= 0 {
			return in.X + ansi.StringWidth(row[:j]), in.Y + i
		}
	}
	t.Fatalf("%q not in %s:\n%s", text, slot, screen)
	return 0, 0
}

// highlighted is the plain text of the screen lines painted in the drag
// selection style (S.TextSelect: the theme's foreground as background).
func highlighted(h *uitest.Harness) []string {
	sgr := bgSGR(h.Context().Theme.P.FG)
	var out []string
	styled := strings.Split(h.Styled(), "\n")
	for _, l := range styled {
		if strings.Contains(l, sgr) {
			out = append(out, strings.TrimSpace(ansi.Strip(l)))
		}
	}
	return out
}

// lastClipboard is the last system-clipboard write ("" = none).
func lastClipboard(h *uitest.Harness) string {
	c := h.Clipboards()
	if len(c) == 0 {
		return ""
	}
	return c[len(c)-1]
}

// TestDragCopiesLines: press on a message line, drag with the button held,
// release: the covered lines are highlighted while dragging, and on release
// their text as shown (no ANSI, outer indent dropped, one line per screen
// line) goes to both clipboards with "복사했어요".
func TestDragCopiesLines(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 120, H: 40})
	x0, y0 := posOf(t, h, layout.SlotMain, "민지 11:02")
	x1, y1 := posOf(t, h, layout.SlotMain, "이거 한 번만")

	h.Send(tea.MouseClickMsg{X: x0, Y: y0, Button: tea.MouseLeft})
	for y := y0 + 1; y <= y1; y++ {
		h.Send(tea.MouseMotionMsg{X: x0, Y: y, Button: tea.MouseLeft})
	}
	h.Send(tea.MouseMotionMsg{X: x1 + 3, Y: y1, Button: tea.MouseLeft})
	hl := highlighted(h)
	if len(hl) != y1-y0+1 || !strings.Contains(hl[0], "민지 11:02") || !strings.Contains(hl[len(hl)-1], "이거 한 번만") {
		t.Fatalf("while dragging, highlighted %q", hl)
	}
	if len(h.Clipboards()) != 0 {
		t.Fatal("nothing is copied before the release")
	}
	h.Send(tea.MouseReleaseMsg{X: x1 + 3, Y: y1, Button: tea.MouseLeft})

	want := "민지 11:02\n오늘 배포 몇 시에 해요?\n👍 2  👀 1 · 💬 답글 3개\n준호 11:04\n이거 한 번만 돌리면 돼요"
	if got := h.Clipboards(); !slices.Equal(got, []string{want}) {
		t.Fatalf("system clipboard %q\nwant %q", got, want)
	}
	if osc := clipboards(h); len(osc) != 1 || osc[0] != want {
		t.Fatalf("OSC 52 %q", osc)
	}
	if tt := toastText(t, h); !strings.Contains(tt, msgCopied) {
		t.Fatalf("toast %q", tt)
	}
	if _, ok := threadRoute(h); ok {
		t.Fatal("a drag opens no thread")
	}
	// the highlight stays until esc (which the pane eats: no back navigation)
	if len(highlighted(h)) == 0 {
		t.Fatal("the highlight stays after the release")
	}
	before := h.Nav()
	h.Press("esc")
	if hl := highlighted(h); len(hl) != 0 {
		t.Fatalf("esc clears the highlight, still %q", hl)
	}
	if !reflect.DeepEqual(before, h.Nav()) {
		t.Fatal("esc with a highlight only clears it")
	}
}

// TestDragUpward: dragging from a lower line up copies the same range in
// screen order.
func TestDragUpward(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 120, H: 40})
	x0, y0 := posOf(t, h, layout.SlotMain, "@minji 점심")
	_, y1 := posOf(t, h, layout.SlotMain, "sam 11:05")
	h.Drag(x0+4, y0, x0, y1)
	if got := lastClipboard(h); got != "sam 11:05\n@minji 점심 먹고 ㄱㄱ" {
		t.Fatalf("clipboard %q", got)
	}
}

// TestDragStartingOnSelectedRowOpensNothing: open-on-click of the selected
// row waits for the release, so a drag that starts there copies instead.
func TestDragStartingOnSelectedRowOpensNothing(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 120, H: 40})
	clickText(t, h, layout.SlotMain, "오늘 배포")
	root := fixtures.Messages(devID(t, h))[0].ID
	if selectedID(t, h) != root {
		t.Fatalf("click selects, got %q", selectedID(t, h))
	}
	x, y := posOf(t, h, layout.SlotMain, "오늘 배포")
	h.Drag(x, y, x, y+1)
	if _, ok := threadRoute(h); ok {
		t.Fatal("a drag from the selected row opens no thread")
	}
	if got := lastClipboard(h); got != "오늘 배포 몇 시에 해요?\n👍 2  👀 1 · 💬 답글 3개" {
		t.Fatalf("clipboard %q", got)
	}
	// a plain click on it (no motion) still opens the thread
	clickText(t, h, layout.SlotMain, "오늘 배포")
	if r, ok := threadRoute(h); !ok || r.ID != root {
		t.Fatalf("click on the selected row opens its thread: %+v", h.Model.Frame().Routes)
	}
}

// TestClickCopiesNothing: a click (press + release, no motion) selects as
// before and writes no clipboard.
func TestClickCopiesNothing(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 120, H: 40})
	clickText(t, h, layout.SlotMain, "점심 먹고")
	if selectedID(t, h) != fixtures.Messages(devID(t, h))[2].ID {
		t.Fatalf("click selects, got %q", selectedID(t, h))
	}
	if len(h.Clipboards()) != 0 || len(clipboards(h)) != 0 || len(highlighted(h)) != 0 {
		t.Fatalf("a click copies nothing: %q %q", h.Clipboards(), highlighted(h))
	}
}

// TestDragClickElsewhereClears: a press anywhere else (here the sidebar)
// clears the highlight.
func TestDragClickElsewhereClears(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 120, H: 40})
	x, y := posOf(t, h, layout.SlotMain, "민지 11:02")
	h.Drag(x, y, x, y+1)
	if len(highlighted(h)) != 2 {
		t.Fatalf("highlighted %q", highlighted(h))
	}
	clickText(t, h, layout.SlotSidebar, "# 잡담")
	if hl := highlighted(h); len(hl) != 0 {
		t.Fatalf("a click elsewhere clears the highlight, still %q", hl)
	}
}

// TestDragPastTheListClamps: dragging down past the list (over the composer
// and off the pane) stops at the last list line; the composer is untouched.
func TestDragPastTheListClamps(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 120, H: 40})
	x, y := posOf(t, h, layout.SlotMain, "6명 참여")
	_, cy := posOf(t, h, layout.SlotMain, "› # 개발에")
	h.Drag(x, y, x, cy+3) // into the composer box and the border below it
	if got := lastClipboard(h); got != "6명 참여" {
		t.Fatalf("clipboard %q", got)
	}
	if channelOf(t, h).composer.c.Value() != "" {
		t.Fatal("the composer is untouched")
	}
}

// TestDragBlockedMessageCopiesOnlyTheFold: a blocked author's message copies
// as the fold line it shows, never its hidden body.
func TestDragBlockedMessageCopiesOnlyTheFold(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 120, H: 40})
	minji := userBy(t, h.Context().Store, "minji")
	h.Context().Store.Blocks[minji.ID] = h.Context().Now()
	h.Event("block.updated", realtime.BlockUpdated{UserID: minji.ID, Blocked: true})
	x, y := posOf(t, h, layout.SlotMain, blockedText)
	_, y1 := posOf(t, h, layout.SlotMain, "이거 한 번만")
	h.Drag(x, y, x, y1)
	got := lastClipboard(h)
	if strings.Contains(got, "오늘 배포") || !strings.HasPrefix(got, blockedText+"\n준호 11:04") {
		t.Fatalf("clipboard %q", got)
	}
}

// TestDragSanitized: a hostile body copies exactly as the screen shows it —
// no escape, control or hidden rune, nothing the screen does not draw.
func TestDragSanitized(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 120, H: 40})
	m := mkMsg(devID(t, h), 900, userBy(t, h.Context().Store, "minji"), pasteJack, h.Context().Now())
	h.Event("message.created", realtime.MessageEvent{Message: m})
	x, y := posOf(t, h, layout.SlotMain, "echo hi")
	line := strings.Split(h.Screen(), "\n")[y]
	shown := strings.TrimRight(line[strings.Index(line, "echo hi"):], " ║")
	h.Drag(x, y-1, x+2, y)
	got := lastClipboard(h)
	for _, r := range got {
		if r == 0x1b || (r < 0x20 && r != '\n') || (r >= 0x7f && r <= 0x9f) || uitest.Hidden(r) {
			t.Fatalf("clipboard %q holds %U", got, r)
		}
	}
	if want := "민지 11:05\n" + shown; got != want {
		t.Fatalf("clipboard %q, screen %q", got, want)
	}
}

// TestDragCopyFailureHints: when the system clipboard cannot be written the
// toast says how to copy with the terminal instead of a false success.
func TestDragCopyFailureHints(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 120, H: 40})
	h.Clip.Fail(clipboard.ErrUnavailable)
	x, y := posOf(t, h, layout.SlotMain, "민지 11:02")
	h.Drag(x, y, x, y+1)
	if tt := toastText(t, h); strings.Contains(tt, msgCopied) || !strings.Contains(tt, "Shift") {
		t.Fatalf("toast %q", tt)
	}
	// y gets the same honesty
	selectText(t, h, "점심 먹고")
	h.Press("y")
	if tt := toastText(t, h); strings.Contains(tt, msgCopied) {
		t.Fatalf("toast %q", tt)
	}
	// a clipboard program that failed says so outright (the newest toast is on top)
	h.Clip.Fail(errors.New("exit status 1"))
	h.Press("y")
	if tt := toastText(t, h); !strings.HasPrefix(tt, ui.CopyHint) {
		t.Fatalf("toast %q, want %q", tt, ui.CopyHint)
	}
}

// TestDragThread: the thread pane copies root and replies the same way, and
// a drag over its composer row starts nothing.
func TestDragThread(t *testing.T) {
	root := devRootMsg(t)
	h := open(t, uitest.Size{W: 120, H: 40}, ui.Route{Kind: ui.RouteThread, ID: root.ID, Arg: root.ChannelID}, uitest.WithSeed(bootstrappedSeed))
	slot := threadSlot(t, h)
	x, y := posOf(t, h, slot, "오늘 배포")
	h.Drag(x, y-1, x, y)
	if got := lastClipboard(h); !strings.HasSuffix(got, "\n오늘 배포 몇 시에 해요?") || !strings.Contains(got, "민지") {
		t.Fatalf("clipboard %q", got)
	}
	n := len(h.Clipboards())
	cx, cy := posOf(t, h, slot, "›")
	h.Drag(cx, cy, cx+5, cy)
	if len(h.Clipboards()) != n {
		t.Fatal("a drag on the composer copies nothing")
	}
}

// TestDragDM: a DM open on the DM surface (CA 5.3.3) drag-copies like a channel,
// and esc only clears the highlight (the DM stays open above the DM list).
func TestDragDM(t *testing.T) {
	for _, sz := range []uitest.Size{{W: 120, H: 40}, {W: 80, H: 24}} {
		h := open(t, sz, ui.Route{Kind: ui.RouteHome}, uitest.WithSeed(bootstrappedSeed))
		h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteChannel, ID: fixtures.DMMinji}})
		if n := h.Nav(); !n.InDM || mainKey(h) != (ui.Route{Kind: ui.RouteChannel, ID: fixtures.DMMinji}).Key() {
			t.Fatalf("%v: precondition: the DM is shown on the DM surface: %+v", sz, n)
		}
		x, y := posOf(t, h, layout.SlotMain, "점심 국밥")
		h.Drag(x, y, x+3, y)
		if got := lastClipboard(h); !strings.Contains(got, "점심 국밥 어때요?") {
			t.Fatalf("%v: clipboard %q\n%s", sz, got, h.Screen())
		}
		if len(highlighted(h)) == 0 {
			t.Fatalf("%v: the highlight stays after the release", sz)
		}
		before := h.Nav()
		h.Press("esc")
		if hl := highlighted(h); len(hl) != 0 {
			t.Fatalf("%v: esc clears the highlight, still %q", sz, hl)
		}
		if !reflect.DeepEqual(before, h.Nav()) {
			t.Fatalf("%v: esc with a highlight only clears it", sz)
		}
		h.Check()
	}
}

// TestDragEveryMode: drag-copy works in widget (bubbles) and tiny (one line
// per message, no pane border) too.
func TestDragEveryMode(t *testing.T) {
	cases := []struct {
		size     uitest.Size
		from, to string
		want     string
	}{
		{uitest.Size{W: 80, H: 24}, "11:05 나", "@minji 점심", "11:05 나\n@minji 점심 먹고 ㄱㄱ"},
		{uitest.Size{W: 80, H: 24}, "준호 11:04", "이거 한 번만", "준호 11:04\n이거 한 번만 돌리면 돼요"},
		{uitest.Size{W: 50, H: 20}, "민지 오늘", "준호 이거", "민지 오늘 배포 몇 시에 해요?\n준호 이거 한 번만 돌리면 돼요 ‹코드 1줄›"},
	}
	for _, c := range cases {
		h := devChannel(t, c.size)
		x, y := posOf(t, h, layout.SlotMain, c.from)
		_, y1 := posOf(t, h, layout.SlotMain, c.to)
		h.Drag(x, y, x, y1)
		if got := lastClipboard(h); got != c.want {
			t.Fatalf("%v: clipboard %q, want %q\n%s", c.size, got, c.want, h.Screen())
		}
		h.Check()
	}
}

// TestDragHighlightIntegrity: a highlighted frame keeps every cell styled and
// every line its width (uitest integrity) in every mode.
func TestDragHighlightIntegrity(t *testing.T) {
	for _, sz := range []uitest.Size{{W: 120, H: 40}, {W: 120, H: 30}, {W: 80, H: 24}, {W: 50, H: 20}} {
		h := devChannel(t, sz)
		x, y := posOf(t, h, layout.SlotMain, "준호")
		h.Send(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
		h.Send(tea.MouseMotionMsg{X: x, Y: y + 2, Button: tea.MouseLeft})
		if len(highlighted(h)) == 0 {
			t.Fatalf("%v: no highlight", sz)
		}
		h.Check()
	}
}

// threadSlot is the slot the thread is placed in.
func threadSlot(t testing.TB, h *uitest.Harness) layout.SlotID {
	t.Helper()
	h.Screen()
	for slot, r := range h.Model.Frame().Routes {
		if r.Kind == ui.RouteThread {
			return slot
		}
	}
	t.Fatalf("no thread placed: %+v", h.Model.Frame().Routes)
	return ""
}

// TestDragPastTheTopScrolls: holding the drag above the list scrolls it a
// line per motion and extends the selection to lines that were off screen.
func TestDragPastTheTopScrolls(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 120, H: 40})
	dev, minji := devID(t, h), userBy(t, h.Context().Store, "minji")
	for i := range 40 {
		h.Event("message.created", realtime.MessageEvent{Message: mkMsg(dev, 900+i, minji, "줄"+strconv.Itoa(i), h.Context().Now())})
	}
	scr := h.Screen()
	if strings.Contains(scr, "줄5\n") || !strings.Contains(scr, "줄39") {
		t.Fatalf("precondition: the list shows the latest lines only\n%s", scr)
	}
	p, _ := h.Model.Frame().Layout.Pane(layout.SlotMain)
	in := p.Rect.Inner()
	top := in.Y
	h.Send(tea.MouseClickMsg{X: in.X + 3, Y: top + 1, Button: tea.MouseLeft})
	for y := top; y >= top-4; y-- { // the border row, then off the pane
		h.Send(tea.MouseMotionMsg{X: in.X + 3, Y: y, Button: tea.MouseLeft})
	}
	h.Send(tea.MouseReleaseMsg{X: in.X + 3, Y: top - 4, Button: tea.MouseLeft})
	got := lastClipboard(h)
	if n := strings.Count(got, "\n") + 1; n != 6 {
		t.Fatalf("5 lines scrolled into the selection plus the press line, got %d: %q", n, got)
	}
}

// busyDev is #dev with 40 more messages ("줄0".."줄39"), the list following
// the bottom.
func busyDev(t *testing.T) *uitest.Harness {
	t.Helper()
	h := devChannel(t, uitest.Size{W: 120, H: 40})
	dev, minji := devID(t, h), userBy(t, h.Context().Store, "minji")
	for i := range 40 {
		h.Event("message.created", realtime.MessageEvent{Message: mkMsg(dev, 900+i, minji, "줄"+strconv.Itoa(i), h.Context().Now())})
	}
	return h
}

// TestClickWhileContentShifts: a press and a release on the same cell are a
// click even when the rows under the pointer move in between (a new message
// while following the bottom, the typing line): nothing is copied, the
// selected row's thread opens, the first click only selects.
func TestClickWhileContentShifts(t *testing.T) {
	shifts := map[string]func(h *uitest.Harness, i int){
		"message.created": func(h *uitest.Harness, i int) {
			dev, junho := devID(t, h), userBy(t, h.Context().Store, "junho")
			h.Event("message.created", realtime.MessageEvent{Message: mkMsg(dev, 990+i, junho, "새것"+strconv.Itoa(i), h.Context().Now())})
		},
		"typing": func(h *uitest.Harness, _ int) {
			junho := userBy(t, h.Context().Store, "junho")
			h.Event("typing", realtime.Typing{ChannelID: devID(t, h), UserID: junho.ID, ExpiresInMS: 6000})
		},
	}
	for name, shift := range shifts {
		t.Run(name, func(t *testing.T) {
			h := busyDev(t)
			want := channelOf(t, h).pane.rows[channelOf(t, h).pane.index[selectIDOf(t, h, "줄38")]].ID
			for i := range 2 { // the selecting click, then the opening click
				x, y := posOf(t, h, layout.SlotMain, "줄38")
				h.Send(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
				shift(h, i)
				h.Screen()
				h.Send(tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft})
				if c := h.Clipboards(); len(c) != 0 || len(clipboards(h)) != 0 {
					t.Fatalf("click %d copied %q", i, c)
				}
				if i == 0 {
					if got := selectedID(t, h); got != want {
						t.Fatalf("the first click selects 줄38, got %q", got)
					}
					if _, ok := threadRoute(h); ok {
						t.Fatal("the first click opens nothing")
					}
				}
			}
			if r, ok := threadRoute(h); !ok || r.ID != want {
				t.Fatalf("the second click opens 줄38's thread: %+v", h.Model.Frame().Routes)
			}
		})
	}
}

// selectIDOf is the row ID of the message whose body is body.
func selectIDOf(t *testing.T, h *uitest.Harness, body string) string {
	t.Helper()
	for _, r := range channelOf(t, h).pane.rows {
		if r.Msg != nil && r.Msg.Body == body {
			return r.ID
		}
	}
	t.Fatalf("no message %q", body)
	return ""
}

// TestPressDoesNotFlash: a press alone paints no highlight (a click must not
// blink its row inverse); the first motion does.
func TestPressDoesNotFlash(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 120, H: 40})
	x, y := posOf(t, h, layout.SlotMain, "민지 11:02")
	h.Send(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	if hl := highlighted(h); len(hl) != 0 {
		t.Fatalf("a press paints %q", hl)
	}
	h.Send(tea.MouseMotionMsg{X: x + 1, Y: y, Button: tea.MouseLeft}) // a one-column wobble
	if hl := highlighted(h); len(hl) != 0 {
		t.Fatalf("a wobble paints %q", hl)
	}
	h.Send(tea.MouseMotionMsg{X: x, Y: y + 1, Button: tea.MouseLeft})
	if hl := highlighted(h); len(hl) != 2 {
		t.Fatalf("a drag paints its lines, got %q", hl)
	}
}

// TestDragPastTheTopLoadsOlder: held above the list at the first loaded line,
// the drag fetches the older page as the wheel does.
func TestDragPastTheTopLoadsOlder(t *testing.T) {
	h := busyDev(t)
	dev := devID(t, h)
	w := chatOf(h.Context()).Window(dev)
	w.Replace(w.Items(), true, false) // more history before the loaded page
	path := "/v1/channels/" + dev + "/messages"
	before := len(reqs(h.API, "GET", path))
	p, _ := h.Model.Frame().Layout.Pane(layout.SlotMain)
	in := p.Rect.Inner()
	h.Send(tea.MouseClickMsg{X: in.X + 3, Y: in.Y + 1, Button: tea.MouseLeft})
	for range 200 { // enough motions to scroll through the page
		h.Send(tea.MouseMotionMsg{X: in.X + 3, Y: in.Y - 2, Button: tea.MouseLeft})
	}
	got := reqs(h.API, "GET", path)[before:]
	if len(got) != 1 || !strings.Contains(got[0].Query, "before=") {
		t.Fatalf("one older-page fetch, got %+v", got)
	}
	h.Send(tea.MouseReleaseMsg{X: in.X + 3, Y: in.Y - 2, Button: tea.MouseLeft})
}
