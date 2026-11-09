package leet

import (
	"context"
	"slices"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/leetcode"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
)

// The settings overlay (leet.settings, A10.4): the language and solutions
// folder and the status switch go to config.toml [tools.leet]; sharing my
// streak with friends is the server setting leet_share_with_friends; and
// every space I own or administer gets a #daily switch (instead of a row in
// the space settings screen — its goldens stay as they are, JX9).

const settingsW, settingsH = 64, 14

type setRowKind int

const (
	rowLang setRowKind = iota
	rowDir
	rowAuto
	rowShare
	rowDaily
)

type setRow struct {
	kind  setRowKind
	space string // rowDaily
}

type settingsDlg struct {
	sel     int
	editing bool      // the folder field has the keys
	dir     lineInput // the folder being typed
	size    ui.Size
	inputY  int // the field's row in the last draw (-1 = not drawn)
	inputX  int
	// sharing is the friends switch's value while its PATCH is out (nil =
	// none): only the dialog shows it, the store keeps the server's.
	sharing *bool
}

var (
	_ ui.Overlay       = (*settingsDlg)(nil)
	_ ui.KeyHandler    = (*settingsDlg)(nil)
	_ ui.Titled        = (*settingsDlg)(nil)
	_ ui.InputCapturer = (*settingsDlg)(nil)
	_ ui.Cursorer      = (*settingsDlg)(nil)
)

func newSettings(*ui.Context, string) ui.Overlay { return &settingsDlg{inputY: -1} }

func (d *settingsDlg) Init(*ui.Context) tea.Cmd        { return nil }
func (d *settingsDlg) Resize(_ *ui.Context, s ui.Size) { d.size = s }
func (d *settingsDlg) Modal() bool                     { return true }
func (d *settingsDlg) CapturesInput() bool             { return d.editing }
func (d *settingsDlg) Bounds(body layout.Rect) layout.Rect {
	return centered(body, settingsW, settingsH)
}
func (d *settingsDlg) Title(ctx *ui.Context) (string, ui.Badge) {
	return ctx.T.Leetcode.SettingsTitle(), ui.Badge{}
}

// shareDoneMsg is the friends switch's answer (settings = nil: it failed).
type shareDoneMsg struct{ settings *api.Settings }

func (d *settingsDlg) Update(ctx *ui.Context, msg tea.Msg) tea.Cmd {
	if m, ok := msg.(shareDoneMsg); ok {
		d.sharing = nil
		if m.settings != nil && ctx.Store != nil {
			ctx.Store.Settings = *m.settings
			if ctx.Store.Me != nil {
				ctx.Store.Me.Settings = *m.settings
			}
		}
	}
	return nil
}

// rows are the dialog's rows now: the spaces I manage come last, in the
// store's order.
func (d *settingsDlg) rows(ctx *ui.Context) []setRow {
	rows := []setRow{{kind: rowLang}, {kind: rowDir}, {kind: rowAuto}, {kind: rowShare}}
	if ctx.Store != nil {
		for _, id := range ctx.Store.SpaceOrder {
			if canManage(ctx, id) {
				rows = append(rows, setRow{kind: rowDaily, space: id})
			}
		}
	}
	return rows
}

func (d *settingsDlg) HandleKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	k := msg.String()
	if d.editing {
		switch k {
		case "enter":
			d.editing = false
			v := strings.TrimSpace(d.dir.String())
			if v == "" {
				return true, setToolKey(ctx, "dir", nil)
			}
			return true, setToolKey(ctx, "dir", v)
		case "esc":
			d.editing = false
			return true, nil
		case "ctrl+c":
			return false, nil
		}
		d.dir.key(msg)
		return true, nil
	}
	rows := d.rows(ctx)
	d.sel = min(max(d.sel, 0), len(rows)-1)
	switch k {
	case "up", "k", "shift+tab":
		d.sel = max(0, d.sel-1)
		return true, nil
	case "down", "j", "tab":
		d.sel = min(len(rows)-1, d.sel+1)
		return true, nil
	case "esc", "q":
		return true, closeOverlay
	case "ctrl+c":
		return false, nil
	case "enter", "space", "right", "l", "left", "h":
	default:
		return true, nil
	}
	r := rows[d.sel]
	switch r.kind {
	case rowLang:
		step := 1
		if k == "left" || k == "h" {
			step = -1
		}
		cur := currentLang(ctx)
		i := slices.IndexFunc(leetcode.Languages, func(l leetcode.Lang) bool { return l.Slug == cur.Slug })
		n := len(leetcode.Languages)
		next := leetcode.Languages[((i+step)%n+n)%n]
		return true, tea.Batch(setToolKey(ctx, "lang", next.Slug), emit(langChangedMsg{}))
	case rowDir:
		if k == "enter" || k == "space" {
			d.editing = true
			d.dir.set(toolConfig(ctx).Dir)
		}
		return true, nil
	case rowAuto:
		return true, setToolKey(ctx, "auto_status", !toolConfig(ctx).AutoStatus)
	case rowShare:
		on := true
		if ctx.Store != nil {
			on = ctx.Store.Settings.LeetShareWithFriends
		}
		if d.sharing != nil {
			return true, nil // one at a time
		}
		next := !on
		d.sharing = &next
		return true, shareToggle(ctx, next)
	case rowDaily:
		return true, setDaily(ctx, r.space, !dailyOn(ctx, r.space))
	}
	return true, nil
}

// shareToggle sets "show my streak to friends" to next. Nothing is changed
// before the server says so: the answer comes back as settings.updated
// (realtime) and as shareDoneMsg to an open dialog; a failure is a toast
// from the Cmd itself, so it shows with the dialog closed too.
func shareToggle(ctx *ui.Context, next bool) tea.Cmd {
	client := ctx.API
	return func() tea.Msg {
		if client == nil {
			return shareDoneMsg{}
		}
		st, err := client.UpdateSettings(context.Background(), api.UpdateSettingsRequest{LeetShareWithFriends: api.Some(next)})
		if err != nil {
			return tea.BatchMsg{
				func() tea.Msg { return ui.ErrorMsg{Err: err} },
				func() tea.Msg { return shareDoneMsg{} },
			}
		}
		return shareDoneMsg{settings: st}
	}
}

// langChangedMsg tells open instances the language setting changed.
type langChangedMsg struct{}

func onOff(ctx *ui.Context, on bool) string {
	if on {
		return ctx.T.Leetcode.On()
	}
	return ctx.T.Leetcode.Off()
}

func (d *settingsDlg) View(ctx *ui.Context, _ bool) string {
	w, h := d.size.W, d.size.H
	d.inputY = -1
	if w <= 0 || h <= 0 {
		return ""
	}
	th, t := themeOf(ctx), ctx.T
	cw := contentWidth(w)
	rows := d.rows(ctx)
	d.sel = min(max(d.sel, 0), len(rows)-1)
	cfg := toolConfig(ctx)
	share := true
	if ctx.Store != nil {
		share = ctx.Store.Settings.LeetShareWithFriends
	}
	if d.sharing != nil {
		share = *d.sharing
	}
	labelOf := func(r setRow) string {
		switch r.kind {
		case rowLang:
			return t.Leetcode.SettingsLang()
		case rowDir:
			return t.Leetcode.SettingsDir()
		case rowAuto:
			return t.Leetcode.SettingsAutoStatus()
		case rowShare:
			return t.Leetcode.SettingsShareFriends()
		}
		return t.Leetcode.SettingsDaily(spaceName(ctx, r.space))
	}
	labelW := 0
	for _, r := range rows {
		labelW = max(labelW, textwidth.Width(textwidth.Sanitize(labelOf(r))))
	}
	labelW = min(labelW, max(4, cw/2))
	var mid []string
	focus := 0
	for i, r := range rows {
		var val string
		switch r.kind {
		case rowLang:
			val = currentLang(ctx).Name
		case rowDir:
			val = shortPath(solutionDir(ctx))
		case rowAuto:
			val = onOff(ctx, cfg.AutoStatus)
		case rowShare:
			val = onOff(ctx, share)
		case rowDaily:
			val = onOff(ctx, dailyOn(ctx, r.space))
		}
		label := textwidth.PadRight(textwidth.Truncate(textwidth.Sanitize(labelOf(r)), labelW, "…"), labelW)
		mark := "  "
		st := th.S.Base
		if i == d.sel {
			mark, st = "› ", th.S.Selected
			focus = len(mid)
		}
		room := max(1, cw-2-labelW-2)
		if r.kind == rowDir && d.editing && i == d.sel {
			shown, col := d.dir.shown(room)
			d.inputX = dialogPad(w) + 2 + labelW + 2 + col
			d.inputY = len(mid)
			mid = append(mid, st.Render(mark+label+"  ")+th.S.InputFocused.Render(textwidth.PadRight(shown, room)))
		} else {
			mid = append(mid, st.Render(textwidth.PadRight(mark+label+"  "+textwidth.Truncate(textwidth.Sanitize(val), room, "…"), cw)))
		}
		if r.kind == rowAuto {
			for _, l := range textwidth.Wrap(t.Leetcode.AutoStatusDesc(), max(1, cw-4)) {
				mid = append(mid, th.S.Dim.Render("    "+l))
			}
		}
	}
	footer := t.Leetcode.SettingsFooter()
	if d.editing {
		footer = t.Leetcode.SettingsDirFooter()
	}
	foot := []string{"", th.S.Dim.Render(textwidth.Truncate(footer, cw, "…"))}
	out, top := stack(nil, mid, foot, focus, h, 0)
	if d.inputY >= 0 {
		d.inputY += top
		if d.inputY < 0 || d.inputY >= h {
			d.inputY = -1
		}
	}
	return renderRows(th, out, d.size)
}

// Cursor is the real cursor in the folder field.
func (d *settingsDlg) Cursor() *tea.Cursor {
	if !d.editing || d.inputY < 0 {
		return nil
	}
	return tea.NewCursor(min(d.inputX, max(0, d.size.W-1)), d.inputY)
}

// lineInput is a one-line text field with the cursor at its end.
type lineInput struct{ v []rune }

func (l *lineInput) set(s string)   { l.v = []rune(s) }
func (l *lineInput) String() string { return string(l.v) }

// key applies a key: typed text, backspace, ctrl+u (clear).
func (l *lineInput) key(msg tea.KeyPressMsg) {
	switch msg.String() {
	case "backspace", "ctrl+h":
		if len(l.v) > 0 {
			l.v = l.v[:len(l.v)-1]
		}
		return
	case "ctrl+u":
		l.v = nil
		return
	}
	if msg.Text != "" {
		l.add(msg.Text)
	}
}

// add appends text without control characters or line breaks.
func (l *lineInput) add(s string) {
	for _, r := range s {
		if !unicode.IsControl(r) && len(l.v) < 4096 {
			l.v = append(l.v, r)
		}
	}
}

// shown is the visible tail of the value in room cells and the cursor column.
func (l *lineInput) shown(room int) (string, int) {
	s := textwidth.Sanitize(string(l.v))
	if textwidth.Width(s) > room-1 {
		s = textwidth.TruncateLeft(s, max(0, room-1), "…")
	}
	return s, textwidth.Width(s)
}
