package leet

import (
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/ionut-t/goeditor/core"

	"github.com/patrache/masil/internal/codeassist"
	"github.com/patrache/masil/internal/lsp"
	"github.com/patrache/masil/internal/ui/codelsp"
)

// The built-in editor's language server (v0.1.3-1): the solution file is
// opened in one for its language (lsp.LangForLeetCode), rooted in the
// solution's folder; its items come first in the completion menu. The
// solution is my own local file: no one else edits it here.

// lspState is the editor's side of the server.
type lspState struct {
	m       codelsp.Manager // nil: none (a test binary, or not this language)
	lang    string          // the server's language ("" = none for this slug)
	open    bool
	path    string // the file's absolute path, as open in the server
	text    string // what the server has
	trigger []string
	seq     int              // the latest request; older answers are dropped
	want    *codelsp.Request // asked, not yet sent
	asked   wordAnchor       // where the latest request was made
	items   []codeassist.Item
	at      wordAnchor // where the answer in items was asked
	ok      bool
	more    bool // isIncomplete: ask again as the word grows
	// triggered: a trigger character (".") opened the menu with no word yet
	triggered bool
	// menu is the menu shown, in goeditor's order (Meta "i" indexes it).
	menu []codeassist.Item
}

// wordAnchor is where a request was made: the cursor's line, where the
// word being typed starts, the line before it and the word so far.
type wordAnchor struct {
	row, from    int
	head, prefix string
}

// setLSPLang follows the language (a slug) for the server.
func (e *goEditor) setLSPLang(slug string) {
	if l := lsp.LangForLeetCode(slug); l != e.srv.lang {
		e.lspClose()
		e.srv.lang = l
	}
}

// here is the cursor's anchor now.
func (e *goEditor) here() wordAnchor {
	b := e.m.GetEditor().GetBuffer()
	pos := b.GetCursor().Position
	line := b.GetLineRunes(pos.Row)
	prefix := codeassist.Prefix(line, pos.Col)
	from := max(0, pos.Col-len([]rune(prefix)))
	return wordAnchor{row: pos.Row, from: from, head: string(line[:min(from, len(line))]), prefix: prefix}
}

func (e *goEditor) isAt(a wordAnchor) bool {
	h := e.here()
	return h.row == a.row && h.from == a.from && h.head == a.head
}

// lspOn reports whether the server serves the file now.
func (e *goEditor) lspOn() bool { return e.srv.open && e.cpOn }

// lspSync opens, updates or closes the file in the server as it stands
// (completion off turns the server off too).
func (e *goEditor) lspSync() {
	if e.srv.m == nil || e.srv.lang == "" || !e.cpOn {
		e.lspClose()
		return
	}
	text := e.m.GetCurrentContent()
	switch {
	case !e.srv.open:
		path, err := filepath.Abs(e.path)
		if err != nil {
			return
		}
		if err := e.srv.m.Open(filepath.Dir(path), path, text); err != nil {
			return
		}
		e.srv.open, e.srv.path, e.srv.text = true, path, text
	case text != e.srv.text:
		e.srv.m.Change(e.srv.path, text)
		e.srv.text = text
	}
	e.srv.trigger = e.srv.m.TriggerCharacters(e.srv.path)
}

// lspClose closes the file in the server (if it is open there).
func (e *goEditor) lspClose() {
	if e.srv.open && e.srv.m != nil {
		e.srv.m.Close(e.srv.path)
	}
	e.srv.open, e.srv.text, e.srv.ok, e.srv.items, e.srv.want = false, "", false, nil, nil
	e.srv.seq++
}

// lspForget drops the request in flight (the menu closed for good).
func (e *goEditor) lspForget() {
	e.srv.triggered = false
	if e.srv.want != nil || e.srv.open {
		e.srv.seq++
		e.srv.want = nil
	}
}

// lspAsk makes a request for the word being typed (an answer still in
// force and complete is filtered instead).
func (e *goEditor) lspAsk() {
	if !e.lspOn() || !e.m.IsInsertMode() {
		return
	}
	h := e.here()
	if !e.forced && !e.srv.triggered && len([]rune(h.prefix)) < codeassist.MinPrefix {
		return
	}
	if e.srv.ok && !e.srv.more && e.isAt(e.srv.at) && strings.HasPrefix(h.prefix, e.srv.at.prefix) {
		return
	}
	b := e.m.GetEditor().GetBuffer()
	pos := b.GetCursor().Position
	e.srv.seq++
	e.srv.asked = h
	e.srv.want = &codelsp.Request{Key: e.lspKey(), Seq: e.srv.seq, Path: e.srv.path,
		Line: pos.Row, Col: pos.Col, LineText: append([]rune(nil), b.GetLineRunes(pos.Row)...)}
}

func (e *goEditor) lspKey() string { return e.id + "\n" + e.srv.path }

// lspCmd sends the request the last key made (nil: none).
func (e *goEditor) lspCmd() tea.Cmd {
	w := e.srv.want
	e.srv.want = nil
	if w == nil || !e.srv.open {
		return nil
	}
	return codelsp.Complete(e.srv.m, *w)
}

// lspResult takes an answer: one for an older request or a cursor that
// moved on is dropped; an error leaves the words.
func (e *goEditor) lspResult(m codelsp.ResultMsg) {
	if m.Key != e.lspKey() || m.Seq != e.srv.seq || !e.lspOn() || !e.m.IsInsertMode() || !e.isAt(e.srv.asked) {
		return
	}
	items, more := m.Items, m.Incomplete
	if m.Err != nil {
		items, more = nil, false
	}
	e.srv.items, e.srv.at, e.srv.ok, e.srv.more = items, e.srv.asked, m.Err == nil, more
	e.refreshCompletion()
}

// serverItems is the answer for the word being typed (filtered when the
// word grew since), nil when it no longer applies.
func (e *goEditor) serverItems(prefix string) []codeassist.Item {
	if !e.lspOn() || !e.srv.ok || !e.isAt(e.srv.at) {
		return nil
	}
	if prefix == e.srv.at.prefix {
		return e.srv.items
	}
	return codeassist.FilterPrefix(e.srv.items, prefix)
}

// completions is items as goeditor's menu (the detail as its type).
func (e *goEditor) completions(items []codeassist.Item) []core.Completion {
	e.srv.menu = items
	cs := make([]core.Completion, len(items))
	for i, it := range items {
		cs[i] = core.Completion{Text: it.Text, Label: it.Label, Type: it.Detail, Meta: map[string]any{"i": i}}
	}
	return cs
}

// selectedEdit is the selected item when it carries a server's own range.
func (e *goEditor) selectedEdit() (codeassist.Item, bool) {
	c, ok := e.m.SelectedCompletion()
	if !ok {
		return codeassist.Item{}, false
	}
	i, ok := c.Meta["i"].(int)
	if !ok || i < 0 || i >= len(e.srv.menu) || !e.srv.menu[i].Edit {
		return codeassist.Item{}, false
	}
	return e.srv.menu[i], true
}

// applyEdit replaces the item's range with its text as typed keys (so
// goeditor's undo sees ordinary typing).
func (e *goEditor) applyEdit(it codeassist.Item) {
	e.closeCompletion()
	b := e.m.GetEditor().GetBuffer()
	pos := b.GetCursor().Position
	from, to := it.Span(b.GetLineRunes(pos.Row), pos.Col)
	for range to - pos.Col {
		e.feed(tea.KeyPressMsg{Code: tea.KeyRight})
	}
	for range to - from {
		e.feed(tea.KeyPressMsg{Code: tea.KeyBackspace})
	}
	for _, r := range it.Text {
		if r == '\n' {
			e.feed(tea.KeyPressMsg{Code: tea.KeyEnter})
			continue
		}
		e.feed(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}
