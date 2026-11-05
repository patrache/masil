package chat

import (
	"cmp"
	"context"
	"slices"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/palette"
	"github.com/patrache/masil/internal/ui/widgets"
)

// User picker texts and limits.
const (
	pickerPlaceholder = "이름이나 @아이디로 찾기"
	pickerNameHolder  = "대화 이름(선택)"
	pickerNoChips     = "아직 고른 사람이 없어요"
	pickerTypeHint    = "이름이나 @아이디를 입력해 보세요"
	pickerMoreHint    = "두 글자 이상 입력하면 찾아봐요"
	pickerSearching   = "찾는 중…"
	pickerNoMatch     = "찾는 사람이 없어요"
	pickerSending     = " 보내는 중…"
	pickerMaxSuffix   = "명까지 고를 수 있어요"
	pickerSearchFail  = "사람을 찾지 못했어요"
	pickerMaxW        = 56
	pickerListRows    = 6 // candidate rows of the full-size box
	pickerDebounce    = 300 * time.Millisecond
	pickerSearchMin   = 2 // runes before a remote search
	pickerSearchLimit = 10
	tagUserSearch     = "chat.user_search:"
)

// pickerSpec describes a user picker. Candidates are offered without a query
// (filtered by the query); with Search the server is asked too (2+ runes, 300ms
// debounce). Exclude hides users (I am always hidden). Max caps the picks (0 =
// no cap). NameField adds the "대화 이름(선택)" field (tab), shown once at least
// NameFrom users are picked (0 = always). Order sorts the offered Candidates
// (nil = by display name). Query is the first query. OnPick/OnDone follow the
// form's tag and morph rules (formSpec); OnPick gets the name only while the name
// field is shown.
type pickerSpec struct {
	Title, Submit string
	Max           int
	Search        bool
	Candidates    func(ctx *ui.Context) []api.UserSummary
	Exclude       func(ctx *ui.Context, id string) bool
	Order         func(ctx *ui.Context, a, b api.UserSummary) int
	NameField     bool
	NameFrom      int
	Query         string
	OnPick        func(ctx *ui.Context, users []api.UserSummary, name string) (tag string, cmd tea.Cmd)
	OnDone        func(ctx *ui.Context, result tea.Msg) (tea.Cmd, ui.Overlay)
}

// pickerSearchMsg is the debounce tick of one picker's query (gen = the query generation).
type pickerSearchMsg struct {
	owner *userPicker
	gen   int
}

// pickerFound is a remote search result of one picker.
type pickerFound struct {
	owner *userPicker
	gen   int
	q     string
	users []api.UserSummary
}

// userPicker is newUserPicker's overlay.
type userPicker struct {
	morph
	spec   pickerSpec
	query  textinput.Model
	name   textinput.Model
	onName bool // the name field has the focus

	chips []api.UserSummary
	cands []api.UserSummary // current candidates (refresh)
	cur   int               // selected candidate, -1 = none (the input)

	gen       int               // query generation (debounce / stale results)
	remote    []api.UserSummary // last search result
	remoteQ   string            // its query
	searching bool              // a search for the current query is pending
	searchErr bool

	busy    bool
	tag     string
	errMsg  string
	spinner *widgets.Spinner

	// last layout (mouse hit testing): candidate i is drawn at row listTop + i - listOff
	listTop, listOff, listRows int
}

var (
	_ ui.Overlay       = (*userPicker)(nil)
	_ ui.KeyHandler    = (*userPicker)(nil)
	_ ui.MouseHandler  = (*userPicker)(nil)
	_ ui.Cursorer      = (*userPicker)(nil)
	_ ui.Titled        = (*userPicker)(nil)
	_ ui.Helper        = (*userPicker)(nil)
	_ ui.InputCapturer = (*userPicker)(nil)
)

// newUserPicker is a modal user picker: the query field, the picked chips, the
// optional name field and the candidates. ↑/↓ move between the query and the
// candidates, enter picks the selected candidate (or submits with an empty
// query and at least one pick), backspace on an empty query drops the last
// pick, tab toggles the name field, esc closes.
func newUserPicker(spec pickerSpec) ui.Overlay {
	p := &userPicker{spec: spec, query: newTextInput(), name: newTextInput(), cur: -1, spinner: widgets.NewSpinner()}
	p.query.SetValue(oneLine(spec.Query))
	p.query.CursorEnd()
	p.name.CharLimit = 50 // group DM name 0..50 (api.md 0.3)
	p.name.Blur()
	return p
}

func (p *userPicker) Init(ctx *ui.Context) tea.Cmd {
	p.refresh(ctx)
	if p.query.Value() != "" {
		return p.queryChanged(ctx)
	}
	return nil
}

func (p *userPicker) Resize(ctx *ui.Context, size ui.Size) {
	p.size = size
	if p.next != nil {
		p.next.Resize(ctx, size)
	}
}

func (p *userPicker) Bounds(body layout.Rect) layout.Rect {
	if p.next != nil {
		return p.next.Bounds(body)
	}
	return centered(body, min(body.W, pickerMaxW), p.fullRows()+2)
}

// fullRows is the inner height of the full-size box: the fixed rows, blank, the
// candidate rows, blank, status (the title is on the border; it is drawn inside
// only when the box got spare rows, i.e. degraded to the whole body).
func (p *userPicker) fullRows() int { return len(p.fixedRows()) + 1 + pickerListRows + 2 }

func (p *userPicker) Modal() bool {
	if p.next != nil {
		return p.next.Modal()
	}
	return true
}

func (p *userPicker) Title(ctx *ui.Context) (string, ui.Badge) {
	if p.next != nil {
		return p.morph.title(ctx)
	}
	return p.spec.Title, ui.Badge{}
}

func (p *userPicker) submitText() string {
	if p.spec.Submit == "" {
		return "확인"
	}
	return p.spec.Submit
}

func (p *userPicker) Help(ctx *ui.Context) []key.Binding {
	if p.next != nil {
		return p.morph.help(ctx)
	}
	hs := []key.Binding{
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "고르기·"+p.submitText())),
		key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑/↓", "이동")),
	}
	if p.nameShown() {
		hs = append(hs, key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "이름 칸")))
	}
	return append(hs, key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "닫기")))
}

func (p *userPicker) CapturesInput() bool {
	if p.next != nil {
		return p.captures()
	}
	return true
}

// ---- candidates ----

// matchUser reports whether u matches q (lower-cased, "@" stripped): a handle
// or display-name substring, or a chosung prefix of the display name.
func matchUser(u api.UserSummary, q string) bool {
	if q == "" {
		return true
	}
	if strings.Contains(strings.ToLower(u.Handle), q) || strings.Contains(strings.ToLower(u.DisplayName), q) {
		return true
	}
	return strings.HasPrefix(strings.ToLower(palette.Chosung(u.DisplayName)), q)
}

// normQuery is the match form of the query.
func (p *userPicker) normQuery() string {
	return strings.ToLower(strings.TrimPrefix(strings.TrimSpace(p.query.Value()), "@"))
}

// hidden: me, Exclude, already picked, no id.
func (p *userPicker) hidden(ctx *ui.Context, u api.UserSummary) bool {
	if u.ID == "" || isMe(ctx, u.ID) {
		return true
	}
	if p.spec.Exclude != nil && p.spec.Exclude(ctx, u.ID) {
		return true
	}
	return slices.ContainsFunc(p.chips, func(c api.UserSummary) bool { return c.ID == u.ID })
}

// refresh rebuilds the candidates: Candidates(ctx) then the last search result,
// matching the query, deduplicated, without hidden users; the local ones by
// display name. The selection stays on its user when it is still there.
func (p *userPicker) refresh(ctx *ui.Context) {
	prev := ""
	if p.cur >= 0 && p.cur < len(p.cands) {
		prev = p.cands[p.cur].ID
	}
	q := p.normQuery()
	seen := map[string]bool{}
	var local, far []api.UserSummary
	add := func(dst *[]api.UserSummary, u api.UserSummary) {
		if seen[u.ID] || p.hidden(ctx, u) || !matchUser(u, q) {
			return
		}
		seen[u.ID] = true
		if ctx != nil && ctx.Store != nil {
			if cu := ctx.Store.User(u.ID); cu != nil {
				u = *cu
			}
		}
		*dst = append(*dst, u)
	}
	if p.spec.Candidates != nil {
		for _, u := range p.spec.Candidates(ctx) {
			add(&local, u)
		}
	}
	slices.SortFunc(local, func(a, b api.UserSummary) int {
		byName := cmp.Or(strings.Compare(displayName(a), displayName(b)), strings.Compare(a.ID, b.ID))
		if p.spec.Order != nil {
			return cmp.Or(p.spec.Order(ctx, a, b), byName)
		}
		return byName
	})
	if p.spec.Search && q != "" {
		for _, u := range p.remote {
			add(&far, u)
		}
	}
	p.cands = append(local, far...)
	p.cur = -1
	if prev != "" {
		p.cur = slices.IndexFunc(p.cands, func(u api.UserSummary) bool { return u.ID == prev })
	}
	if p.cur < 0 && q != "" && len(p.cands) > 0 {
		p.cur = 0 // typing selects the best match
	}
}

// queryChanged restarts the debounce for the new query and reselects.
func (p *userPicker) queryChanged(ctx *ui.Context) tea.Cmd {
	p.gen++
	p.cur = -1
	p.searchErr = false
	p.refresh(ctx)
	p.searching = false
	if !p.spec.Search || len([]rune(p.normQuery())) < pickerSearchMin {
		return nil
	}
	p.searching = true
	return ui.After(ctx, pickerDebounce, pickerSearchMsg{owner: p, gen: p.gen})
}

// search asks the server for the current query.
func (p *userPicker) search(ctx *ui.Context) tea.Cmd {
	q := strings.TrimSpace(p.query.Value())
	gen := p.gen
	return ui.Do(ctx, tagUserSearch+q, func(c context.Context, cl *api.Client) (pickerFound, error) {
		us, err := cl.SearchUsers(c, q, pickerSearchLimit)
		return pickerFound{owner: p, gen: gen, q: q, users: us}, err
	})
}

// pick adds the selected candidate to the chips (Max permitting) and clears the query.
func (p *userPicker) pick(ctx *ui.Context, i int) tea.Cmd {
	if i < 0 || i >= len(p.cands) {
		return nil
	}
	if p.spec.Max > 0 && len(p.chips) >= p.spec.Max {
		p.errMsg = "최대 " + strconv.Itoa(p.spec.Max) + pickerMaxSuffix
		return nil
	}
	p.errMsg = ""
	p.chips = append(p.chips, p.cands[i])
	p.query.Reset()
	cmd := p.queryChanged(ctx)
	p.cur = -1
	return cmd
}

// submit calls OnPick with the chips and the name.
func (p *userPicker) submit(ctx *ui.Context) tea.Cmd {
	if len(p.chips) == 0 {
		return nil
	}
	p.errMsg = ""
	if p.spec.OnPick == nil {
		return p.done(ctx, nil)
	}
	name := ""
	if p.nameShown() {
		name = strings.TrimSpace(p.name.Value())
	}
	tag, cmd := p.spec.OnPick(ctx, slices.Clone(p.chips), name)
	if tag == "" {
		return tea.Batch(cmd, p.done(ctx, nil))
	}
	p.busy, p.tag = true, tag
	return tea.Batch(cmd, p.spinner.Start(ctx))
}

// done finishes through OnDone (morph or close).
func (p *userPicker) done(ctx *ui.Context, result tea.Msg) tea.Cmd {
	if p.spec.OnDone == nil {
		return closeOverlay
	}
	cmd, o := p.spec.OnDone(ctx, result)
	return p.finish(ctx, cmd, o)
}

// ---- input ----

// nameShown reports whether the name field is shown (NameField, NameFrom picks).
func (p *userPicker) nameShown() bool {
	return p.spec.NameField && len(p.chips) >= p.spec.NameFrom
}

func (p *userPicker) setNameFocus(on bool) {
	p.onName = on
	if on {
		p.query.Blur()
		p.name.Focus()
	} else {
		p.name.Blur()
		p.query.Focus()
	}
}

func (p *userPicker) HandleKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	if p.next != nil {
		return p.morph.key(ctx, msg)
	}
	s := msg.String()
	if s == "esc" || s == "ctrl+k" {
		return false, nil // the app closes the picker (and opens the palette for ctrl+k)
	}
	if p.busy {
		return true, nil
	}
	switch s {
	case "tab", "shift+tab":
		if p.nameShown() {
			p.setNameFocus(!p.onName)
		}
		return true, nil
	case "enter":
		if p.onName {
			return true, p.submit(ctx)
		}
		if p.cur >= 0 {
			return true, p.pick(ctx, p.cur)
		}
		if strings.TrimSpace(p.query.Value()) == "" {
			return true, p.submit(ctx)
		}
		return true, nil
	case "down", "ctrl+n":
		if !p.onName && len(p.cands) > 0 {
			p.cur = min(p.cur+1, len(p.cands)-1)
		}
		return true, nil
	case "up", "ctrl+p":
		if !p.onName {
			p.cur = max(p.cur-1, -1)
		}
		return true, nil
	case "backspace":
		if !p.onName && p.query.Value() == "" {
			if len(p.chips) > 0 {
				p.chips = p.chips[:len(p.chips)-1]
				p.errMsg = ""
				p.refresh(ctx)
			}
			return true, nil
		}
	}
	return p.edit(ctx, msg)
}

// edit feeds a key or paste to the focused field.
func (p *userPicker) edit(ctx *ui.Context, msg tea.Msg) (bool, tea.Cmd) {
	if p.onName {
		handled, cmd := inputUpdate(&p.name, msg)
		return handled, cmd
	}
	before := p.query.Value()
	handled, cmd := inputUpdate(&p.query, msg)
	if handled {
		p.errMsg = ""
	}
	if p.query.Value() != before {
		cmd = tea.Batch(cmd, p.queryChanged(ctx))
	}
	return handled, cmd
}

func (p *userPicker) Update(ctx *ui.Context, msg tea.Msg) tea.Cmd {
	if p.next != nil {
		return p.next.Update(ctx, msg)
	}
	switch m := msg.(type) {
	case tea.PasteMsg:
		if p.busy {
			return nil
		}
		_, cmd := p.edit(ctx, m)
		return cmd
	case pickerSearchMsg:
		if m.owner != p || m.gen != p.gen {
			return nil
		}
		return p.search(ctx)
	case ui.ResultMsg[pickerFound]:
		f := m.Value
		if f.owner != p || f.gen != p.gen {
			return nil
		}
		p.searching = false
		if m.Err != nil {
			p.searchErr = true
			return nil
		}
		p.remote, p.remoteQ = f.users, f.q
		p.refresh(ctx)
		return nil
	}
	var landed tea.Cmd
	if m, ok := msg.(ui.ResultMsg[msgAction]); ok {
		landed = landAction(ctx, m) // a doAction OnPick (e.g. openDMCmd) lands here when no conversation did
	}
	if tag, err, ok := resultOf(msg); ok {
		if !p.busy || tag != p.tag {
			return landed
		}
		p.busy = false
		p.spinner.Stop()
		if err != nil {
			p.errMsg = api.Describe(err)
			return landed
		}
		return tea.Batch(landed, p.done(ctx, msg))
	}
	if ctx != nil && ctx.Store != nil {
		if _, ok := msg.(ui.EventMsg); ok {
			p.refresh(ctx) // friends / blocks may have changed
		}
	}
	return p.spinner.Update(ctx, msg)
}

func (p *userPicker) HandleMouse(ctx *ui.Context, msg tea.MouseMsg, local ui.Point) tea.Cmd {
	if p.next != nil {
		return p.morph.mouse(ctx, msg, local)
	}
	if p.busy {
		return nil
	}
	switch ev := msg.(type) {
	case tea.MouseClickMsg:
		if ev.Button != tea.MouseLeft {
			return nil
		}
		if r := local.Y - p.listTop; r >= 0 && r < p.listRows {
			if i := p.listOff + r; i < len(p.cands) {
				p.setNameFocus(false)
				return p.pick(ctx, i)
			}
		}
	case tea.MouseWheelMsg:
		switch ev.Button {
		case tea.MouseWheelUp:
			p.cur = max(p.cur-1, -1)
		case tea.MouseWheelDown:
			if len(p.cands) > 0 {
				p.cur = min(p.cur+1, len(p.cands)-1)
			}
		}
	}
	return nil
}

// ---- drawing ----

// fieldRow is one focusable input row.
const (
	rowQuery = iota
	rowChips
	rowName
)

// fixedRows are the field rows: query, chips, (name).
func (p *userPicker) fixedRows() []int {
	if p.nameShown() {
		return []int{rowQuery, rowChips, rowName}
	}
	return []int{rowQuery, rowChips}
}

// chipsLine draws the picks as " 이름 " chips (S.Selected) in exactly cw cells;
// the newest picks win when they do not all fit ("…" first).
func chipsLine(t *theme.Theme, chips []api.UserSummary, cw int) string {
	if len(chips) == 0 {
		return cell(t.S.Dim.Render, pickerNoChips, cw)
	}
	labels := make([]string, len(chips))
	for i, u := range chips {
		labels[i] = " " + strings.TrimSpace(textwidth.Sanitize(displayName(u))) + " "
	}
	first, used := len(labels), 0
	for i := len(labels) - 1; i >= 0; i-- {
		w := textwidth.Width(labels[i]) + 1
		if used+w > cw && first < len(labels) {
			break
		}
		used += w
		first = i
	}
	var b strings.Builder
	x := 0
	if first > 0 && cw >= 2 {
		b.WriteString(t.S.Dim.Render("…"))
		b.WriteString(blank(t, 1))
		x = 2
	}
	for i := first; i < len(labels); i++ {
		l := textwidth.Truncate(labels[i], max(0, cw-x), "…")
		if l == "" {
			break
		}
		b.WriteString(t.S.Selected.Render(l))
		x += textwidth.Width(l)
		if x < cw {
			b.WriteString(blank(t, 1))
			x++
		}
	}
	return b.String() + blank(t, cw-x)
}

// candRow is "› ● 이름 @handle" in exactly cw cells.
func (p *userPicker) candRow(ctx *ui.Context, t *theme.Theme, i, cw int) string {
	u := p.cands[i]
	st, mark := t.S.Base, "  "
	if i == p.cur && !p.onName {
		st, mark = t.S.Selected, "› "
	}
	if cw <= 2 {
		return cell(st.Render, mark, cw)
	}
	presence := api.PresenceOffline
	if ctx != nil && ctx.Store != nil {
		presence = userPresence(ctx, u.ID)
	}
	dot := widgets.PresenceDot(t, presence) + st.Render(" ")
	room := cw - 4
	if room <= 0 {
		return st.Render(mark) + cell(st.Render, "", cw-2)
	}
	name := strings.TrimSpace(textwidth.Sanitize(displayName(u)))
	handle := ""
	if h := textwidth.Sanitize(u.Handle); h != "" {
		handle = " @" + h
	}
	if textwidth.Width(name)+textwidth.Width(handle) > room {
		if textwidth.Width(name) >= room-1 || handle == "" {
			handle = ""
		} else {
			handle = textwidth.Truncate(handle, room-textwidth.Width(name), "…")
		}
	}
	name = textwidth.Truncate(name, room-textwidth.Width(handle), "…")
	gap := room - textwidth.Width(name) - textwidth.Width(handle)
	return st.Render(mark) + dot + st.Render(name) + t.S.Dim.Render(handle) + st.Render(strings.Repeat(" ", max(gap, 0)))
}

// emptyText is the line shown when there is no candidate.
func (p *userPicker) emptyText() string {
	q := p.normQuery()
	switch {
	case p.searching:
		return pickerSearching
	case p.searchErr:
		return pickerSearchFail
	case q == "" && p.spec.Search:
		return pickerTypeHint
	case p.spec.Search && len([]rune(q)) < pickerSearchMin:
		return pickerMoreHint
	}
	return pickerNoMatch
}

// layout builds the rows and the focused field's cursor (relative to the inner rect).
func (p *userPicker) layout(ctx *ui.Context, t *theme.Theme) (rows []string, curX, curY int) {
	w, h := p.size.W, p.size.H
	pad := overlayPad(w)
	cw := max(1, w-2*pad)
	vw := max(1, cw-2)

	var fixed []string
	focusRow := 0
	for _, r := range p.fixedRows() {
		switch r {
		case rowQuery:
			marker := t.S.Title.Render("› ")
			if p.onName || p.cur >= 0 { // the "›" marks one row: the field or the candidate
				marker = blank(t, 2)
			}
			line, col := inputLine(t, p.query, pickerPlaceholder, vw)
			if !p.onName {
				focusRow, curX = len(fixed), pad+2+col
			}
			fixed = append(fixed, marker+line)
		case rowChips:
			fixed = append(fixed, blank(t, 2)+chipsLine(t, p.chips, vw))
		case rowName:
			marker := blank(t, 2)
			if p.onName {
				marker = t.S.Title.Render("› ")
			}
			line, col := inputLine(t, p.name, pickerNameHolder, vw)
			if p.onName {
				focusRow, curX = len(fixed), pad+2+col
			}
			fixed = append(fixed, marker+line)
		}
	}

	var status string
	switch {
	case p.errMsg != "":
		status = cell(t.S.Error.Render, "✕ "+textwidth.Sanitize(p.errMsg), cw)
	case p.busy:
		status = p.spinner.View(t) + cell(t.S.Dim.Render, pickerSending, cw-1)
	case len(p.chips) > 0 && strings.TrimSpace(p.query.Value()) == "" && p.cur < 0:
		status = cell(t.S.Dim.Render, "enter "+textwidth.Sanitize(p.submitText())+" · esc 닫기", cw)
	default:
		status = cell(t.S.Dim.Render, "enter 고르기 · ↑↓ 이동 · esc 닫기", cw)
	}

	// Budget: the focused field row always, then the status, the other fixed
	// rows, at least one list row, the spacers, and the title when rows remain.
	if h <= 0 {
		return nil, curX, 0
	}
	if h < len(fixed)+2 { // tiny: the focused field first, then status, then the rest
		rows = append(rows, fixed[focusRow])
		if h > 1 {
			rows = append(rows, status)
		}
		p.listRows = 0
		return rows[:min(len(rows), h)], curX, 0
	}
	rest := h - len(fixed) - 1 // list area incl. spacers and title
	spacerTop, spacerFoot, title := false, false, false
	listN := rest
	if listN >= 3 {
		spacerFoot, listN = true, listN-1
	}
	if listN >= 3 {
		spacerTop, listN = true, listN-1
	}
	if listN >= 2+pickerListRows {
		title, listN = true, listN-2
	}

	if title {
		rows = append(rows, cell(t.S.Title.Render, textwidth.Sanitize(p.spec.Title), cw), "")
	}
	fixedTop := len(rows)
	rows = append(rows, fixed...)
	if spacerTop {
		rows = append(rows, "")
	}
	p.listTop, p.listRows = len(rows), listN
	off := 0
	if p.cur >= listN {
		off = p.cur - listN + 1
	}
	off = max(0, min(off, len(p.cands)-listN))
	p.listOff = off
	for i := range listN {
		switch {
		case len(p.cands) == 0 && i == 0:
			rows = append(rows, blank(t, 2)+cell(t.S.Dim.Render, p.emptyText(), max(0, cw-2)))
		case off+i < len(p.cands):
			rows = append(rows, p.candRow(ctx, t, off+i, cw))
		default:
			rows = append(rows, "")
		}
	}
	if spacerFoot {
		rows = append(rows, "")
	}
	rows = append(rows, status)
	return rows, curX, fixedTop + focusRow
}

func (p *userPicker) View(ctx *ui.Context, focused bool) string {
	if p.next != nil {
		return p.next.View(ctx, focused)
	}
	if p.size.W <= 0 || p.size.H <= 0 {
		return ""
	}
	t := themeOf(ctx)
	rows, _, _ := p.layout(ctx, t)
	return renderOverlayRows(t, rows, p.size)
}

// Cursor is the focused field's cursor (relative to the inner rect), nil while sending.
func (p *userPicker) Cursor() *tea.Cursor {
	if p.next != nil {
		return p.morph.cursor()
	}
	if p.busy || p.size.W <= 0 || p.size.H <= 0 {
		return nil
	}
	_, x, y := p.layout(nil, theme.Get(theme.Default))
	if y < 0 || y >= p.size.H || x >= p.size.W {
		return nil
	}
	return tea.NewCursor(x, y)
}
