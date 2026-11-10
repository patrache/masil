package leet

import (
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/alecthomas/chroma/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/ionut-t/goeditor/core"

	"github.com/patrache/masil/internal/codeassist"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/third_party/goeditor"
	"github.com/patrache/masil/internal/ui"
)

// goEditor is the built-in editor (no nvim, no pane: Windows, tiny): a
// goeditor vim-mode buffer on the solution file. masil drives it by hand —
// its Cmds are never run (one of them waits on its signal channel forever,
// others are timers): the signals are drained right after each key. Ex
// commands: :w saves to the solution file (the only file it writes), :Run
// and :Submit ask the tool to run / submit (the saved file), and so do r and
// s in normal mode (vim's replace and substitute give way, A10.4). esc in
// normal mode leaves the editor, so the tool's keys (e, l, o…) work again.
type goEditor struct {
	m    goeditor.Model
	id   string // the instance
	path string
	size ui.Size

	// leave is set when a key asks to leave the editor (esc in normal mode, :q).
	leave bool

	// v0.1.3-1: syntax colors and completion (the settings' switches).
	lang   string // codeassist language
	th     *theme.Theme
	styles map[chroma.TokenType]lipgloss.Style
	hlOn   bool
	cpOn   bool
	forced bool // ctrl+n / ctrl+space: the menu from the first char
	words  codeassist.Index
}

var _ codeEditor = (*goEditor)(nil)

// newGoEditor opens path (it must exist) in a goeditor buffer, colored for
// a dark or light background as masil's theme is (goeditor would ask the
// terminal itself, racing Bubble Tea for its input: third_party/goeditor),
// its syntax in th's colors for lang (a LeetCode slug).
func newGoEditor(path string, th *theme.Theme, lang string) (codeEditor, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	m := goeditor.New(80, 20, goeditor.WithDarkBackground(th.Dark))
	m.SetContent(string(b))
	m.Focus()
	m.WithCompletionAutoTrigger(false) // masil asks and answers (refreshCompletion)
	m.SetCompletionMenuMaxVisibleItems(codeassist.MaxVisible)
	e := &goEditor{m: m, path: path, lang: codeassist.FromLeet(lang), th: th, cpOn: true}
	e.m.SetSyntaxStyle(e.tokenStyle)
	e.m.SetCursorCellStyle(cursorCellStyle(th))
	e.setHighlight(true)
	e.drain()
	return e, nil
}

// tokenStyle is a token's color in masil's theme (cached).
func (e *goEditor) tokenStyle(tt chroma.TokenType) lipgloss.Style {
	if e.styles == nil {
		e.styles = map[chroma.TokenType]lipgloss.Style{}
	}
	st, ok := e.styles[tt]
	if !ok {
		st = e.th.EditorTokenStyle(tt)
		e.styles[tt] = st
	}
	return st
}

func (e *goEditor) setHighlight(on bool) {
	e.hlOn = on
	if on {
		e.m.SetLanguage(codeassist.ChromaName(e.lang), "")
	} else {
		e.m.SetLanguage("", "")
	}
}

// setLang follows a language change that kept the file (python3 / python).
func (e *goEditor) setLang(slug string) {
	if l := codeassist.FromLeet(slug); l != e.lang {
		e.lang = l
		e.setHighlight(e.hlOn)
	}
}

// applyAssist follows the settings' switches and the theme.
func (e *goEditor) applyAssist(ctx *ui.Context) {
	if th := themeOf(ctx); th != e.th {
		e.th, e.styles = th, nil
		e.m.SetSyntaxStyle(e.tokenStyle)
		e.m.SetCursorCellStyle(cursorCellStyle(th))
	}
	hl, cp := true, true
	if ctx != nil && ctx.Config != nil {
		hl, cp = ctx.Config.UI.CodeHighlight, ctx.Config.UI.CodeComplete
	}
	if hl != e.hlOn {
		e.setHighlight(hl)
	}
	if e.cpOn = cp; !cp {
		e.closeCompletion()
	}
}

// completionKeys: ctrl+n / ctrl+p are mine now (insert mode, completion on).
func (e *goEditor) completionKeys() bool { return e.cpOn && e.m.IsInsertMode() }

// refreshCompletion asks for the menu of the word being typed and answers
// at once (goeditor's own request goes out on a timer masil never runs).
func (e *goEditor) refreshCompletion() {
	if !e.cpOn || !e.m.IsInsertMode() {
		e.closeCompletion()
		return
	}
	b := e.m.GetEditor().GetBuffer()
	pos := b.GetCursor().Position
	line := b.GetLineRunes(pos.Row)
	prefix := codeassist.Prefix(line, pos.Col)
	if !e.forced && len([]rune(prefix)) < codeassist.MinPrefix {
		e.closeCompletion()
		return
	}
	e.words.Update(b.GetLines())
	items := codeassist.Complete(e.lang, prefix, codeassist.WordAt(line, pos.Col), e.words.Counts())
	if len(items) == 0 {
		e.closeCompletion()
		return
	}
	cs := make([]core.Completion, len(items))
	for i, it := range items {
		cs[i] = core.Completion{Text: it, Label: textwidth.Sanitize(it)}
	}
	e.answer(cs)
}

// answer opens the menu with cs (none: closes it).
func (e *goEditor) answer(cs []core.Completion) {
	e.feed(tea.KeyPressMsg{Code: tea.KeySpace, Mod: tea.ModCtrl}) // the request: its context
	var req *core.CompletionContext
	for _, s := range e.drain() {
		if r, ok := s.(core.CompletionRequestSignal); ok {
			c := r.Context()
			req = &c
		}
	}
	if req != nil {
		e.feed(goeditor.CompletionResponseMsg{Completions: cs, Context: *req})
	}
}

func (e *goEditor) closeCompletion() {
	e.forced = false
	if e.m.CompletionMenuVisible() && e.m.IsInsertMode() {
		e.answer(nil)
	}
}

// menuKeys are what goeditor's open menu takes.
var menuKeys = map[string]bool{"up": true, "down": true, "ctrl+n": true, "ctrl+p": true,
	"tab": true, "enter": true, "esc": true, "ctrl+y": true, "ctrl+e": true}

// afterTyping updates the menu after a key in insert mode: a word rune
// refills (or opens) it, a deletion refills an open one, anything else
// closes it.
func (e *goEditor) afterTyping(msg tea.KeyPressMsg) {
	switch {
	case msg.Text != "":
		for _, r := range msg.Text {
			if !codeassist.IsWordRune(r) {
				e.closeCompletion()
				return
			}
		}
		e.refreshCompletion()
	case msg.Code == tea.KeyBackspace || msg.Code == tea.KeyDelete:
		if e.m.CompletionMenuVisible() {
			e.refreshCompletion()
		}
	default:
		e.closeCompletion()
	}
}

func (e *goEditor) Path() string { return e.path }

// Reload reads the file again (after an outside editor wrote it).
func (e *goEditor) Reload() error {
	b, err := os.ReadFile(e.path)
	if err != nil {
		return err
	}
	e.m.SetContent(string(b))
	e.drain()
	return nil
}

func (e *goEditor) Init(*ui.Context) tea.Cmd            { return nil }
func (e *goEditor) Update(*ui.Context, tea.Msg) tea.Cmd { return nil }

func (e *goEditor) Resize(_ *ui.Context, s ui.Size) {
	e.size = s
	if s.W > 0 && s.H > 0 {
		e.m.SetSize(s.W, s.H)
	}
}

// CapturesInput: every printable key is the editor's while it has focus.
func (e *goEditor) CapturesInput() bool { return true }

// cursorCellStyle is the cursor cell (v0.1.3-1): reverse video of the text
// colors, so it stands out in every theme, also where the terminal's own
// cursor is hard to see.
func cursorCellStyle(th *theme.Theme) *lipgloss.Style {
	st := lipgloss.NewStyle().Foreground(th.P.FG).Background(th.P.BG).Reverse(true)
	return &st
}

// Cursor is the terminal's real cursor on goeditor's cursor cell: a block
// in normal mode, a bar in insert and command mode (v0.1.3-1; goeditor
// still draws the cell itself).
func (e *goEditor) Cursor() *tea.Cursor {
	x, y, ok := e.m.CursorPosition()
	if !ok || x >= e.size.W || y >= e.size.H {
		return nil
	}
	c := tea.NewCursor(x, y)
	if e.m.IsInsertMode() || e.m.IsCommandMode() {
		c.Shape = tea.CursorBar
	}
	return c
}

// takeLeave reports (once) that the last key asked to leave the editor.
func (e *goEditor) takeLeave() bool {
	l := e.leave
	e.leave = false
	return l
}

// HandleKey gives the key to goeditor, except masil's ex commands and esc
// in normal mode (handled=false: the tool takes focus back).
func (e *goEditor) HandleKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	k := msg.String()
	if k == "ctrl+c" {
		return false, nil
	}
	if e.m.IsCommandMode() && k == "enter" {
		line := strings.TrimSpace(strings.TrimPrefix(e.m.GetEditor().GetState().CommandLine, ":"))
		var req tea.Msg
		switch line {
		case "Run":
			req = runRequestedMsg{id: e.id}
		case "Submit":
			req = submitRequestedMsg{id: e.id}
		}
		if req != nil {
			e.feed(tea.KeyPressMsg{Code: tea.KeyEscape}) // leave command mode, nothing executed
			return true, emit(req)
		}
	}
	if k == "esc" && e.m.IsNormalMode() {
		e.leave = true
		return false, nil
	}
	if (k == "r" || k == "s") && e.normalIdle() { // Task 20: r / s after esc run / submit
		if k == "r" {
			return true, emit(runRequestedMsg{id: e.id})
		}
		return true, emit(submitRequestedMsg{id: e.id})
	}
	e.applyAssist(ctx)
	open := e.m.CompletionMenuVisible()
	if e.m.IsInsertMode() && !open && (k == "ctrl+n" || k == "ctrl+space" || k == "ctrl+@") {
		if e.cpOn {
			e.forced = true
			e.refreshCompletion()
		}
		return true, nil
	}
	e.feedKey(msg)
	cmd := e.afterKey(ctx)
	switch {
	case !e.m.IsInsertMode():
		e.forced = false
	case open && menuKeys[k]:
		if !e.m.CompletionMenuVisible() {
			e.forced = false // accepted or closed
		}
	default:
		e.afterTyping(msg)
	}
	return true, cmd
}

// normalIdle: normal mode with nothing part typed (no count, operator,
// prefix or argument: the r of "fr" or "dr" stays vim's).
func (e *goEditor) normalIdle() bool {
	if !e.m.IsNormalMode() {
		return false
	}
	ed := e.m.GetEditor()
	if ed.GetState().PendingCount != nil {
		return false
	}
	idle, ok := ed.GetMode().(interface{ Idle() bool })
	return ok && idle.Idle()
}

// feedKey gives goeditor a key: typed text of several runes (an IME
// committing more than one at once) goes as one key per rune, since
// goeditor reads one rune from a key (v0.1.3-1).
func (e *goEditor) feedKey(msg tea.KeyPressMsg) {
	if msg.Mod&(tea.ModCtrl|tea.ModAlt) != 0 || utf8.RuneCountInString(msg.Text) <= 1 {
		e.feed(msg)
		return
	}
	for _, r := range msg.Text {
		e.feed(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

// feed gives goeditor one msg, dropping its Cmds.
func (e *goEditor) feed(msg tea.Msg) {
	m, _ := e.m.Update(msg)
	e.m = m
}

// afterKey drains goeditor's signals: :w writes the solution file, :q leaves.
func (e *goEditor) afterKey(ctx *ui.Context) tea.Cmd {
	var cmds []tea.Cmd
	for _, s := range e.drain() {
		switch s := s.(type) {
		case core.SaveSignal:
			_, content := s.Value() // a path given to :w is ignored: only the solution file is written
			if err := os.WriteFile(e.path, []byte(content), 0o644); err != nil {
				cmds = append(cmds, ui.Toast(ctx.T.Leetcode.SaveFileFailed(), ui.ToastError))
			} else {
				cmds = append(cmds, ui.Toast(textwidth.Sanitize(ctx.T.Leetcode.Saved(filepath.Base(e.path))), ui.ToastInfo))
			}
		case core.QuitSignal:
			e.leave = true
		}
	}
	return tea.Batch(cmds...)
}

// drain empties goeditor's signal channel (it is buffered and never blocks
// a sender; nothing else reads it).
func (e *goEditor) drain() []core.Signal {
	ch := e.m.GetEditor().GetUpdateSignalChan()
	var out []core.Signal
	for {
		select {
		case s := <-ch:
			out = append(out, s)
		default:
			return out
		}
	}
}

func (e *goEditor) View(ctx *ui.Context, _ bool) string {
	e.applyAssist(ctx)
	w, h := e.size.W, e.size.H
	if w <= 0 || h <= 0 {
		return ""
	}
	th := themeOf(ctx)
	lines := strings.Split(e.m.View(), "\n")
	out := make([]string, h)
	for i := range out {
		l := ""
		if i < len(lines) {
			l = cleanStyled(lines[i])
		}
		out[i] = fitStyled(th, l, w)
	}
	return strings.Join(textwidth.Fit(out, w, h), "\n")
}

// cleanStyled keeps goeditor's colors (SGR) and the text, sanitized: any
// other escape sequence or control character in a line (from the file's
// content) is dropped, so the file cannot drive the terminal.
func cleanStyled(s string) string {
	var b strings.Builder
	var state byte
	for len(s) > 0 {
		seq, width, n, next := ansi.DecodeSequence(s, state, nil)
		if n <= 0 {
			break
		}
		state, s = next, s[n:]
		switch {
		case width > 0:
			b.WriteString(textwidth.Sanitize(seq))
		case ansi.HasCsiPrefix(seq) && strings.HasSuffix(seq, "m"):
			b.WriteString(seq)
		}
	}
	return b.String()
}
