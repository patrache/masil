package chat

import (
	"context"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/errtext"
	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/keymap"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/widgets"
)

func init() {
	ui.RegisterOverlay("chat.reaction_users", newReactionUsers)
}

// Reaction users overlay texts and sizes.
const (
	reactionUsersMaxW = 40
	reactionUsersMaxH = 14
	reactionUsersMinH = 6 // the app degrades shorter overlays to the whole body
	reactionUsersPage = 50
)

// reactionUsersResult is one ListReactionUsers page of an overlay instance.
type reactionUsersResult struct {
	owner *reactionUsers
	page  *api.Page[api.UserSummary]
}

// reactionUsers is chat.reaction_users: who reacted with one emoji to a message
// ("<emoji> 누른 사람"), avatar + name + @handle, the next cursor page fetched
// when the selection or the wheel reaches the end.
type reactionUsers struct {
	messageID, emoji string

	list    *widgets.List[api.UserSummary]
	items   []api.UserSummary
	next    string // cursor of the next page ("" = none)
	loaded  bool   // the first page landed
	loading bool   // a page is in flight
	failed  string // inline error ("" = none)

	size   ui.Size
	bounds layout.Rect // last Bounds (bordered iff size is its inner size)
	shown  int         // rows the content asked for at the last Bounds
}

var (
	_ ui.Overlay      = (*reactionUsers)(nil)
	_ ui.KeyHandler   = (*reactionUsers)(nil)
	_ ui.MouseHandler = (*reactionUsers)(nil)
	_ ui.Titled       = (*reactionUsers)(nil)
	_ ui.Helper       = (*reactionUsers)(nil)
)

// newReactionUsers is the chat.reaction_users factory (args "<messageID> <emoji>";
// nil when malformed).
func newReactionUsers(_ *ui.Context, args string) ui.Overlay {
	id, emoji, ok := strings.Cut(strings.TrimSpace(args), " ")
	emoji = strings.TrimSpace(emoji)
	if !ok || id == "" || emoji == "" {
		return nil
	}
	return newReactionUsersFor(id, emoji)
}

// newReactionUsersFor is the overlay of messageID's emoji reaction.
func newReactionUsersFor(messageID, emoji string) *reactionUsers {
	o := &reactionUsers{messageID: messageID, emoji: emoji}
	o.list = widgets.NewList(func(u api.UserSummary) string { return u.ID }, o.render)
	return o
}

func (o *reactionUsers) Init(ctx *ui.Context) tea.Cmd { return o.fetch(ctx) }

// fetch requests the next page (the first when nothing landed yet), one at a time.
func (o *reactionUsers) fetch(ctx *ui.Context) tea.Cmd {
	if o.loading || (o.loaded && o.next == "") {
		return nil
	}
	o.loading = true
	id, emoji, cursor := o.messageID, o.emoji, o.next
	return ui.Do(ctx, "chat.reaction_users:"+id, func(c context.Context, cl *api.Client) (reactionUsersResult, error) {
		p, err := cl.ListReactionUsers(c, id, emoji, api.PageOpts{Cursor: cursor, Limit: reactionUsersPage})
		return reactionUsersResult{owner: o, page: p}, err
	})
}

func (o *reactionUsers) Update(ctx *ui.Context, msg tea.Msg) tea.Cmd {
	m, ok := msg.(ui.ResultMsg[reactionUsersResult])
	if !ok || m.Value.owner != o {
		return nil
	}
	o.loading = false
	if m.Err != nil || m.Value.page == nil {
		o.failed = cat(ctx).Chat.ReactionUsersFailed() + ": " + errtext.Describe(ctx.T, m.Err)
		return nil
	}
	o.failed = ""
	first := !o.loaded
	o.loaded = true
	for _, u := range m.Value.page.Items {
		if !o.has(u.ID) {
			o.items = append(o.items, u)
		}
	}
	o.next = ""
	if c := m.Value.page.NextCursor; c != nil {
		o.next = *c
	}
	o.list.SetItems(o.items)
	if first && len(o.items) > 0 {
		o.list.Select(o.items[0].ID)
	}
	return nil
}

func (o *reactionUsers) has(id string) bool {
	for _, u := range o.items {
		if u.ID == id {
			return true
		}
	}
	return false
}

// atEnd: the selection is on the last loaded item.
func (o *reactionUsers) atEnd() bool {
	sel, ok := o.list.Selected()
	return ok && len(o.items) > 0 && sel.ID == o.items[len(o.items)-1].ID
}

func (o *reactionUsers) Resize(_ *ui.Context, size ui.Size) { o.size = size }

// rows is the number of content rows wanted (at least one: loading, empty, error).
func (o *reactionUsers) rows() int { return max(1, len(o.items)) }

func (o *reactionUsers) Bounds(body layout.Rect) layout.Rect {
	o.shown = o.rows()
	o.bounds = centered(body, reactionUsersMaxW, min(reactionUsersMaxH, max(reactionUsersMinH, o.shown+2)))
	return o.bounds
}

func (o *reactionUsers) Modal() bool { return true }

func (o *reactionUsers) title(c *i18n.T) string {
	return c.Chat.ReactionUsersTitle(textwidth.Sanitize(o.emoji))
}

func (o *reactionUsers) Title(ctx *ui.Context) (string, ui.Badge) {
	return o.title(cat(ctx)), ui.Badge{}
}

func (o *reactionUsers) Help(ctx *ui.Context) []key.Binding {
	return []key.Binding{
		key.NewBinding(key.WithKeys("j", "k"), key.WithHelp("j/k", cat(ctx).Common.HelpMove())),
		key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", cat(ctx).Common.HelpClose())),
	}
}

func (o *reactionUsers) HandleKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	if ctx.Keys.Matches(msg, keymap.NavBack) {
		return false, nil // the app closes it
	}
	handled, cmd := o.list.HandleKey(ctx, msg)
	if !handled {
		return false, nil
	}
	if o.atEnd() {
		cmd = tea.Batch(cmd, o.fetch(ctx))
	}
	return true, cmd
}

func (o *reactionUsers) HandleMouse(ctx *ui.Context, msg tea.MouseMsg, local ui.Point) tea.Cmd {
	local.Y -= o.headRows()
	if local.Y < 0 {
		return nil
	}
	p := overlayPad(o.size.W)
	local.X -= p
	cmd := o.list.HandleMouse(ctx, msg, local)
	if o.list.AtBottom() || o.atEnd() {
		cmd = tea.Batch(cmd, o.fetch(ctx))
	}
	return cmd
}

// bordered: the app drew a border (the title is on it); otherwise the overlay
// was degraded to the whole body and the title goes inside.
func (o *reactionUsers) bordered() bool {
	return o.bounds.W >= 2 && o.bounds.H >= 2 && o.size.W == o.bounds.W-2 && o.size.H == o.bounds.H-2
}

// headRows is the title line (and a spacer) drawn inside when not bordered.
func (o *reactionUsers) headRows() int {
	if o.bordered() || o.size.H <= 1 {
		return 0
	}
	if o.size.H > 2 {
		return 2
	}
	return 1
}

// render draws one user: "› " marker, avatar, name and @handle in one line.
func (o *reactionUsers) render(ctx *ui.Context, u api.UserSummary, selected bool, width int) []string {
	t := themeOf(ctx)
	marker, st := "  ", t.S.Base
	if selected {
		marker, st = "› ", t.S.Selected
	}
	line := st.Render(marker)
	room := width - textwidth.Width(marker)
	if room >= 6 {
		line += widgets.Avatar(t, u, 2) + st.Render(" ")
		room -= 3
	}
	name := widgets.UserName(t, u, isMe(ctx, u.ID))
	handle := "@" + textwidth.Sanitize(u.Handle)
	if nw := textwidth.Width(name); nw+1+textwidth.Width(handle) <= room {
		line += name + st.Render(" ") + t.S.Dim.Render(handle)
	} else {
		line += textwidth.Truncate(name, max(1, room), "…")
	}
	return []string{fitPad(t, line, width)}
}

// fitPad truncates a styled line to width and pads it with Base blanks.
func fitPad(t *theme.Theme, l string, width int) string {
	l = fit(l, width)
	return l + blank(t, width-textwidth.Width(l))
}

func (o *reactionUsers) View(ctx *ui.Context, _ bool) string {
	w, h := o.size.W, o.size.H
	if w <= 0 || h <= 0 {
		return ""
	}
	t := themeOf(ctx)
	p := overlayPad(w)
	cw := w - 2*p
	var head []string
	if n := o.headRows(); n > 0 {
		head = append(head, cell(t.S.Title.Render, o.title(cat(ctx)), cw))
		if n > 1 {
			head = append(head, "")
		}
	}
	bodyH := h - len(head)
	var body []string
	switch {
	case o.failed != "" && len(o.items) == 0:
		body = []string{cell(t.S.Error.Render, textwidth.Sanitize(o.failed), cw)}
	case !o.loaded:
		body = []string{cell(t.S.Dim.Render, cat(ctx).Chat.Loading(), cw)}
	case len(o.items) == 0:
		body = []string{cell(t.S.Dim.Render, cat(ctx).Chat.ReactionUsersEmpty(), cw)}
	default:
		body = strings.Split(o.list.View(ctx, ui.Size{W: cw, H: bodyH}), "\n")
	}
	return renderOverlayRows(t, append(head, body...), o.size)
}
