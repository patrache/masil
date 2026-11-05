package widgets_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/golden"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/widgets"
)

// gallery composes every drawing widget inside one full-screen Frame. The last row is a
// status line (AssertIntegrity check 4: the last row is never part of a bordered pane).
func gallery(w, h int) string {
	inner := ui.Size{W: w - 2, H: h - 3}
	iw := inner.W
	pad := func(s string) string {
		return s + th.S.Base.Render(strings.Repeat(" ", max(0, iw-ansi.StringWidth(s))))
	}
	sep := th.S.Base.Render(" ")

	var lines []string
	tabs := widgets.NewTabs()
	items := []widgets.Tab{{Label: "#일반"}, {Label: "#개발", Badge: ui.Badge{Count: 3}}, {Label: "#잡담", Badge: ui.Badge{Dot: true}}}
	for i := range 6 {
		items = append(items, widgets.Tab{Label: fmt.Sprintf("#방%d", i+1)})
	}
	items = append(items, widgets.Tab{Label: "@민지", Badge: ui.Badge{Count: 120, Mention: true}})
	tabs.Set(items, 1)
	lines = append(lines, tabs.View(th, iw))

	lines = append(lines, pad(th.S.Dim.Render("배지")+sep+widgets.Badge(th, ui.Badge{Count: 3})+sep+
		widgets.Badge(th, ui.Badge{Count: 100, Mention: true})+sep+widgets.Badge(th, ui.Badge{Dot: true})))

	var pres []string
	for _, p := range []struct {
		s     api.PresenceStatus
		label string
	}{{api.PresenceOnline, "온라인"}, {api.PresenceAway, "자리 비움"}, {api.PresenceDND, "방해 금지"}, {api.PresenceOffline, "오프라인"}} {
		pres = append(pres, widgets.PresenceDot(th, p.s)+sep+th.S.Base.Render(p.label))
	}
	lines = append(lines, pad(strings.Join(pres, th.S.Base.Render("  "))))

	minji := api.UserSummary{ID: "u_minji", Handle: "minji", DisplayName: "민지"}
	sam := api.UserSummary{ID: "u_sam", Handle: "sam", DisplayName: "샘"}
	lines = append(lines, pad(widgets.Avatar(th, minji, 4)+sep+widgets.UserName(th, minji, false)+
		th.S.Base.Render("  ")+widgets.Avatar(th, sam, 1)+sep+widgets.UserName(th, sam, true)))

	rs := []api.ReactionSummary{{Emoji: "👍", Count: 2, Me: true}, {Emoji: "👀", Count: 1}, {Emoji: "❄️", Count: 12}, {Emoji: "🎉", Count: 4}}
	for _, l := range widgets.ReactionChips(th, rs, iw) {
		lines = append(lines, pad(l))
	}
	for _, l := range widgets.PollView(th, pollFixture(), iw, 1) {
		lines = append(lines, pad(l))
	}
	toasts := []ui.ToastMsg{
		{Text: "저장했어요", Kind: ui.ToastSuccess},
		{Text: "서버에 연결할 수 없어요", Kind: ui.ToastError},
		{Text: "민지: 점심 먹었어?", Kind: ui.ToastAlert},
		{Text: "이미 신고한 대상이에요", Kind: ui.ToastInfo},
	}
	for _, l := range widgets.Toasts(th, toasts, iw) {
		lines = append(lines, pad(l))
	}
	if rest := inner.H - len(lines); rest > 0 {
		lines = append(lines, strings.Split(widgets.Empty(th, ui.Size{W: iw, H: rest}, "💬", "메시지가 없어요", "i 를 눌러 입력"), "\n")...)
	}

	frame := widgets.Frame(th, "위젯 갤러리", ui.Badge{Count: 7}, true, ui.Size{W: w, H: h - 1}, strings.Join(lines, "\n"))
	status := th.S.StatusBar.Render(textwidth.PadRight(" 위젯 갤러리", w))
	return frame + "\n" + status
}

func TestGalleryGolden(t *testing.T) {
	if len(uitest.Required) != 4 {
		t.Fatal("uitest.Required must list the 4 required sizes")
	}
	for _, sz := range uitest.Required {
		t.Run(fmt.Sprintf("%dx%d", sz.W, sz.H), func(t *testing.T) {
			out := gallery(sz.W, sz.H)
			uitest.AssertIntegrity(t, out, sz.W, sz.H,
				[]uitest.Box{{Rect: layout.Rect{X: 0, Y: 0, W: sz.W, H: sz.H - 1}, Bordered: true}}, th.Border)
			golden.RequireEqual(t, ansi.Strip(out))
		})
	}
}
