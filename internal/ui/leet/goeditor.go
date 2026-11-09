package leet

import (
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/ionut-t/goeditor/core"

	"github.com/patrache/masil/internal/textwidth"
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
}

var _ codeEditor = (*goEditor)(nil)

// newGoEditor opens path (it must exist) in a goeditor buffer, colored for
// a dark or light background as masil's theme is (goeditor would ask the
// terminal itself, racing Bubble Tea for its input: third_party/goeditor).
func newGoEditor(path string, dark bool) (codeEditor, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	m := goeditor.New(80, 20, goeditor.WithDarkBackground(dark))
	m.SetContent(string(b))
	m.Focus()
	e := &goEditor{m: m, path: path}
	e.drain()
	return e, nil
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

// Cursor: goeditor draws its own cursor cell.
func (e *goEditor) Cursor() *tea.Cursor { return nil }

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
	e.feed(msg)
	cmd := e.afterKey(ctx)
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
