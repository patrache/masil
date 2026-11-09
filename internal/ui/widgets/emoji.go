package widgets

import (
	"slices"
	"strings"
	"sync"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
)

// emojiEntry is one pickable emoji with its search names (Korean and English).
type emojiEntry struct {
	emoji string
	names []string
}

// emojiSet is the default set (plan 03 Task 28): width-2 emoji only, no VS16.
var emojiSet = []emojiEntry{
	{"👍", []string{"좋아요", "최고", "thumbsup", "like"}},
	{"👎", []string{"싫어요", "별로", "thumbsdown", "dislike"}},
	{"😀", []string{"웃음", "미소", "grin", "smile"}},
	{"😂", []string{"폭소", "웃겨", "joy", "lol"}},
	{"😍", []string{"하트눈", "반함", "heart eyes", "love"}},
	{"🥲", []string{"웃픈", "감동", "smiling tear"}},
	{"😭", []string{"엉엉", "울음", "sob", "cry"}},
	{"😡", []string{"화남", "분노", "angry", "rage"}},
	{"🙏", []string{"부탁", "감사", "pray", "thanks"}},
	{"👏", []string{"박수", "짝짝", "clap"}},
	{"🎉", []string{"축하", "파티", "tada", "party"}},
	{"🔥", []string{"불", "대박", "fire", "hot"}},
	{"💯", []string{"백점", "완벽", "100", "hundred"}},
	{"✅", []string{"확인", "완료", "check", "done"}},
	{"❌", []string{"엑스", "아니", "cross", "no"}},
	{"👀", []string{"눈", "보는 중", "eyes", "look"}},
	{"🤔", []string{"생각", "고민", "thinking", "hmm"}},
	{"😅", []string{"머쓱", "식은땀", "sweat smile"}},
	{"😎", []string{"선글라스", "멋짐", "cool", "sunglasses"}},
	{"🙌", []string{"만세", "환호", "raised hands", "hooray"}},
	{"💪", []string{"힘", "화이팅", "muscle", "strong"}},
	{"🤝", []string{"악수", "협력", "handshake", "deal"}},
	{"🍚", []string{"밥", "식사", "rice", "meal"}},
	{"☕", []string{"커피", "카페", "coffee", "cafe"}},
	{"🍺", []string{"맥주", "한잔", "beer", "cheers"}},
	{"🎂", []string{"생일", "케이크", "cake", "birthday"}},
	{"🚀", []string{"로켓", "배포", "rocket", "ship"}},
	{"🐛", []string{"버그", "벌레", "bug"}},
	{"💡", []string{"아이디어", "전구", "idea", "bulb"}},
	{"📌", []string{"고정", "핀", "pin", "pushpin"}},
	{"⭐", []string{"별", "즐겨찾기", "star"}},
	{"💬", []string{"말풍선", "댓글", "speech", "comment"}},
	{"🚬", []string{"흡연", "담배", "smoking", "cigarette"}},
}

// emojiRecentMax is how many recently picked emoji are remembered (package memory).
const emojiRecentMax = 8

var emojiRecent struct {
	mu   sync.Mutex
	list []string // most recent first
}

func recentEmoji() []string {
	emojiRecent.mu.Lock()
	defer emojiRecent.mu.Unlock()
	return slices.Clone(emojiRecent.list)
}

func rememberEmoji(e string) {
	emojiRecent.mu.Lock()
	defer emojiRecent.mu.Unlock()
	l := slices.DeleteFunc(emojiRecent.list, func(s string) bool { return s == e })
	l = append([]string{e}, l...)
	emojiRecent.list = l[:min(len(l), emojiRecentMax)]
}

// emojiNames returns the names of e ("" entries skipped), or nil.
func emojiNames(e string) []string {
	for _, it := range emojiSet {
		if it.emoji == e {
			return it.names
		}
	}
	return nil
}

// emojiCell is the drawn width of one grid cell (a width-2 emoji + one blank).
const emojiCell = 3

// emojiPicker is widgets.EmojiPicker.
type emojiPicker struct {
	onPick func(string) tea.Cmd
	query  textinput.Model
	recent []string // snapshot taken when opened
	sel    int      // index into items()
	size   ui.Size

	hits map[int][]emojiHit // last layout: inner row -> cells
}

// emojiHit is one drawn grid cell [x0, x1) holding items()[idx].
type emojiHit struct{ x0, x1, idx int }

var (
	_ ui.Overlay      = (*emojiPicker)(nil)
	_ ui.KeyHandler   = (*emojiPicker)(nil)
	_ ui.MouseHandler = (*emojiPicker)(nil)
	_ ui.Cursorer     = (*emojiPicker)(nil)
	_ ui.Helper       = (*emojiPicker)(nil)
)

// EmojiPicker shows recent + common set, search by Korean/English name (CA 5.7).
// Typing filters (substring of any name, case-insensitive); arrows move, enter or a
// click picks: onPick(emoji) runs (nil = nothing), the emoji becomes the most recent
// one and the picker closes. esc (unhandled) closes it through the app.
func EmojiPicker(onPick func(emoji string) tea.Cmd) ui.Overlay {
	ti := newTextInput()
	ti.CharLimit = 32
	return &emojiPicker{onPick: onPick, query: ti, recent: recentEmoji()}
}

func (p *emojiPicker) Init(*ui.Context) tea.Cmd            { return p.query.Focus() }
func (p *emojiPicker) Resize(_ *ui.Context, size ui.Size)  { p.size = size }
func (p *emojiPicker) Bounds(body layout.Rect) layout.Rect { return centered(body, emojiW, emojiH) }
func (p *emojiPicker) Modal() bool                         { return true }

func (p *emojiPicker) Help(*ui.Context) []key.Binding {
	return []key.Binding{
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "고르기")),
		key.NewBinding(key.WithKeys("up", "down", "left", "right"), key.WithHelp("←↑↓→", "이동")),
		key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "닫기")),
	}
}

// sections are the drawn groups: (label, emoji) — recent + all without a query,
// the matches with one.
func (p *emojiPicker) sections() (labels []string, groups [][]string) {
	q := strings.ToLower(strings.TrimSpace(p.query.Value()))
	if q == "" {
		if len(p.recent) > 0 {
			labels, groups = append(labels, "최근"), append(groups, p.recent)
		}
		all := make([]string, 0, len(emojiSet))
		for _, it := range emojiSet {
			all = append(all, it.emoji)
		}
		return append(labels, "이모지"), append(groups, all)
	}
	var found []string
	for _, it := range emojiSet {
		if it.emoji == q || slices.ContainsFunc(it.names, func(n string) bool { return strings.Contains(strings.ToLower(n), q) }) {
			found = append(found, it.emoji)
		}
	}
	return []string{""}, [][]string{found}
}

// items is the flat list of drawn emoji (selection indexes it).
func (p *emojiPicker) items() []string {
	_, groups := p.sections()
	var out []string
	for _, g := range groups {
		out = append(out, g...)
	}
	return out
}

// cols is the grid width in cells for the current size.
func (p *emojiPicker) cols() int {
	cw := max(1, p.size.W-2*dialogPad(p.size.W))
	return max(1, cw/emojiCell)
}

// grid is the item indexes per grid row (sections start new rows).
func (p *emojiPicker) grid() [][]int {
	_, groups := p.sections()
	cols := p.cols()
	var rows [][]int
	i := 0
	for _, g := range groups {
		for start := 0; start < len(g); start += cols {
			var r []int
			for j := start; j < min(len(g), start+cols); j++ {
				r = append(r, i+j)
			}
			rows = append(rows, r)
		}
		i += len(g)
	}
	return rows
}

func (p *emojiPicker) pick(i int) tea.Cmd {
	items := p.items()
	if i < 0 || i >= len(items) {
		return nil
	}
	e := items[i]
	rememberEmoji(e)
	if p.onPick != nil {
		if cmd := p.onPick(e); cmd != nil {
			return tea.Batch(closeOverlay, cmd)
		}
	}
	return closeOverlay
}

// moveRow moves the selection to the nearest cell one grid row up (-1) or down (+1).
func (p *emojiPicker) moveRow(d int) {
	rows := p.grid()
	for r, row := range rows {
		c := slices.Index(row, p.sel)
		if c < 0 {
			continue
		}
		if t := r + d; t >= 0 && t < len(rows) {
			p.sel = rows[t][min(c, len(rows[t])-1)]
		}
		return
	}
}

func (p *emojiPicker) HandleKey(_ *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	n := len(p.items())
	switch s := msg.String(); s {
	case "esc", "ctrl+k":
		return false, nil
	case "enter":
		return true, p.pick(p.sel)
	case "left":
		p.sel = max(0, p.sel-1)
	case "right", "tab":
		p.sel = min(max(0, n-1), p.sel+1)
	case "shift+tab":
		p.sel = max(0, p.sel-1)
	case "up":
		p.moveRow(-1)
	case "down":
		p.moveRow(+1)
	default:
		before := p.query.Value()
		handled, cmd := inputUpdate(&p.query, msg)
		if p.query.Value() != before {
			p.sel = 0
		}
		return handled, cmd
	}
	return true, nil
}

func (p *emojiPicker) Update(_ *ui.Context, msg tea.Msg) tea.Cmd {
	if m, ok := msg.(tea.PasteMsg); ok {
		_, cmd := inputUpdate(&p.query, m)
		p.sel = 0
		return cmd
	}
	return nil
}

func (p *emojiPicker) HandleMouse(_ *ui.Context, msg tea.MouseMsg, local ui.Point) tea.Cmd {
	c, ok := msg.(tea.MouseClickMsg)
	if !ok || c.Button != tea.MouseLeft {
		return nil
	}
	for _, h := range p.hits[local.Y] {
		if local.X >= h.x0 && local.X < h.x1 {
			return p.pick(h.idx)
		}
	}
	return nil
}

// layout builds the rows, the grid hit cells and the query cursor column.
func (p *emojiPicker) layout(t *theme.Theme) (rows []string, hits map[int][]emojiHit, curX int) {
	w, h := p.size.W, p.size.H
	pd := dialogPad(w)
	cw := max(1, w-2*pd)
	items := p.items()
	if n := len(items); p.sel >= n {
		p.sel = max(0, n-1)
	}
	line, col := inputLine(t, p.query, "이름으로 찾기 (박수, clap)", max(1, cw-2))
	head := []string{t.S.Base.Render("› ") + line, t.S.Dim.Render(strings.Repeat("─", cw))}

	labels, groups := p.sections()
	grid := p.grid()
	var mid []string
	gridRow := map[int]int{} // mid index -> grid row
	focus := 0
	gr := 0
	for gi, g := range groups {
		if labels[gi] != "" {
			if gi > 0 {
				mid = append(mid, "")
			}
			mid = append(mid, t.S.Dim.Render(labels[gi]))
		}
		cols := p.cols()
		for start := 0; start < len(g); start += cols {
			var b strings.Builder
			for _, idx := range grid[gr] {
				st := t.S.Base
				if idx == p.sel {
					st = t.S.Selected
					focus = len(mid)
				}
				b.WriteString(st.Render(items[idx]) + t.S.Base.Render(" "))
			}
			gridRow[len(mid)] = gr
			mid = append(mid, b.String())
			gr++
		}
	}
	if len(items) == 0 {
		mid = append(mid, t.S.Dim.Render(textwidth.Truncate("찾는 이모지가 없어요", cw, "…")))
	}
	var foot string
	if len(items) > 0 {
		e := items[p.sel]
		foot = t.S.Base.Render(e+" ") + t.S.Dim.Render(textwidth.Truncate(strings.Join(emojiNames(e), " · "), max(1, cw-3), "…"))
	}
	rows, midTop := stack(head, mid, []string{foot}, focus, h)
	hits = map[int][]emojiHit{}
	for mi, r := range gridRow {
		y := midTop + mi
		if y < 0 || y >= len(rows) {
			continue
		}
		for c, idx := range grid[r] {
			x0 := pd + c*emojiCell
			hits[y] = append(hits[y], emojiHit{x0: x0, x1: x0 + 2, idx: idx})
		}
	}
	return rows, hits, pd + 2 + col
}

func (p *emojiPicker) View(ctx *ui.Context, _ bool) string {
	if p.size.W <= 0 || p.size.H <= 0 {
		return ""
	}
	t := themeOf(ctx)
	rows, hits, _ := p.layout(t)
	p.hits = hits
	return renderRows(t, rows, p.size)
}

// Cursor is the search field cursor (row 0 of the inner rect).
func (p *emojiPicker) Cursor() *tea.Cursor {
	if p.size.W <= 0 || p.size.H < 4 { // below 4 rows the search line may be dropped
		return nil
	}
	_, _, x := p.layout(theme.Get(theme.Default))
	if x >= p.size.W {
		return nil
	}
	return tea.NewCursor(x, 0)
}
