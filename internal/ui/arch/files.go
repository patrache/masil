package arch

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/archdoc"
	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/widgets"
)

// filesStep is what the files dialog is doing.
type filesStep int

const (
	filesList   filesStep = iota // the list of personal designs
	filesNew                     // typing a new design's name
	filesRename                  // typing the selected design's new name
	filesImport                  // typing the path of a design JSON
)

// maxInput caps a typed name or path (bytes).
const maxInput = 1024

// filesDialog is arch.open (P3 Task 20): the personal designs, newest first
// (name · title · node count · modified time). enter opens one, n starts a new
// named design, r renames, d deletes (after a confirm), i imports a design
// JSON file (at most 1MB, archdoc.Decode; the failure reason is shown).
type filesDialog struct {
	entries []archdoc.Entry
	cur     int
	step    filesStep
	input   string
	hint    string
	size    ui.Size
	curX    int // the input cursor (-1 = none)
	curY    int
}

var (
	_ ui.Overlay    = (*filesDialog)(nil)
	_ ui.KeyHandler = (*filesDialog)(nil)
	_ ui.Titled     = (*filesDialog)(nil)
	_ ui.Cursorer   = (*filesDialog)(nil)
)

func newFilesDialog() *filesDialog { return &filesDialog{curX: -1} }

// filesDeletedMsg reports a confirmed delete of the dialog's design name.
type filesDeletedMsg struct {
	dlg  *filesDialog
	name string
	err  error
}

// importDoneMsg reports an import: name it was saved as, else hint says why not.
type importDoneMsg struct {
	dlg  *filesDialog
	name string
	hint string
}

func (d *filesDialog) Init(ctx *ui.Context) tea.Cmd {
	d.reload(ctx)
	return nil
}

func (d *filesDialog) Resize(_ *ui.Context, size ui.Size) { d.size = size }
func (d *filesDialog) Modal() bool                        { return true }
func (d *filesDialog) Title(ctx *ui.Context) (string, ui.Badge) {
	return ctx.T.Archcanvas.FilesTitle(), ui.Badge{}
}

func (d *filesDialog) Bounds(body layout.Rect) layout.Rect {
	// the rows, a blank, the hints (two when a reason shows) and the border
	return centeredRect(body, 76, min(max(len(d.entries)+6, 10), 22))
}

// centeredRect is a w x h rect centred in body (clamped to it).
func centeredRect(body layout.Rect, w, h int) layout.Rect {
	w, h = min(w, body.W), min(h, body.H)
	return layout.Rect{X: body.X + max(0, (body.W-w)/2), Y: body.Y + max(0, (body.H-h)/2), W: w, H: h}
}

// reload lists the store again (the selection stays on the same name).
func (d *filesDialog) reload(ctx *ui.Context) {
	sel := ""
	if d.cur < len(d.entries) {
		sel = d.entries[d.cur].Name
	}
	d.entries = nil
	st, err := storeOf(ctx)
	if err == nil {
		d.entries, err = st.List()
	}
	if err != nil {
		d.hint = ctx.T.Archcanvas.FilesFailed()
	}
	d.selectName(sel)
}

func (d *filesDialog) selectName(name string) {
	d.cur = min(d.cur, max(len(d.entries)-1, 0))
	for i, e := range d.entries {
		if e.Name == name {
			d.cur = i
		}
	}
}

// selected is the design under the cursor ("" = none).
func (d *filesDialog) selected() string {
	if d.cur < len(d.entries) {
		return d.entries[d.cur].Name
	}
	return ""
}

// isOpen reports whether personal design name is open on the tool surface: it
// is not renamed or deleted under its canvas (whose save would bring it back).
func isOpen(ctx *ui.Context, name string) bool {
	if ctx == nil || ctx.OpenTools == nil {
		return false
	}
	for _, o := range ctx.OpenTools() {
		if o.Route.Arg == ui.ToolArch && o.Route.ID == keyFile+name {
			return true
		}
	}
	return false
}

func (d *filesDialog) Update(ctx *ui.Context, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.PasteMsg:
		if d.step != filesList {
			d.typeText(msg.Content)
		}
	case filesDeletedMsg:
		if msg.dlg != d {
			return nil
		}
		d.reload(ctx)
		if msg.err != nil {
			d.hint = ctx.T.Archcanvas.FilesFailed()
			return nil
		}
		return ui.Toast(ctx.T.Archcanvas.FilesDeleted(cleanText(msg.name)), ui.ToastInfo)
	case importDoneMsg:
		if msg.dlg != d {
			return nil
		}
		if msg.hint != "" {
			d.hint = msg.hint
			return nil
		}
		return tea.Batch(closeOverlay, openFile(msg.name), ui.Toast(ctx.T.Archcanvas.ImportDone(cleanText(msg.name)), ui.ToastSuccess))
	}
	return nil
}

// closeOverlay closes the top overlay.
func closeOverlay() tea.Msg { return ui.CloseOverlayMsg{} }

// typeText adds typed or pasted text to the input (one line).
func (d *filesDialog) typeText(s string) {
	s = strings.NewReplacer("\r\n", "", "\r", "", "\n", "", "\t", " ").Replace(s)
	s = textwidth.SanitizeInput(s)
	for _, r := range s {
		if len(d.input)+utf8.RuneLen(r) > maxInput {
			break
		}
		d.input += string(r)
	}
}

func (d *filesDialog) HandleKey(ctx *ui.Context, k tea.KeyPressMsg) (bool, tea.Cmd) {
	s := k.String()
	if d.step != filesList {
		return true, d.inputKey(ctx, k, s)
	}
	a := ctx.T.Archcanvas
	d.hint = ""
	switch s {
	case "j", "down":
		d.cur = min(d.cur+1, max(len(d.entries)-1, 0))
	case "k", "up":
		d.cur = max(d.cur-1, 0)
	case "home", "g":
		d.cur = 0
	case "end", "G":
		d.cur = max(len(d.entries)-1, 0)
	case "enter":
		if name := d.selected(); name != "" {
			return true, tea.Batch(closeOverlay, openFile(name))
		}
	case "n":
		d.step, d.input = filesNew, ""
	case "i":
		d.step, d.input = filesImport, ""
	case "r":
		name := d.selected()
		switch {
		case name == "":
		case isOpen(ctx, name):
			d.hint = a.FilesIsOpen()
		default:
			d.step, d.input = filesRename, name
		}
	case "d", "delete":
		name := d.selected()
		switch {
		case name == "":
		case isOpen(ctx, name):
			d.hint = a.FilesIsOpen()
		default:
			st, err := storeOf(ctx)
			if err != nil {
				d.hint = a.FilesFailed()
				return true, nil
			}
			del := func() tea.Msg { return filesDeletedMsg{dlg: d, name: name, err: st.Delete(name)} }
			return true, ui.OpenOverlay(widgets.Confirm(a.FilesDeleteTitle(cleanText(name)), a.FilesDeleteBody(), a.FilesDeleteYes(), a.KeyCancel(), del))
		}
	default:
		return false, nil
	}
	return true, nil
}

// inputKey is a key while a name or a path is typed.
func (d *filesDialog) inputKey(ctx *ui.Context, k tea.KeyPressMsg, s string) tea.Cmd {
	switch s {
	case "esc":
		d.step, d.input, d.hint = filesList, "", ""
	case "backspace":
		if r := []rune(d.input); len(r) > 0 {
			d.input = string(r[:len(r)-1])
		}
	case "ctrl+u":
		d.input = ""
	case "enter":
		return d.submit(ctx)
	default:
		if k.Text != "" {
			d.typeText(k.Text)
		}
	}
	return nil
}

// submit acts on the typed input.
func (d *filesDialog) submit(ctx *ui.Context) tea.Cmd {
	a := ctx.T.Archcanvas
	st, err := storeOf(ctx)
	if err != nil {
		d.hint = a.FilesFailed()
		return nil
	}
	switch d.step {
	case filesNew:
		name, err := archdoc.SafeName(d.input)
		if err != nil {
			d.hint = a.FilesBadName()
			return nil
		}
		for _, e := range d.entries {
			if strings.EqualFold(e.Name, name) {
				d.hint = a.FilesNameExists()
				return nil
			}
		}
		return tea.Batch(closeOverlay, openFile(name))
	case filesRename:
		from := d.selected()
		to, err := archdoc.SafeName(d.input)
		if err != nil {
			d.hint = a.FilesBadName()
			return nil
		}
		if to == from {
			d.step, d.input, d.hint = filesList, "", ""
			return nil
		}
		if isOpen(ctx, to) {
			d.hint = a.FilesIsOpen()
			return nil
		}
		switch err := st.Rename(from, to); {
		case errors.Is(err, archdoc.ErrExists):
			d.hint = a.FilesNameExists()
			return nil
		case errors.Is(err, archdoc.ErrBadName):
			d.hint = a.FilesBadName()
			return nil
		case err != nil:
			d.hint = a.FilesFailed()
			return nil
		}
		d.step, d.input, d.hint = filesList, "", ""
		d.reload(ctx)
		d.selectName(to)
	case filesImport:
		path, t := d.input, ctx.T
		return func() tea.Msg {
			name, hint := importDesign(st, t, path)
			return importDoneMsg{dlg: d, name: name, hint: hint}
		}
	}
	return nil
}

// expandHome turns a leading "~/" into the home folder.
func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[1:])
		}
	}
	return p
}

// importDesign reads the design JSON at path (at most archdoc.MaxBytes),
// decodes it and saves it as a new personal design named after its title (or
// the file), never over an existing one. It returns the name, or the reason
// it was not imported.
func importDesign(st *archdoc.Store, t *i18n.T, path string) (name, hint string) {
	a := t.Archcanvas
	p := expandHome(strings.TrimSpace(path))
	f, err := os.Open(p)
	if err != nil {
		return "", a.ImportReadFailed()
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return "", a.ImportReadFailed()
	}
	if info.Size() > archdoc.MaxBytes {
		return "", a.ImportTooLarge()
	}
	data, err := io.ReadAll(io.LimitReader(f, archdoc.MaxBytes+1))
	if err != nil {
		return "", a.ImportReadFailed()
	}
	d, err := archdoc.Decode(data)
	if err != nil {
		return "", decodeReason(t, err)
	}
	base := strings.TrimSuffix(filepath.Base(p), filepath.Ext(p))
	for _, cand := range []string{fileSafe(d.Title), fileSafe(base), "arch"} {
		if cand == "" {
			continue
		}
		for i := 1; i <= 99; i++ {
			n := cand
			if i > 1 {
				n = cand + " (" + strconvItoa(i) + ")"
			}
			if _, err := archdoc.SafeName(n); err != nil {
				break
			}
			switch err := st.Save(n, d, false); {
			case err == nil:
				return n, ""
			case errors.Is(err, archdoc.ErrExists):
				continue
			default:
				return "", a.FilesFailed()
			}
		}
	}
	return "", a.FilesNameExists()
}

// decodeReason is why archdoc.Decode refused a file, in words.
func decodeReason(t *i18n.T, err error) string {
	a := t.Archcanvas
	var ie *archdoc.InvalidError
	if !errors.As(err, &ie) || len(ie.Issues) == 0 {
		return a.ImportSyntax()
	}
	is := ie.Issues[0]
	switch is.Code {
	case archdoc.IssueTooLarge:
		return a.ImportTooLarge()
	case archdoc.IssueSyntax:
		return a.ImportSyntax()
	case archdoc.IssueSchemaVersion:
		return a.ImportVersion()
	}
	return a.ImportInvalid(cleanText(is.String()))
}

// fileSafe makes s a design or file name: the characters a file name cannot
// hold become "-", leading dots and outer spaces go ("" when nothing is left
// or it is still not a safe name).
func fileSafe(s string) string {
	s = cleanText(s)
	s = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`/\:*?"<>|`, r) {
			return '-'
		}
		return r
	}, s)
	s = strings.TrimSpace(strings.TrimLeft(s, ". "))
	n, err := archdoc.SafeName(s)
	if err != nil {
		return ""
	}
	return n
}

// lines is the dialog's content.
func (d *filesDialog) lines(ctx *ui.Context, w, h int) []formLine {
	a := ctx.T.Archcanvas
	var out []formLine
	foot := []formLine{}
	if d.hint != "" {
		foot = append(foot, formLine{s: d.hint, st: sWarn, row: -1})
	}
	if d.step != filesList {
		head := a.FilesNewPrompt()
		switch d.step {
		case filesRename:
			head = a.FilesRenamePrompt(cleanText(d.selected()))
		case filesImport:
			head = a.FilesImportPrompt()
		}
		out = append(out, formLine{s: head, st: sAccBold, row: -1}, formLine{s: "> " + d.input, st: sBase, row: 0, in: true}, formLine{row: -1})
		foot = append(foot, formLine{s: a.FilesInputHint(), st: sDim, row: -1})
		return append(out, foot...)
	}
	foot = append(foot, formLine{s: a.FilesHint(), st: sDim, row: -1})
	if len(d.entries) == 0 {
		return append([]formLine{{s: a.FilesEmpty(), st: sDim, row: -1}, {row: -1}}, foot...)
	}
	rows := max(h-len(foot)-1, 1)
	top := min(max(d.cur-rows+1, 0), max(len(d.entries)-rows, 0))
	for i := top; i < len(d.entries) && i < top+rows; i++ {
		e := d.entries[i]
		name := cleanText(e.Name)
		if t := cleanText(e.Title); t != "" && t != name {
			name += " · " + t
		}
		right := a.FilesNodes(e.Nodes) + " · " + e.Modified.Local().Format("2006-01-02 15:04")
		lw := max(w-2-textwidth.Width(right)-2, 4)
		left := textwidth.PadRight(textwidth.Truncate(name, lw, "…"), lw)
		st, mark := sBase, "  "
		if i == d.cur {
			st, mark = sSel, "▸ "
		}
		out = append(out, formLine{s: mark + left + "  " + right, st: st, row: i})
	}
	out = append(out, formLine{row: -1})
	return append(out, foot...)
}

func (d *filesDialog) View(ctx *ui.Context, _ bool) string {
	w, h := d.size.W, d.size.H
	if w <= 0 || h <= 0 {
		return ""
	}
	b := newBuf(w, h)
	b.fill(layout.Rect{W: w, H: h}, sBase)
	d.curX = -1
	for i, l := range d.lines(ctx, w-2, h) {
		if i >= h {
			break
		}
		b.line(1, i, w-2, l.s, l.st)
		if l.in {
			if x := 1 + textwidth.Width(l.s); x < w-1 {
				d.curX, d.curY = x, i
			}
		}
	}
	return b.render(ctx.Theme)
}

// Cursor is the real cursor at the end of the typed input.
func (d *filesDialog) Cursor() *tea.Cursor {
	if d.step == filesList || d.curX < 0 {
		return nil
	}
	return tea.NewCursor(d.curX, d.curY)
}
