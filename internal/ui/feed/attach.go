package feed

import (
	"os"
	"path/filepath"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
)

// overlayAttach is the photo picker of the composer. Args "post"|"story", then
// optionally " <compose route key>" (default: the new post / new story
// composer, "compose//post" | "compose//story"), so a reply or quote composer
// gets its own picks back.
const overlayAttach = "feed.attach"

// Upload purposes (api.md 5.14) the picker accepts.
const (
	purposePost  = "post"
	purposeStory = "story"
)

// Attach overlay copy and size.
const (
	textAttachTitle      = "사진 첨부"
	textAttachStoryTitle = "스토리 사진"
	textAttachHint       = "파일 경로 · 예: ~/Pictures/photo.jpg"
	textAttachInfo       = "PNG·JPEG·GIF·WebP · 5MB까지 · 메타데이터는 지워져요"
	textAttachKeys       = "enter 첨부 · esc 취소"
	textFileNotFound     = "파일을 찾을 수 없어요"
	attachW              = 60
	attachH              = 6
)

func init() {
	ui.RegisterOverlay(overlayAttach, func(_ *ui.Context, args string) ui.Overlay {
		if o := newAttach(args); o != nil {
			return o
		}
		return nil
	})
}

// attachPickMsg is a file the user picked for the composer whose route key is
// Route; that composer prepares and uploads it (attachedMsg comes back).
type attachPickMsg struct {
	Route   string
	Purpose string
	Path    string
}

// attachOverlay is feed.attach: one path field. enter checks that the file
// exists (else the toast "파일을 찾을 수 없어요" and the dialog stays), then closes
// and hands the path to the composer.
type attachOverlay struct {
	route   string
	purpose string
	in      textinput.Model
	size    ui.Size
	col     int // cursor column of the last View
}

var (
	_ ui.Overlay    = (*attachOverlay)(nil)
	_ ui.KeyHandler = (*attachOverlay)(nil)
	_ ui.Cursorer   = (*attachOverlay)(nil)
	_ ui.Titled     = (*attachOverlay)(nil)
	_ ui.Helper     = (*attachOverlay)(nil)
)

// newAttach parses "post|story [route key]"; anything else is nil.
func newAttach(args string) *attachOverlay {
	f := strings.Fields(args)
	if len(f) == 0 || len(f) > 2 || (f[0] != purposePost && f[0] != purposeStory) {
		return nil
	}
	route := ui.Route{Kind: ui.RouteCompose, Arg: f[0]}.Key()
	if len(f) == 2 {
		route = f[1]
	}
	return &attachOverlay{route: route, purpose: f[0], in: newPathInput()}
}

// newPathInput is a one-line textinput with the dialog rules (CA 5.3.7): real
// cursor, no prompt, ctrl+k (palette) out of DeleteAfterCursor, clipboard paste
// and suggestions unbound (they would bypass normalization).
func newPathInput() textinput.Model {
	ti := textinput.New()
	ti.Prompt = ""
	ti.SetVirtualCursor(false)
	km := textinput.DefaultKeyMap()
	var keep []string
	for _, k := range km.DeleteAfterCursor.Keys() {
		if k != "ctrl+k" {
			keep = append(keep, k)
		}
	}
	km.DeleteAfterCursor.SetKeys(keep...)
	none := key.NewBinding(key.WithDisabled())
	km.Paste = none
	km.AcceptSuggestion, km.NextSuggestion, km.PrevSuggestion = none, none, none
	ti.KeyMap = km
	ti.Focus()
	return ti
}

// oneLine is textwidth.SanitizeInput for a single-line field (newlines -> spaces).
func oneLine(s string) string {
	return strings.ReplaceAll(textwidth.SanitizeInput(s), "\n", " ")
}

// isTextKey reports a key that types text (no ctrl/alt/meta/super/hyper).
func isTextKey(m tea.KeyPressMsg) bool {
	return m.Text != "" && m.Mod&(tea.ModCtrl|tea.ModAlt|tea.ModMeta|tea.ModSuper|tea.ModHyper) == 0
}

// inputUpdate feeds msg to ti with normalization (typed and pasted text through
// oneLine first, the whole value re-normalized after the edit). ctrl+c clears a
// non-empty value (handled) and is not handled on an empty one.
func inputUpdate(ti *textinput.Model, msg tea.Msg) (handled bool, cmd tea.Cmd) {
	switch m := msg.(type) {
	case tea.KeyPressMsg:
		if m.String() == "ctrl+c" {
			if ti.Value() == "" {
				return false, nil
			}
			ti.Reset()
			return true, nil
		}
		if isTextKey(m) {
			msg = tea.PasteMsg{Content: oneLine(m.Text)}
		}
	case tea.PasteMsg:
		msg = tea.PasteMsg{Content: oneLine(m.Content)}
	default:
		return false, nil
	}
	*ti, cmd = ti.Update(msg)
	if v := ti.Value(); oneLine(v) != v {
		rs := []rune(v)
		before := oneLine(string(rs[:min(ti.Position(), len(rs))]))
		ti.SetValue(oneLine(v))
		ti.SetCursor(len([]rune(before)))
	}
	return true, cmd
}

// expandHome turns a leading "~/" (or a lone "~") into the home directory.
func expandHome(p string) string {
	if p != "~" && !strings.HasPrefix(p, "~/") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	return filepath.Join(home, strings.TrimPrefix(p[1:], "/"))
}

// fileExists reports a regular (non-directory) file at p.
func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

func (o *attachOverlay) Init(*ui.Context) tea.Cmd { return nil }

func (o *attachOverlay) Update(ctx *ui.Context, msg tea.Msg) tea.Cmd {
	forwardResync(ctx, msg)
	cmds := []tea.Cmd{applyResults(ctx, msg)}
	if _, ok := msg.(tea.PasteMsg); ok {
		_, cmd := inputUpdate(&o.in, msg)
		cmds = append(cmds, cmd)
	}
	return tea.Batch(cmds...)
}

func (o *attachOverlay) Resize(_ *ui.Context, size ui.Size) { o.size = size }

func (o *attachOverlay) Bounds(body layout.Rect) layout.Rect {
	return centeredIn(body, min(attachW, body.W-4), attachH)
}

func (o *attachOverlay) Modal() bool { return true }

func (o *attachOverlay) Title(*ui.Context) (string, ui.Badge) {
	if o.purpose == purposeStory {
		return textAttachStoryTitle, ui.Badge{}
	}
	return textAttachTitle, ui.Badge{}
}

func (o *attachOverlay) Help(ctx *ui.Context) []key.Binding {
	return []key.Binding{
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "첨부")),
		key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "취소")),
	}
}

// submit checks the path and hands it to the composer.
func (o *attachOverlay) submit() tea.Cmd {
	raw := strings.TrimSpace(o.in.Value())
	if raw == "" {
		return nil
	}
	p := expandHome(raw)
	if !fileExists(p) {
		return ui.Toast(textFileNotFound, ui.ToastError)
	}
	pick := attachPickMsg{Route: o.route, Purpose: o.purpose, Path: p}
	return tea.Batch(closeOverlay, func() tea.Msg { return pick })
}

func (o *attachOverlay) HandleKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	switch msg.String() {
	case "enter":
		return true, o.submit()
	case "esc", "tab", "shift+tab":
		return false, nil
	}
	return inputUpdate(&o.in, msg)
}

func (o *attachOverlay) Cursor() *tea.Cursor {
	if o.size.W <= 0 || o.size.H <= 0 {
		return nil
	}
	return tea.NewCursor(min(o.col, o.size.W-1), 0)
}

// inputLine draws the field value in exactly w cells (the hint, dim, while
// empty) and returns the cursor column; long values scroll to keep the cursor.
func pathLine(t *theme.Theme, ti textinput.Model, hint string, w int) (string, int) {
	if w <= 0 {
		return "", 0
	}
	rs := []rune(ti.Value())
	if len(rs) == 0 {
		return fit(t, t.S.Placeholder.Render(textwidth.Truncate(hint, w, "…")), w), 0
	}
	pos := min(ti.Position(), len(rs))
	before, after := textwidth.Sanitize(string(rs[:pos])), textwidth.Sanitize(string(rs[pos:]))
	if textwidth.Width(before) >= w {
		before = textwidth.TruncateLeft(before, w-1, "")
		after = ""
	}
	col := textwidth.Width(before)
	return fit(t, t.S.Base.Render(before+textwidth.Truncate(after, w-col, "")), w), col
}

func (o *attachOverlay) View(ctx *ui.Context, _ bool) string {
	t := themeOf(ctx)
	w, h := o.size.W, o.size.H
	if w <= 0 || h <= 0 {
		return ""
	}
	line, col := pathLine(t, o.in, textAttachHint, w)
	o.col = col
	out := []string{
		line,
		t.S.Base.Foreground(t.P.Line).Render(strings.Repeat("─", w)),
		fit(t, t.S.Dim.Render(textwidth.Truncate(textAttachInfo, w, "…")), w),
		fit(t, t.S.Dim.Render(textwidth.Truncate(textAttachKeys, w, "…")), w),
	}
	return blankRows(t, out, w, h)
}
