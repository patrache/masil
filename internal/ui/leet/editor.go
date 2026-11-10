package leet

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/leetcode"
	"github.com/patrache/masil/internal/termhost"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/termpane"
)

// The editor slot (A10.4): the user's nvim in a pane when panes work here
// and nvim is installed, else the built-in goeditor; E opens the same file
// in nvim (or $EDITOR) on a screen switch and reloads it afterwards. Run
// and submit always use the file as saved on disk.

// editorKind is which editor fills the slot.
type editorKind int

const (
	editorPane     editorKind = iota // nvim in a termpane
	editorGoeditor                   // the built-in goeditor
)

// chooseEditor: a pane needs both a pane-capable terminal and nvim.
func chooseEditor(paneOK bool, nvim string) editorKind {
	if paneOK && nvim != "" {
		return editorPane
	}
	return editorGoeditor
}

// codeEditor is what fills the editor slot.
type codeEditor interface {
	ui.Component
	ui.KeyHandler
	ui.InputCapturer
	ui.Cursorer
	Path() string
	Reload() error
}

// runRequestedMsg / submitRequestedMsg ask instance id to run / submit the
// saved solution (ctrl+\ r / s in the pane, :Run / :Submit, r / s).
type (
	runRequestedMsg    struct{ id string }
	submitRequestedMsg struct{ id string }
)

// execSuffix makes the instance id of an E screen switch (its ExitedMsg
// must not end the pane, which has the plain id).
const execSuffix = "#exec"

// Seams (export_test.go swaps them).
var (
	seamMu   sync.Mutex
	lookPath = exec.LookPath
	termEnv  = termhost.CurrentEnv
)

func findProgram(name string) string {
	seamMu.Lock()
	lp := lookPath
	seamMu.Unlock()
	p, err := lp(name)
	if err != nil {
		return ""
	}
	return p
}

func currentTermEnv(ctx *ui.Context) termhost.Env {
	seamMu.Lock()
	f := termEnv
	seamMu.Unlock()
	return f(ctx.Config, toolID, ctx.Mode == layout.ModeTiny)
}

// paneEditor is nvim in a pane.
type paneEditor struct {
	*termpane.Pane
	path string
}

var _ codeEditor = (*paneEditor)(nil)

func (p *paneEditor) Path() string        { return p.path }
func (p *paneEditor) Reload() error       { return nil } // nvim reads the file itself
func (p *paneEditor) CapturesInput() bool { return p.GrabsKeys() }

// solutionFile is the problem's solution file in the current language,
// created with LeetCode's starting code when it is not there yet.
func (in *instance) solutionFile(ctx *ui.Context) (string, error) {
	lang := currentLang(ctx)
	path, err := leetcode.SolutionPath(solutionDir(ctx), in.date, in.q.FrontendID, in.q.Slug, lang)
	if err != nil {
		return "", err
	}
	if _, err := leetcode.EnsureSolutionFile(path, in.q.Snippets[lang.Slug]); err != nil {
		return "", err
	}
	return path, nil
}

// canEdit: a problem with a body is loaded.
func (in *instance) canEdit() bool { return in.q != nil && in.phase == phaseReady }

// openEditor fills the slot (or focuses it when it is filled).
func (in *instance) openEditor(ctx *ui.Context) tea.Cmd {
	if !in.canEdit() {
		return nil
	}
	in.tab = tabCode
	if in.ed != nil {
		in.edFocus = true
		if p, ok := in.ed.(*paneEditor); ok && !p.GrabsKeys() {
			p.HandleKey(ctx, tea.KeyPressMsg{Code: tea.KeyEnter}) // back into nvim
		}
		return nil
	}
	path, err := in.solutionFile(ctx)
	if err != nil {
		return ui.Toast(ctx.T.Leetcode.FileFailed(), ui.ToastError)
	}
	var toast tea.Cmd
	mode, _ := termhost.Choose(termhost.Request{ToolID: toolID, Mode: termhost.ModePane}, currentTermEnv(ctx))
	if nvim := findProgram("nvim"); chooseEditor(mode == termhost.ModePane, nvim) == editorPane {
		id := in.id
		l, cmd, err := termpane.Launch(ctx, termhost.Request{Program: nvim, Args: []string{path}, Dir: filepath.Dir(path), Title: "nvim", ToolID: toolID, Mode: termhost.ModePane},
			termpane.Options{ToolID: toolID, InstanceID: id, OnPrefix: func(_ *ui.Context, key string) tea.Cmd {
				switch key {
				case "r":
					return emit(runRequestedMsg{id: id})
				case "s":
					return emit(submitRequestedMsg{id: id})
				}
				return nil
			}})
		switch {
		case err == nil && l.View != nil:
			in.ed, in.edFocus = &paneEditor{Pane: l.View, path: path}, true
			in.layoutEditor(ctx)
			return cmd
		case err == nil: // it went to a screen switch after all: that is the edit
			return cmd
		}
		toast = cmd // the pane limit: the built-in editor instead
	}
	ed, err := newGoEditor(path, themeOf(ctx), currentLang(ctx).Slug)
	if err != nil {
		return tea.Batch(toast, ui.Toast(ctx.T.Leetcode.EditorFailed(), ui.ToastError))
	}
	ed.(*goEditor).id = in.id
	in.ed, in.edFocus = ed, true
	in.layoutEditor(ctx)
	return toast
}

// execEdit opens the solution file in nvim (else $EDITOR, split on spaces,
// no shell) on a screen switch; its end reloads the slot's editor.
func (in *instance) execEdit(ctx *ui.Context) tea.Cmd {
	if !in.canEdit() {
		return nil
	}
	path, err := in.solutionFile(ctx)
	if err != nil {
		return ui.Toast(ctx.T.Leetcode.FileFailed(), ui.ToastError)
	}
	var argv []string
	if nvim := findProgram("nvim"); nvim != "" {
		argv = []string{nvim}
	} else {
		argv = strings.Fields(os.Getenv("EDITOR"))
	}
	if len(argv) == 0 {
		return ui.Toast(ctx.T.Leetcode.NoExternalEditor(), ui.ToastInfo)
	}
	req := termhost.Request{Program: argv[0], Args: append(slices.Clone(argv[1:]), path), Dir: filepath.Dir(path),
		Title: filepath.Base(argv[0]), ToolID: toolID, Mode: termhost.ModeExec}
	_, cmd, err := termpane.Launch(ctx, req, termpane.Options{ToolID: toolID, InstanceID: in.id + execSuffix})
	if err != nil {
		return tea.Batch(cmd, ui.Toast(ctx.T.Leetcode.EditorFailed(), ui.ToastError))
	}
	return cmd
}

// dropEditor empties the slot (a pane's nvim is ended).
func (in *instance) dropEditor() tea.Cmd {
	ed := in.ed
	in.ed, in.edFocus = nil, false
	if p, ok := ed.(*paneEditor); ok {
		h := p.Handle()
		return func() tea.Msg { _ = h.Close(); return nil }
	}
	return nil
}

// cycleLang is l: the next language (the slot then opens that file). The
// pick is the device's memory (tools-state, A8.5); config.toml is the
// settings' — only a [tools.leet] lang pin, which would hide the pick, is
// taken out of it.
func (in *instance) cycleLang(ctx *ui.Context) tea.Cmd {
	cur := currentLang(ctx)
	i := slices.IndexFunc(leetcode.Languages, func(l leetcode.Lang) bool { return l.Slug == cur.Slug })
	next := leetcode.Languages[(i+1)%len(leetcode.Languages)]
	st := loadState(ctx)
	st.Lang = next.Slug
	saveState(ctx, st)
	var unpin tea.Cmd
	if toolConfig(ctx).Lang != "" {
		unpin = setToolKey(ctx, "lang", nil)
	}
	return tea.Batch(unpin, emit(langChangedMsg{}))
}

// edRect is where the editor is drawn inside the instance (ok=false: not
// shown now).
func (in *instance) edRect(ctx *ui.Context) (x, y, w, h int, ok bool) {
	W, H := in.size.W, in.size.H
	sp := splitFor(ctx.Mode, in.zoomed, W)
	switch {
	case sp.Tiny:
		return 0, 0, W, H - 1, in.edFocus && H > 1
	case sp.Tabbed:
		return 0, 2, W, H - 3, in.tab == tabCode && H > 3
	}
	return sp.Left + 1, 1, sp.Right, H - 2, H > 2 && in.canEdit()
}

// layoutEditor gives the editor its size.
func (in *instance) layoutEditor(ctx *ui.Context) {
	if in.ed == nil {
		return
	}
	_, _, w, h, _ := in.edRect(ctx)
	if w > 0 && h > 0 {
		in.ed.Resize(ctx, ui.Size{W: w, H: h})
	}
}

// edVisible: the editor has focus and is on screen.
func (in *instance) edActive(ctx *ui.Context) bool {
	if in.ed == nil || !in.edFocus {
		return false
	}
	_, _, _, _, ok := in.edRect(ctx)
	return ok
}
