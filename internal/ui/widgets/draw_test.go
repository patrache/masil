package widgets_test

import (
	"fmt"
	"image/color"
	"math"
	"strings"
	"testing"
	"time"
	_ "time/tzdata"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/config"
	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/widgets"
)

var (
	th       = theme.Get(theme.Amber)
	testNow  = time.Date(2026, 11, 5, 2, 5, 0, 0, time.UTC)
	seoul, _ = time.LoadLocation("Asia/Seoul")
)

// recTimers is a test-local ui.Timers that records every (d, msg) and returns nil.
type recTimers struct {
	ds   []time.Duration
	msgs []tea.Msg
}

func (r *recTimers) After(d time.Duration, msg tea.Msg) tea.Cmd {
	r.ds = append(r.ds, d)
	r.msgs = append(r.msgs, msg)
	return nil
}

func (r *recTimers) last() tea.Msg { return r.msgs[len(r.msgs)-1] }

func newCtx(timers ui.Timers, animations bool) *ui.Context {
	cfg := config.Defaults()
	cfg.UI.Animations = animations
	clk := uitest.NewFakeClock(testNow)
	if timers == nil {
		timers = clk
	}
	return &ui.Context{Theme: th, Config: cfg, Timers: timers, Now: clk.Now, Loc: seoul}
}

// sgr returns the truecolor SGR fragment for c ("38;2;r;g;b" or "48;2;r;g;b").
func sgr(prefix int, c color.Color) string {
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("%d;2;%d;%d;%d", prefix, r>>8, g>>8, b>>8)
}

func sameColor(a, b color.Color) bool {
	if a == nil || b == nil {
		return a == b
	}
	ar, ag, ab, _ := a.RGBA()
	br, bg, bb, _ := b.RGBA()
	return ar == br && ag == bg && ab == bb
}

// assertCells checks each line is exactly w cells under both width methods.
func assertCells(t *testing.T, lines []string, w int) {
	t.Helper()
	for i, l := range lines {
		if sw, wc := ansi.StringWidth(l), ansi.StringWidthWc(l); sw != w || wc != w {
			t.Errorf("line %d: width %d / wc %d, want %d: %q", i, sw, wc, w, ansi.Strip(l))
		}
	}
}

func assertAtMost(t *testing.T, lines []string, w int) {
	t.Helper()
	for i, l := range lines {
		if sw, wc := ansi.StringWidth(l), ansi.StringWidthWc(l); sw > w || wc != sw {
			t.Errorf("line %d: width %d / wc %d, want <= %d: %q", i, sw, wc, w, ansi.Strip(l))
		}
	}
}

func TestFrame(t *testing.T) {
	out := widgets.Frame(th, "#개발", ui.Badge{Count: 3}, true, ui.Size{W: 30, H: 5}, "본문")
	lines := strings.Split(out, "\n")
	if len(lines) != 5 {
		t.Fatalf("lines = %d, want 5", len(lines))
	}
	assertCells(t, lines, 30)
	top := ansi.Strip(lines[0])
	if !strings.HasPrefix(top, "╔═ #개발  3 ═") || !strings.HasSuffix(top, "═╗") {
		t.Errorf("top = %q", top)
	}
	if got := ansi.Strip(lines[1]); !strings.HasPrefix(got, "║본문") || !strings.HasSuffix(got, "║") {
		t.Errorf("content line = %q", got)
	}
	if got := ansi.Strip(lines[4]); got != "╚"+strings.Repeat("═", 28)+"╝" {
		t.Errorf("bottom = %q", got)
	}
	// focused border uses acc, blurred uses line
	if !strings.Contains(lines[0], sgr(38, th.P.Acc)) {
		t.Error("focused frame border not acc colored")
	}
	blurred := widgets.Frame(th, "x", ui.Badge{}, false, ui.Size{W: 10, H: 3}, "")
	if !strings.Contains(blurred, sgr(38, th.P.Line)) {
		t.Error("blurred frame border not line colored")
	}

	// long title is truncated with … to W-6
	long := widgets.Frame(th, strings.Repeat("가나다라", 10), ui.Badge{}, false, ui.Size{W: 20, H: 3}, "")
	ll := strings.Split(long, "\n")
	assertCells(t, ll, 20)
	if top := ansi.Strip(ll[0]); !strings.Contains(top, "…") || !strings.HasSuffix(top, "═╗") {
		t.Errorf("long title top = %q", top)
	}

	// dot badge keeps a blank before the border fill
	dot := ansi.Strip(strings.Split(widgets.Frame(th, "a", ui.Badge{Dot: true}, true, ui.Size{W: 12, H: 3}, ""), "\n")[0])
	if !strings.HasPrefix(dot, "╔═ a ● ═") {
		t.Errorf("dot top = %q", dot)
	}

	// tiny sizes never panic and stay exact
	if got := ansi.Strip(widgets.Frame(th, "제목", ui.Badge{Count: 5}, true, ui.Size{W: 2, H: 2}, "x")); got != "╔╗\n╚╝" {
		t.Errorf("2x2 frame = %q", got)
	}
	for w := 0; w <= 8; w++ {
		for h := 0; h <= 4; h++ {
			out := widgets.Frame(th, "#개발채널", ui.Badge{Count: 120, Mention: true}, true, ui.Size{W: w, H: h}, "본문\n둘째 줄")
			if w == 0 || h == 0 {
				if out != "" {
					t.Errorf("%dx%d frame = %q, want empty", w, h, out)
				}
				continue
			}
			ls := strings.Split(out, "\n")
			if len(ls) != h {
				t.Errorf("%dx%d frame has %d lines", w, h, len(ls))
			}
			assertCells(t, ls, w)
		}
	}

	// hostile content is fit, never breaks the border
	hostile := widgets.Frame(th, "❄️👨‍👩‍👧", ui.Badge{}, true, ui.Size{W: 16, H: 4}, "아주 긴 본문 줄이 여기 있어요 ❄\n"+strings.Repeat("x", 40))
	assertCells(t, strings.Split(hostile, "\n"), 16)
	// a raw tab in content must not widen the line when styled
	assertCells(t, strings.Split(widgets.Frame(th, "", ui.Badge{}, false, ui.Size{W: 10, H: 3}, "a\tb\tc\td"), "\n"), 10)
}

func TestBox(t *testing.T) {
	out := widgets.Box(th, th.S.InputFocused, ui.Size{W: 12, H: 3}, "안녕")
	lines := strings.Split(out, "\n")
	if len(lines) != 3 {
		t.Fatalf("lines = %d", len(lines))
	}
	assertCells(t, lines, 12)
	if got := ansi.Strip(lines[0]); got != "╔"+strings.Repeat("═", 10)+"╗" {
		t.Errorf("top = %q", got)
	}
	if got := ansi.Strip(lines[1]); got != "║안녕      ║" {
		t.Errorf("mid = %q", got)
	}
	if !strings.Contains(lines[0], sgr(38, th.P.Acc)) {
		t.Error("InputFocused box border not acc colored")
	}
	if !strings.Contains(widgets.Box(th, th.S.Input, ui.Size{W: 5, H: 3}, ""), sgr(38, th.P.Line)) {
		t.Error("Input box border not line colored")
	}
	if got := ansi.Strip(widgets.Box(th, th.S.Input, ui.Size{W: 2, H: 2}, "zz")); got != "╔╗\n╚╝" {
		t.Errorf("2x2 box = %q", got)
	}
	// a non-amber theme border comes from the theme (Rounded for charm)
	charm := theme.Get(theme.Charm)
	if got := ansi.Strip(strings.Split(widgets.Box(charm, charm.S.Input, ui.Size{W: 4, H: 3}, ""), "\n")[0]); got != "╭──╮" {
		t.Errorf("charm top = %q", got)
	}
}

func TestBadge(t *testing.T) {
	cases := []struct {
		b    ui.Badge
		want string
	}{
		{ui.Badge{}, ""},
		{ui.Badge{Count: 0}, ""},
		{ui.Badge{Count: -2}, ""},
		{ui.Badge{Count: 3}, " 3 "},
		{ui.Badge{Count: 99}, " 99 "},
		{ui.Badge{Count: 100}, " 99+ "},
		{ui.Badge{Dot: true}, "●"},
		{ui.Badge{Count: 3, Mention: true}, " 3 "},
	}
	for _, c := range cases {
		got := widgets.Badge(th, c.b)
		if ansi.Strip(got) != c.want {
			t.Errorf("Badge(%+v) = %q, want %q", c.b, ansi.Strip(got), c.want)
		}
		if c.want == "" && got != "" {
			t.Errorf("Badge(%+v) styled = %q, want empty", c.b, got)
		}
	}
	m := widgets.Badge(th, ui.Badge{Count: 3, Mention: true})
	if !strings.Contains(m, sgr(48, th.P.Acc2)) {
		t.Errorf("mention badge not acc2 bg: %q", m)
	}
	if !strings.Contains(widgets.Badge(th, ui.Badge{Count: 3}), sgr(48, th.P.Acc2)) {
		t.Error("count badge not acc2 bg")
	}
}

func TestPresenceDot(t *testing.T) {
	cases := map[api.PresenceStatus]struct {
		glyph string
		fg    color.Color
	}{
		api.PresenceOnline:  {"●", th.P.Acc3},
		api.PresenceAway:    {"◐", th.P.Acc},
		api.PresenceDND:     {"⊘", th.P.Acc2},
		api.PresenceOffline: {"○", th.P.Dim},
		"":                  {"○", th.P.Dim},
		"weird":             {"○", th.P.Dim},
	}
	for s, want := range cases {
		got := widgets.PresenceDot(th, s)
		if ansi.Strip(got) != want.glyph {
			t.Errorf("PresenceDot(%q) = %q, want %q", s, ansi.Strip(got), want.glyph)
		}
		if ansi.StringWidth(got) != 1 || ansi.StringWidthWc(got) != 1 {
			t.Errorf("PresenceDot(%q) width not 1", s)
		}
		if !strings.Contains(got, sgr(38, want.fg)) {
			t.Errorf("PresenceDot(%q) color wrong: %q", s, got)
		}
	}
}

func TestAvatarAndName(t *testing.T) {
	u := api.UserSummary{ID: "u_minji", Handle: "minji", DisplayName: "민지"}
	if got := ansi.Strip(widgets.Avatar(th, u, 2)); got != "민" {
		t.Errorf("Avatar(2) = %q", got)
	}
	if got := ansi.Strip(widgets.Avatar(th, u, 1)); got != "M" {
		t.Errorf("Avatar(1) = %q", got)
	}
	av := widgets.Avatar(th, u, 4)
	if ansi.Strip(av) != " 민 " || ansi.StringWidth(av) != 4 {
		t.Errorf("Avatar(4) = %q", ansi.Strip(av))
	}
	if !strings.Contains(av, sgr(48, th.P.Chip)) || !strings.Contains(av, sgr(38, th.UserColor(u.ID, false))) {
		t.Errorf("Avatar not chip bg + user color: %q", av)
	}
	if widgets.Avatar(th, u, 0) != "" {
		t.Error("Avatar(0) not empty")
	}
	// hostile display name: sanitized, width exact
	h := api.UserSummary{ID: "u_x", Handle: "x", DisplayName: "👨‍👩‍👧 가족"}
	for w := 1; w <= 5; w++ {
		a := widgets.Avatar(th, h, w)
		if ansi.StringWidth(a) != w || ansi.StringWidthWc(a) != w {
			t.Errorf("hostile Avatar(%d) width %d/%d: %q", w, ansi.StringWidth(a), ansi.StringWidthWc(a), ansi.Strip(a))
		}
	}
	// empty display name falls back to the handle initial
	if got := ansi.Strip(widgets.Avatar(th, api.UserSummary{ID: "u", Handle: "sam"}, 2)); got != "S " {
		t.Errorf("Avatar(no name) = %q", got)
	}

	self := widgets.UserName(th, u, true)
	if ansi.Strip(self) != "민지" || !strings.Contains(self, sgr(38, th.P.Acc)) {
		t.Errorf("UserName self = %q", self)
	}
	other := widgets.UserName(th, u, false)
	if !strings.Contains(other, sgr(38, th.UserColor(u.ID, false))) {
		t.Errorf("UserName other = %q", other)
	}
	if got := ansi.Strip(widgets.UserName(th, api.UserSummary{ID: "u", Handle: "sam"}, false)); got != "sam" {
		t.Errorf("UserName fallback = %q", got)
	}
	if got := ansi.Strip(widgets.UserName(th, api.UserSummary{ID: "u", DisplayName: "❄️눈\x1b[31m"}, false)); got != "❄눈" {
		t.Errorf("UserName sanitized = %q", got)
	}
}

func TestReactionChips(t *testing.T) {
	rs := []api.ReactionSummary{{Emoji: "👍", Count: 2, Me: true}, {Emoji: "👀", Count: 1}}
	lines := widgets.ReactionChips(th, rs, 80)
	if len(lines) != 1 || ansi.Strip(lines[0]) != "👍 2  👀 1" {
		t.Fatalf("chips = %q", lines)
	}
	mine := th.S.ReactionMine.Render("👍 2")
	if !strings.Contains(lines[0], mine) {
		t.Errorf("my chip not ReactionMine styled: %q", lines[0])
	}
	narrow := widgets.ReactionChips(th, rs, 6)
	if len(narrow) != 2 || ansi.Strip(narrow[0]) != "👍 2" || ansi.Strip(narrow[1]) != "👀 1" {
		t.Errorf("narrow = %q", narrow)
	}
	assertAtMost(t, narrow, 6)
	if got := widgets.ReactionChips(th, nil, 80); len(got) != 0 {
		t.Errorf("nil reactions = %q", got)
	}
	// hostile emoji keys are sanitized for display; widths stay exact for tiny widths
	hostile := []api.ReactionSummary{{Emoji: "❄️", Count: 12}, {Emoji: "👍🏽", Count: 3}, {Emoji: "👨‍👩‍👧", Count: 1000}}
	for _, w := range []int{1, 3, 5, 12, 40} {
		assertAtMost(t, widgets.ReactionChips(th, hostile, w), w)
	}
	if got := ansi.Strip(widgets.ReactionChips(th, hostile, 80)[0]); got != "❄ 12  👍 3  👨 1000" {
		t.Errorf("hostile chips = %q", got)
	}
}

func pollFixture() api.Poll {
	return api.Poll{
		ID:         "p1",
		Question:   "점심 메뉴",
		Options:    []api.PollOption{{Idx: 0, Text: "국밥", VoteCount: 4}, {Idx: 1, Text: "돈까스", VoteCount: 2}},
		VoterCount: 6,
		MyVotes:    []int{0},
	}
}

func TestPollView(t *testing.T) {
	p := pollFixture()
	lines := widgets.PollView(th, p, 80, -1)
	if len(lines) != 3 {
		t.Fatalf("poll lines = %d: %q", len(lines), lines)
	}
	if got := ansi.Strip(lines[0]); got != "점심 메뉴 · 6명 참여" {
		t.Errorf("header = %q", got)
	}
	l1, l2 := ansi.Strip(lines[1]), ansi.Strip(lines[2])
	if !strings.HasPrefix(l1, "✓ 국밥") || !strings.HasSuffix(l1, " 4") {
		t.Errorf("option 0 = %q", l1)
	}
	if !strings.HasPrefix(l2, "  돈까스") || !strings.HasSuffix(l2, " 2") {
		t.Errorf("option 1 = %q", l2)
	}
	for i, l := range []string{l1, l2} {
		full, empty := strings.Count(l, "█"), strings.Count(l, "░")
		bar := full + empty
		if bar == 0 {
			t.Fatalf("option %d has no bar: %q", i, l)
		}
		want := float64(bar) * float64(p.Options[i].VoteCount) / 6
		if math.Abs(float64(full)-want) > 0.5 {
			t.Errorf("option %d: %d/%d cells full, want ~%.1f", i, full, bar, want)
		}
	}
	// both bars have the same length (labels padded to the same width)
	if strings.Count(l1, "█")+strings.Count(l1, "░") != strings.Count(l2, "█")+strings.Count(l2, "░") {
		t.Error("bars differ in length")
	}

	p.Closed = true
	if got := ansi.Strip(widgets.PollView(th, p, 80, -1)[0]); got != "점심 메뉴 · 6명 참여 (마감)" {
		t.Errorf("closed header = %q", got)
	}
	for _, w := range []int{80, 30, 12, 5, 1} {
		assertAtMost(t, widgets.PollView(th, p, w, 1), w)
	}
	// selected row carries the selection background
	sel := widgets.PollView(th, pollFixture(), 40, 1)
	if !strings.Contains(sel[2], sgr(48, th.P.SelBG)) || strings.Contains(sel[1], sgr(48, th.P.SelBG)) {
		t.Error("selected row styling wrong")
	}
	// zero votes: no division by zero, bars empty
	z := api.Poll{Question: "q", Options: []api.PollOption{{Idx: 0, Text: "a"}}}
	zl := widgets.PollView(th, z, 30, -1)
	if strings.Contains(ansi.Strip(zl[1]), "█") {
		t.Errorf("zero poll = %q", zl)
	}
	// hostile option text
	hp := api.Poll{Question: "❄️ 투표\x07", Options: []api.PollOption{{Text: "👨‍👩‍👧 가족 여행을 아주 멀리 떠나요", VoteCount: 1}}, VoterCount: 1}
	for _, w := range []int{60, 20, 8} {
		assertAtMost(t, widgets.PollView(th, hp, w, 0), w)
	}
}

func makeTabs(n int) []widgets.Tab {
	var tabs []widgets.Tab
	for i := range n {
		tabs = append(tabs, widgets.Tab{Label: fmt.Sprintf("#채널%d", i+1)})
	}
	return tabs
}

func TestTabsOverflow(t *testing.T) {
	tb := widgets.NewTabs()
	tb.Set(makeTabs(3), 0)
	all := tb.View(th, 40)
	if ansi.StringWidth(all) != 40 || strings.Contains(ansi.Strip(all), "‹") || strings.Contains(ansi.Strip(all), "›") {
		t.Errorf("fitting tabs = %q", ansi.Strip(all))
	}
	if !strings.Contains(all, th.S.TabActive.Render(" #채널1 ")) {
		t.Errorf("active tab not TabActive: %q", all)
	}
	if i, ok := tb.Hit(1); !ok || i != 0 {
		t.Errorf("Hit(1) = %d,%v", i, ok)
	}
	if _, ok := tb.Hit(39); ok {
		t.Error("Hit on padding should miss")
	}

	tabs := makeTabs(9)
	tabs[2].Badge = ui.Badge{Count: 4}
	tb.Set(tabs, 8)
	out := tb.View(th, 30)
	plain := ansi.Strip(out)
	if ansi.StringWidth(out) != 30 || ansi.StringWidthWc(out) != 30 {
		t.Errorf("width = %d: %q", ansi.StringWidth(out), plain)
	}
	if !strings.Contains(plain, "‹") || !strings.Contains(plain, "#채널9") || strings.Contains(plain, "›") {
		t.Errorf("overflow view = %q", plain)
	}
	// Hit maps every column of the active label to index 8
	at := strings.Index(plain, "#채널9")
	if at < 0 {
		t.Fatalf("active label missing: %q", plain)
	}
	col := ansi.StringWidth(plain[:at])
	if i, ok := tb.Hit(col); !ok || i != 8 {
		t.Errorf("Hit(%d) = %d,%v, want 8", col, i, ok)
	}
	// the left arrow selects the nearest hidden tab
	if i, ok := tb.Hit(0); !ok || i >= 8 {
		t.Errorf("Hit(arrow) = %d,%v", i, ok)
	}
	for _, x := range []int{-1, 30, 100} {
		if _, ok := tb.Hit(x); ok {
			t.Errorf("Hit(%d) should miss", x)
		}
	}

	// active in the middle: both arrows
	tb.Set(makeTabs(9), 4)
	mid := ansi.Strip(tb.View(th, 30))
	if !strings.Contains(mid, "‹") || !strings.Contains(mid, "›") || !strings.Contains(mid, "#채널5") {
		t.Errorf("middle view = %q", mid)
	}
	// every width stays exact, even absurdly small
	for w := 0; w <= 32; w++ {
		v := tb.View(th, w)
		if ansi.StringWidth(v) != w || ansi.StringWidthWc(v) != w {
			t.Errorf("width %d: got %d: %q", w, ansi.StringWidth(v), ansi.Strip(v))
		}
	}
	// empty tab bar
	empty := widgets.NewTabs()
	if v := empty.View(th, 10); ansi.StringWidth(v) != 10 {
		t.Errorf("empty tabs width = %d", ansi.StringWidth(v))
	}
	if _, ok := empty.Hit(0); ok {
		t.Error("empty Hit should miss")
	}
}

func TestToastsIconsWidth(t *testing.T) {
	icons := map[ui.ToastKind]string{ui.ToastInfo: "○", ui.ToastSuccess: "●", ui.ToastError: "✕", ui.ToastAlert: "★"}
	for kind, icon := range icons {
		if ansi.StringWidth(icon) != 1 || ansi.StringWidthWc(icon) != 1 {
			t.Errorf("icon %q not 1 cell", icon)
		}
		lines := widgets.Toasts(th, []ui.ToastMsg{{Text: "저장했어요", Kind: kind}}, 30)
		if len(lines) != 1 || !strings.HasPrefix(ansi.Strip(lines[0]), icon+" 저장했어요") {
			t.Errorf("toast kind %d = %q", kind, lines)
		}
		assertAtMost(t, lines, 30)
	}
	items := []ui.ToastMsg{
		{Text: "서버에 연결할 수 없어요. 잠시 후 다시 시도해 주세요", Kind: ui.ToastError},
		{Text: "❄️ 민지: 👨‍👩‍👧 안녕\x1b[31m!", Kind: ui.ToastAlert},
	}
	for _, w := range []int{40, 20, 10, 3, 1} {
		lines := widgets.Toasts(th, items, w)
		if len(lines) < 2 {
			t.Errorf("width %d: %d lines", w, len(lines))
		}
		assertAtMost(t, lines, w)
	}
	// wrapped continuation lines are indented under the text
	wrapped := widgets.Toasts(th, items[:1], 20)
	if len(wrapped) < 2 || !strings.HasPrefix(ansi.Strip(wrapped[1]), "  ") {
		t.Errorf("wrapped = %q", wrapped)
	}
	if got := widgets.Toasts(th, nil, 20); len(got) != 0 {
		t.Errorf("no toasts = %q", got)
	}
}

func TestSpinner(t *testing.T) {
	// FakeClock records the first tick at Dot.FPS
	clk := uitest.NewFakeClock(testNow)
	ctx := newCtx(clk, true)
	s := widgets.NewSpinner()
	cmd := s.Start(ctx)
	if cmd == nil {
		t.Fatal("Start returned nil with animations on")
	}
	if msg := cmd(); msg != nil {
		t.Errorf("FakeClock cmd returned %v", msg)
	}
	if p := clk.Pending(); len(p) != 1 || p[0] != spinner.Dot.FPS {
		t.Errorf("Pending = %v, want [%v]", p, spinner.Dot.FPS)
	}

	rec := &recTimers{}
	ctx = newCtx(rec, true)
	s = widgets.NewSpinner()
	first := ansi.Strip(s.View(th))
	if first != spinner.Dot.Frames[0] {
		t.Errorf("first frame = %q", first)
	}
	s.Start(ctx)
	if len(rec.msgs) != 1 || rec.ds[0] != spinner.Dot.FPS {
		t.Fatalf("recorded = %v", rec.ds)
	}
	stale := rec.last()
	if cmd := s.Update(ctx, stale); cmd != nil {
		t.Error("recTimers cmd should be nil")
	}
	if len(rec.msgs) != 2 {
		t.Fatalf("tick did not re-arm: %d", len(rec.msgs))
	}
	if got := ansi.Strip(s.View(th)); got != spinner.Dot.Frames[1] {
		t.Errorf("after tick frame = %q", got)
	}
	// the frame is acc colored
	if !strings.Contains(s.View(th), sgr(38, th.P.Acc)) {
		t.Error("spinner not acc colored")
	}
	// stale generation is ignored
	s.Stop()
	s.Start(ctx)
	n := len(rec.msgs)
	before := s.View(th)
	s.Update(ctx, stale)
	if len(rec.msgs) != n || s.View(th) != before {
		t.Error("stale tick advanced/re-armed the spinner")
	}
	// fresh tick still works; frames wrap around
	for range len(spinner.Dot.Frames) {
		s.Update(ctx, rec.last())
	}
	if len(rec.msgs) != n+len(spinner.Dot.Frames) {
		t.Errorf("fresh ticks re-armed %d times", len(rec.msgs)-n)
	}
	// after Stop, the pending tick is dropped and nothing re-arms
	s.Stop()
	n = len(rec.msgs)
	s.Update(ctx, rec.last())
	if len(rec.msgs) != n {
		t.Error("stopped spinner re-armed")
	}
	// unrelated msgs are ignored
	if s.Update(ctx, tea.KeyPressMsg{}) != nil {
		t.Error("foreign msg produced a cmd")
	}
	// two spinners do not consume each other's ticks
	a, b := widgets.NewSpinner(), widgets.NewSpinner()
	a.Start(ctx)
	aTick := rec.last()
	b.Start(ctx)
	n = len(rec.msgs)
	b.Update(ctx, aTick)
	if len(rec.msgs) != n {
		t.Error("spinner b consumed spinner a's tick")
	}

	// animations off: Start nil, static first frame
	off := newCtx(rec, false)
	s = widgets.NewSpinner()
	n = len(rec.msgs)
	if s.Start(off) != nil || len(rec.msgs) != n {
		t.Error("Start with animations off armed a tick")
	}
	if got := ansi.Strip(s.View(th)); got != spinner.Dot.Frames[0] {
		t.Errorf("static frame = %q", got)
	}
	// width is stable under both width methods
	if v := s.View(th); ansi.StringWidth(v) != ansi.StringWidthWc(v) {
		t.Error("spinner width differs between methods")
	}
}

func TestFade(t *testing.T) {
	rec := &recTimers{}
	ctx := newCtx(rec, true)
	var f widgets.Fade
	base := th.S.Base
	if !sameColor(f.Style(th, "m1").GetBackground(), th.P.BG) {
		t.Error("unknown id should use base bg")
	}
	if f.Start(ctx, "m1") != nil {
		t.Error("recTimers Start cmd should be nil")
	}
	if len(rec.ds) != 1 || rec.ds[0] != 250*time.Millisecond {
		t.Fatalf("Start armed %v", rec.ds)
	}
	if !sameColor(f.Style(th, "m1").GetBackground(), th.P.SelBG) {
		t.Errorf("start bg = %v, want SelBG", f.Style(th, "m1").GetBackground())
	}
	if !sameColor(f.Style(th, "m1").GetForeground(), th.P.FG) {
		t.Error("fade style not derived from Base")
	}
	steps := lipgloss.Blend1D(5, th.P.SelBG, th.P.BG)
	stale := rec.last()
	for i := 1; i <= 4; i++ {
		f.Update(ctx, rec.last())
		got := f.Style(th, "m1").GetBackground()
		if !sameColor(got, steps[i]) {
			t.Errorf("after tick %d bg = %v, want %v", i, got, steps[i])
		}
	}
	if !sameColor(f.Style(th, "m1").GetBackground(), th.P.BG) {
		t.Error("fade did not end at BG")
	}
	if len(rec.ds) != 4 {
		t.Errorf("armed %d ticks, want 4 (start + 3 re-arms)", len(rec.ds))
	}
	_ = stale

	// restarting the same id resets; the old tick is stale
	f.Start(ctx, "m2")
	old := rec.last()
	f.Start(ctx, "m2")
	n := len(rec.ds)
	f.Update(ctx, old)
	if len(rec.ds) != n || !sameColor(f.Style(th, "m2").GetBackground(), th.P.SelBG) {
		t.Error("stale fade tick advanced m2")
	}
	// two Fades with the same item id are independent
	var g widgets.Fade
	g.Start(ctx, "m2")
	n = len(rec.ds)
	f.Update(ctx, rec.last())
	if len(rec.ds) != n {
		t.Error("fade f consumed fade g's tick")
	}
	if f.Update(ctx, tea.KeyPressMsg{}) != nil {
		t.Error("foreign msg produced a cmd")
	}

	// animations off: nothing, Style is base
	off := newCtx(rec, false)
	var h widgets.Fade
	n = len(rec.ds)
	if h.Start(off, "m3") != nil || len(rec.ds) != n {
		t.Error("Start with animations off armed a tick")
	}
	if !sameColor(h.Style(th, "m3").GetBackground(), base.GetBackground()) {
		t.Error("animations off style is not base")
	}

	// FakeClock path: the cmd records one 250ms timer
	clk := uitest.NewFakeClock(testNow)
	var k widgets.Fade
	cmd := k.Start(newCtx(clk, true), "m4")
	if cmd == nil {
		t.Fatal("Start returned nil with animations on")
	}
	cmd()
	if p := clk.Pending(); len(p) != 1 || p[0] != 250*time.Millisecond {
		t.Errorf("Pending = %v", p)
	}
}

func TestEmpty(t *testing.T) {
	out := widgets.Empty(th, ui.Size{W: 40, H: 10}, "💬", "메시지가 없어요", "i 를 눌러 입력")
	lines := strings.Split(out, "\n")
	if len(lines) != 10 {
		t.Fatalf("lines = %d", len(lines))
	}
	assertCells(t, lines, 40)
	var texts []string
	first := -1
	for i, l := range lines {
		if s := strings.TrimSpace(ansi.Strip(l)); s != "" {
			if first < 0 {
				first = i
			}
			texts = append(texts, s)
		}
	}
	if strings.Join(texts, "|") != "💬|메시지가 없어요|i 를 눌러 입력" {
		t.Errorf("texts = %q", texts)
	}
	// vertically centered: 4 rows (icon, blank, title, hint) in 10 => starts at row 3
	if first != 3 {
		t.Errorf("block starts at row %d, want 3", first)
	}
	// horizontally centered
	title := ansi.Strip(lines[5])
	left := len(title) - len(strings.TrimLeft(title, " "))
	right := len(title) - len(strings.TrimRight(title, " "))
	if d := left - right; d < -1 || d > 1 {
		t.Errorf("title not centered: %q", title)
	}
	for _, sz := range []ui.Size{{W: 1, H: 1}, {W: 10, H: 2}, {W: 5, H: 1}, {W: 3, H: 6}} {
		e := widgets.Empty(th, ui.Size{W: sz.W, H: sz.H}, "💬", "메시지가 없어요", "i 를 눌러 입력")
		el := strings.Split(e, "\n")
		if len(el) != sz.H {
			t.Errorf("%v: %d lines", sz, len(el))
		}
		assertCells(t, el, sz.W)
	}
	// a 1-row empty state shows the title, not the icon
	if got := strings.TrimSpace(ansi.Strip(widgets.Empty(th, ui.Size{W: 30, H: 1}, "💬", "비었어요", "힌트"))); got != "비었어요" {
		t.Errorf("1-row = %q", got)
	}
	if widgets.Empty(th, ui.Size{}, "a", "b", "c") != "" {
		t.Error("zero size Empty not empty")
	}
}

func TestPlaceholder(t *testing.T) {
	ctx := newCtx(nil, true)
	p := widgets.Placeholder("준비 중")
	if p == nil {
		t.Fatal("Placeholder returned nil")
	}
	if p.Init(ctx) != nil || p.Update(ctx, tea.KeyPressMsg{}) != nil {
		t.Error("placeholder produced cmds")
	}
	p.Resize(ctx, ui.Size{W: 30, H: 7})
	out := p.View(ctx, true)
	lines := strings.Split(out, "\n")
	if len(lines) != 7 {
		t.Fatalf("lines = %d", len(lines))
	}
	assertCells(t, lines, 30)
	if !strings.Contains(ansi.Strip(out), "준비 중") {
		t.Errorf("placeholder = %q", ansi.Strip(out))
	}
	// before any Resize it draws nothing rather than panicking
	if q := widgets.Placeholder("x"); q.View(ctx, false) != "" {
		t.Error("unsized placeholder not empty")
	}
}
