package feed

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/humanize"
	"github.com/patrache/masil/internal/markup"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/keymap"
	"github.com/patrache/masil/internal/ui/widgets"
)

// overlayGuestbookWrite is the ui/fun write dialog (args = owner user id). Its
// successful answer is broadcast as ResultMsg[*api.GuestbookEntry] with tag
// tagGuestbookWritten + owner id; the tab puts that entry on top.
const (
	overlayGuestbookWrite = "fun.guestbook_write"
	tagGuestbookWritten   = "fun.guestbook_write:"
)

// Guestbook result tags (ui.Do).
const (
	tagGuestbook       = "feed.guestbook:"        // + route id: first page
	tagGuestbookMore   = "feed.guestbook_more:"   // + route id: next page
	tagGuestbookDelete = "feed.guestbook_delete:" // + entry id
)

// Guestbook copy and layout.
const (
	textGuestbookEmpty     = "아직 방명록이 비어 있어요"
	hintGuestbookEmpty     = "w 로 첫 글을 남겨 보세요"
	chipGuestbookSecret    = "🔒 비밀글"
	textGuestbookHidden    = "비밀글이에요"
	textSomeone            = "누군가"
	textGuestbookDelAsk    = "방명록 글을 지울까요?"
	textGuestbookDelYes    = "지우기"
	textGuestbookDeleted   = "방명록 글을 지웠어요"
	textGuestbookReportOf  = "의 방명록 글"
	errContextGuestbook    = "방명록"
	errContextGuestbookDel = "방명록 삭제"
	guestbookBodyLines     = 6
)

// guestbookDeleteOutcome is a DELETE /v1/guestbook/{id} request.
type guestbookDeleteOutcome struct {
	claim
	entryID string
}

// guestbookTab is the profile's 방명록 tab: the server entries (raw, newest
// first) and the shown ones (deleted entries and blocked authors dropped).
type guestbookTab struct {
	raw         []api.GuestbookEntry
	items       []api.GuestbookEntry
	list        *widgets.List[api.GuestbookEntry]
	cursor      *string
	loaded      bool
	loading     bool
	loadingMore bool
	err         error
	deleting    map[string]bool

	y, h int // list rows of the last View (pane-local; y -1 = not drawn)
}

func newGuestbookTab() *guestbookTab {
	g := &guestbookTab{deleting: map[string]bool{}, y: -1}
	g.list = widgets.NewList(func(e api.GuestbookEntry) string { return e.ID }, renderGuestbookEntry)
	return g
}

// reset forgets the loaded entries (an unblock reloads the profile).
func (g *guestbookTab) reset() {
	g.raw, g.cursor, g.loaded, g.err = nil, nil, false, nil
}

// onGuestbook reports whether the 방명록 tab is showing.
func (p *profile) onGuestbook(ctx *ui.Context) bool {
	return p.tab == tabGuestbook && p.canView(ctx)
}

// loadGuestbook requests a page of the guestbook (cursor "" = the first page).
func (p *profile) loadGuestbook(ctx *ui.Context, cursor string) tea.Cmd {
	g := p.gb
	if p.prof == nil || g.loading || g.loadingMore {
		return nil
	}
	tag := tagGuestbook + p.id
	if cursor != "" {
		g.loadingMore, tag = true, tagGuestbookMore+p.id
	} else {
		g.loading = true
	}
	owner := api.UserID(p.prof.ID)
	return tea.Batch(p.spin.Start(ctx), ui.Do(ctx, tag, func(c context.Context, a *api.Client) (*api.Page[api.GuestbookEntry], error) {
		if a == nil {
			return nil, errNoClient
		}
		return a.ListGuestbook(c, owner, api.PageOpts{Cursor: cursor})
	}))
}

// ensureGuestbook loads the guestbook the first time its tab can be shown.
func (p *profile) ensureGuestbook(ctx *ui.Context) tea.Cmd {
	if !p.onGuestbook(ctx) || p.gb.loaded || p.gb.loading {
		return nil
	}
	return p.loadGuestbook(ctx, "")
}

// guestbookUpdate handles the guestbook's own results, the ui/fun write
// dialog's broadcast answer and deletes.
func (p *profile) guestbookUpdate(ctx *ui.Context, msg tea.Msg) tea.Cmd {
	g := p.gb
	switch m := msg.(type) {
	case guestbookDeletingMsg:
		g.deleting[m.entryID] = true
	case ui.ResultMsg[*api.Page[api.GuestbookEntry]]:
		more := false
		switch m.Tag {
		case tagGuestbook + p.id:
			if !g.loading {
				return nil
			}
			g.loading = false
		case tagGuestbookMore + p.id:
			if !g.loadingMore {
				return nil
			}
			g.loadingMore, more = false, true
		default:
			return nil
		}
		p.stopSpinner()
		if m.Err != nil || m.Value == nil {
			if !more {
				g.err, g.loaded = m.Err, true
			}
			return profileErr(m.Err, errContextGuestbook)
		}
		g.err, g.loaded = nil, true
		if more {
			seen := make(map[string]bool, len(g.raw))
			for _, e := range g.raw {
				seen[e.ID] = true
			}
			for _, e := range m.Value.Items {
				if !seen[e.ID] {
					g.raw = append(g.raw, e)
				}
			}
		} else {
			g.raw = append([]api.GuestbookEntry(nil), m.Value.Items...)
		}
		g.cursor = m.Value.NextCursor
		if len(m.Value.Items) == 0 {
			g.cursor = nil // an empty page never loops
		}
	case ui.ResultMsg[*api.GuestbookEntry]:
		e := m.Value
		if p.prof == nil || m.Tag != tagGuestbookWritten+p.prof.ID || m.Err != nil || e == nil || !g.loaded {
			return nil
		}
		out := []api.GuestbookEntry{*e}
		for _, x := range g.raw {
			if x.ID != e.ID {
				out = append(out, x)
			}
		}
		g.raw = out
		p.syncGuestbook(ctx, true)
		g.list.Select(e.ID)
		g.list.Restore(widgets.ScrollAnchor{ItemID: e.ID})
	case ui.ResultMsg[*guestbookDeleteOutcome]:
		o := m.Value
		if o == nil || !strings.HasPrefix(m.Tag, tagGuestbookDelete) {
			return nil
		}
		delete(g.deleting, o.entryID)
		first := o.take()
		if m.Err != nil {
			if first {
				return profileErr(m.Err, errContextGuestbookDel)
			}
			return nil
		}
		out := g.raw[:0:0]
		for _, x := range g.raw {
			if x.ID != o.entryID {
				out = append(out, x)
			}
		}
		g.raw = out
		if first {
			return ui.Toast(textGuestbookDeleted, ui.ToastSuccess)
		}
	}
	return nil
}

// buildGuestbook is the raw entries minus deleted ones and blocked authors
// (Task 5 block filter).
func (p *profile) buildGuestbook(ctx *ui.Context) []api.GuestbookEntry {
	out := make([]api.GuestbookEntry, 0, len(p.gb.raw))
	for _, e := range p.gb.raw {
		if e.Deleted {
			continue
		}
		if e.Author != nil && ctx != nil && ctx.Store != nil && ctx.Store.IsBlocked(e.Author.ID) {
			continue
		}
		out = append(out, e)
	}
	return out
}

// syncGuestbook re-reads the entries into the List (unless force, only when
// the IDs changed); a list that was empty starts at the top.
func (p *profile) syncGuestbook(ctx *ui.Context, force bool) {
	g := p.gb
	items := p.buildGuestbook(ctx)
	if !force && sameEntryIDs(items, g.items) {
		return
	}
	wasEmpty := len(g.items) == 0
	g.items = items
	g.list.SetItems(items)
	if wasEmpty && len(items) > 0 {
		g.list.Restore(widgets.ScrollAnchor{ItemID: items[0].ID})
	}
}

func sameEntryIDs(a, b []api.GuestbookEntry) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].ID != b[i].ID {
			return false
		}
	}
	return true
}

// canDeleteEntry: the guestbook's owner or the entry's author.
func (p *profile) canDeleteEntry(ctx *ui.Context, e api.GuestbookEntry) bool {
	if p.isMe(ctx) {
		return true
	}
	return e.Author != nil && ctx != nil && ctx.Store != nil && ctx.Store.IsMe(e.Author.ID)
}

// deleteEntry is the Confirm before DELETE /v1/guestbook/{id}.
func (p *profile) deleteEntry(ctx *ui.Context, e api.GuestbookEntry) tea.Cmd {
	if p.gb.deleting[e.ID] {
		return nil
	}
	id := e.ID
	o := &guestbookDeleteOutcome{entryID: id}
	del := ui.Do(ctx, tagGuestbookDelete+id, func(c context.Context, a *api.Client) (*guestbookDeleteOutcome, error) {
		if a == nil {
			return o, errNoClient
		}
		return o, a.DeleteGuestbookEntry(c, id)
	})
	mark := func() tea.Msg { return guestbookDeletingMsg{id} }
	return ui.OpenOverlay(widgets.Confirm(textGuestbookDelAsk, "", textGuestbookDelYes, textCancel, tea.Batch(mark, del)))
}

// guestbookDeletingMsg marks an entry as being deleted (sent with the request).
type guestbookDeletingMsg struct{ entryID string }

// entryAuthorName is the author's display name ("누군가" when hidden).
func entryAuthorName(e api.GuestbookEntry) string {
	if e.Hidden || e.Author == nil {
		return textSomeone
	}
	return displayName(*e.Author)
}

// guestbookActionKey handles w (write), d (delete) and m (report) on the
// 방명록 tab; m falls through to the profile menu when no entry can be reported.
func (p *profile) guestbookActionKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	km := ctx.Keys
	sel, ok := p.gb.list.Selected()
	switch {
	case msg.String() == "w":
		return true, ui.OpenRegisteredOverlay(overlayGuestbookWrite, p.prof.ID)
	case km.Matches(msg, keymap.MsgDelete):
		if !ok || !p.canDeleteEntry(ctx, sel) {
			return true, nil
		}
		return true, p.deleteEntry(ctx, sel)
	case km.Matches(msg, keymap.MsgActions):
		if !ok || sel.Hidden {
			return false, nil
		}
		return true, ui.OpenOverlay(widgets.ReportDialog("guestbook_entry", sel.ID, entryAuthorName(sel)+textGuestbookReportOf))
	}
	return false, nil
}

// guestbookListKey moves the selection (loading the next page near the end).
func (p *profile) guestbookListKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	handled, cmd := p.gb.list.HandleKey(ctx, msg)
	if !handled {
		return false, nil
	}
	return true, tea.Batch(cmd, p.guestbookMore(ctx))
}

func (p *profile) guestbookMore(ctx *ui.Context) tea.Cmd {
	g := p.gb
	if g.cursor == nil || len(g.items) == 0 {
		return nil
	}
	sel, ok := g.list.Selected()
	if !ok {
		return nil
	}
	for i, e := range g.items {
		if e.ID == sel.ID && i >= len(g.items)-moreThreshold {
			return p.loadGuestbook(ctx, *g.cursor)
		}
	}
	return nil
}

// guestbookMouse scrolls / selects within the drawn entry list (ok = in it).
func (p *profile) guestbookMouse(ctx *ui.Context, msg tea.MouseMsg, local ui.Point) (tea.Cmd, bool) {
	g := p.gb
	if g.y < 0 || local.Y < g.y || local.Y >= g.y+g.h {
		return nil, false
	}
	cmd := g.list.HandleMouse(ctx, msg, ui.Point{X: local.X, Y: local.Y - g.y})
	return tea.Batch(cmd, p.guestbookMore(ctx)), true
}

// guestbookView is the tab body in rest rows of width w.
func (p *profile) guestbookView(ctx *ui.Context, w, rest, top int) []string {
	t := themeOf(ctx)
	g := p.gb
	g.y, g.h = -1, 0
	empty := func(icon, title, hint string) []string {
		return strings.Split(widgets.Empty(t, ui.Size{W: w, H: rest}, icon, title, hint), "\n")
	}
	switch {
	case len(g.items) == 0 && (g.loading || !g.loaded):
		return []string{p.loadingLine(ctx, w)}
	case len(g.items) == 0 && g.err != nil:
		return empty("⚠", textwidth.Sanitize(api.Describe(g.err)), "")
	case len(g.items) == 0:
		return empty("📖", textGuestbookEmpty, hintGuestbookEmpty)
	}
	footer := 0
	if g.loadingMore {
		footer = 1
	}
	var out []string
	g.y, g.h = top, max(rest-footer, 0)
	if g.h > 0 {
		out = strings.Split(g.list.View(ctx, ui.Size{W: w, H: g.h}), "\n")
	}
	if footer > 0 {
		out = append(out, p.loadingLine(ctx, w))
	}
	return out
}

// renderGuestbookEntry is "@handle 이름 · 3분  🔒 비밀글", the body (or
// 비밀글이에요 for a hidden entry) and a rule.
func renderGuestbookEntry(ctx *ui.Context, e api.GuestbookEntry, selected bool, width int) []string {
	t := themeOf(ctx)
	if width <= 0 {
		return nil
	}
	cw := max(width-gutterW, 0)
	g := t.S.Base.Render("  ")
	if selected {
		g = t.S.Base.Foreground(t.P.Acc).Render("▎") + t.S.Base.Render(" ")
	}
	if width < gutterW {
		g = ""
	}
	line := func(s string) string { return fit(t, g+fit(t, s, cw), width) }
	out := []string{line(entryHeader(ctx, t, e, cw))}
	switch {
	case cw <= 0:
	case e.Hidden:
		out = append(out, line(t.S.Dim.Render(textwidth.Truncate(textGuestbookHidden, cw, "…"))))
	case e.Body != "":
		myHandle := ""
		if me := ctx.Me(); me != nil {
			myHandle = me.Handle
		}
		doc := markup.Parse(e.Body, markup.ParseOptions{Hashtags: false})
		for _, l := range markup.Render(doc, t, markup.RenderOptions{Width: cw, MyHandle: myHandle, MaxLines: guestbookBodyLines, Hyperlinks: true}) {
			out = append(out, line(l))
		}
	}
	return append(out, fit(t, t.S.Base.Foreground(t.P.Line).Render(strings.Repeat("┄", width)), width))
}

// entryHeader is the author, the age and the secret chip in w cells: the chip
// goes first, then the age, then the name is cut before the handle.
func entryHeader(ctx *ui.Context, t *theme.Theme, e api.GuestbookEntry, w int) string {
	if w <= 0 {
		return ""
	}
	age := ""
	if ctx != nil && ctx.Now != nil {
		age = " · " + humanize.Relative(ctx.Now(), e.CreatedAt)
	}
	chip := ""
	if e.IsSecret {
		chip = "  " + chipGuestbookSecret
	}
	var handle, name string
	if e.Hidden || e.Author == nil {
		name = textSomeone
	} else {
		handle, name = handleOf(*e.Author), displayName(*e.Author)
	}
	room := w - textwidth.Width(age) - textwidth.Width(chip)
	if room < 4 && chip != "" {
		chip, room = "", w-textwidth.Width(age)
	}
	if room < 4 {
		age, room = "", w
	}
	var who string
	switch {
	case handle == "":
		who = t.S.Dim.Render(textwidth.Truncate(name, room, "…"))
	case textwidth.Width(handle)+3 <= room:
		n := textwidth.Truncate(name, room-textwidth.Width(handle)-1, "…")
		named := *e.Author
		named.DisplayName = n
		self := ctx != nil && ctx.Store != nil && ctx.Store.IsMe(e.Author.ID)
		who = t.S.Bold.Render(handle) + t.S.Base.Render(" ") + widgets.UserName(t, named, self)
	default:
		who = t.S.Bold.Render(textwidth.Truncate(handle, room, "…"))
	}
	s := who + t.S.Timestamp.Render(age)
	if chip != "" {
		s += t.S.Base.Render("  ") + t.S.Dim.Render(chipGuestbookSecret)
	}
	return fit(t, s, w)
}
