// Package codelsp feeds the built-in code editors' completion menus from a
// language server (v0.1.3-1, owner 16:41 "lsp 수준의 추천이 있어야 한다"):
// asynchronous requests as tea.Cmds that never block the UI, the answer
// turned into codeassist menu items (labels, a short detail, text edits in
// rune columns). The editors keep their word items and merge these first
// (codeassist.Merge); without a server they show words only.
//
// Only a folder's local owner may use it (internal/lsp starts a process in
// that folder): the workspace never calls it for a share guest.
package codelsp

import (
	"cmp"
	"context"
	"slices"
	"strings"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/codeassist"
	"github.com/patrache/masil/internal/lsp"
	"github.com/patrache/masil/internal/textwidth"
)

// Manager is what the editors use of internal/lsp's Manager (a fake in
// tests). Open, Change, Close and TriggerCharacters return at once;
// Complete waits (call it from a tea.Cmd: Complete does).
type Manager interface {
	Open(root, path, text string) error
	Change(path, text string)
	Close(path string)
	Complete(ctx context.Context, path string, line, utf16col int) (lsp.List, error)
	TriggerCharacters(path string) []string
}

// shared adapts the process-wide lsp.Manager.
type shared struct{ m *lsp.Manager }

func (s shared) Open(root, path, text string) error { return s.m.Open(root, path, text) }
func (s shared) Change(path, text string)           { s.m.Change(path, text) }
func (s shared) Close(path string)                  { s.m.Close(path) }
func (s shared) Complete(ctx context.Context, path string, line, col int) (lsp.List, error) {
	return s.m.Complete(ctx, path, line, col)
}
func (s shared) TriggerCharacters(path string) []string {
	if c := s.m.Client(path); c != nil {
		return c.TriggerCharacters()
	}
	return nil
}

var (
	mgrMu  sync.Mutex
	mgrSet bool
	mgr    Manager
)

// Get is the editors' Manager: lsp.Shared(). Inside a test binary it is nil
// (no language server) unless a test installed one (SetManagerForTest), so
// no test starts a real server — or waits for one — by accident.
func Get() Manager {
	mgrMu.Lock()
	defer mgrMu.Unlock()
	if mgrSet {
		return mgr
	}
	if testing.Testing() {
		return nil
	}
	return shared{lsp.Shared()}
}

// Timeout bounds one request; a slower answer leaves the word items.
const Timeout = lsp.CompletionTimeout

// Request is one completion request from an editor.
type Request struct {
	Key      string // the editor's own key: the answer goes back with it
	Seq      int    // the editor's request number: an older answer is dropped
	Path     string // the open document (absolute)
	Line     int    // the cursor's line (0-based)
	Col      int    // the cursor's rune column
	LineText []rune // the cursor's line as asked
}

// ResultMsg is a request's answer (Err set: none — keep the words).
type ResultMsg struct {
	Key        string
	Seq        int
	Items      []codeassist.Item
	Incomplete bool // ask again as the word grows (the server said isIncomplete)
	Err        error
}

// Complete asks m for completions at r in a Cmd (at most Timeout).
func Complete(m Manager, r Request) tea.Cmd {
	if m == nil {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), Timeout)
		defer cancel()
		l, err := m.Complete(ctx, r.Path, r.Line, lsp.UTF16Col(string(r.LineText), r.Col))
		if err == nil && ctx.Err() != nil {
			err = ctx.Err()
		}
		if err != nil {
			return ResultMsg{Key: r.Key, Seq: r.Seq, Err: err}
		}
		items, cut := Items(l, r.LineText, r.Line, r.Col)
		// A cut answer is incomplete too: the editors filter a complete one
		// as the word grows, and what sorted past the cut would never show.
		return ResultMsg{Key: r.Key, Seq: r.Seq, Items: items, Incomplete: l.Incomplete || cut}
	}
}

// IsTrigger reports whether text, one typed key, is one of the server's
// trigger characters (".", ":", ">" …).
func IsTrigger(trigger []string, text string) bool {
	return text != "" && slices.Contains(trigger, text)
}

// detailWidth is how wide a menu item's detail may be.
const detailWidth = 28

// Items turns a server's answer at (row, col) of line into menu items:
// ordered by SortText, then FilterText (Label when empty); a text edit on
// the cursor's line that spans the cursor keeps its range (UTF-16 columns
// to rune columns), any other edit inserts its text as a plain item.
// cut reports that items were left out past codeassist.MaxMenuItems.
func Items(l lsp.List, line []rune, row, col int) (out []codeassist.Item, cut bool) {
	src := slices.Clone(l.Items)
	key := func(it lsp.Item) (string, string) {
		f := cmp.Or(it.FilterText, it.Label)
		return cmp.Or(it.SortText, f), f
	}
	slices.SortStableFunc(src, func(a, b lsp.Item) int {
		as, af := key(a)
		bs, bf := key(b)
		return cmp.Or(strings.Compare(as, bs), strings.Compare(af, bf))
	})
	text := string(line)
	out = make([]codeassist.Item, 0, min(len(src), codeassist.MaxMenuItems))
	for _, it := range src {
		label := textwidth.Sanitize(it.Label)
		ins := it.InsertText
		if ins == "" {
			ins = it.Label
		}
		ins = textwidth.SanitizeInput(ins)
		if label == "" || ins == "" {
			continue
		}
		if len(out) == codeassist.MaxMenuItems {
			return out, true
		}
		item := codeassist.Item{Label: label, Detail: short(it.Detail, it.Kind), Text: ins,
			Filter: textwidth.Sanitize(it.FilterText), Server: true}
		if e := it.Edit; e != nil && e.Range.Start.Line == row && e.Range.End.Line == row {
			from, to := lsp.RuneCol(text, e.Range.Start.Character), lsp.RuneCol(text, e.Range.End.Character)
			if from <= col && col <= to {
				item.Edit, item.From, item.After = true, from, to-col
			}
		}
		out = append(out, item)
	}
	return out, false
}

// short is a menu item's detail: the first line of the server's detail,
// cut to detailWidth cells, else the item's kind ("function").
func short(detail string, kind lsp.CompletionItemKind) string {
	d := strings.TrimSpace(detail)
	if i := strings.IndexAny(d, "\r\n"); i >= 0 {
		d = strings.TrimSpace(d[:i])
	}
	d = strings.Join(strings.Fields(textwidth.Sanitize(d)), " ")
	if d == "" {
		return kind.String()
	}
	return textwidth.Truncate(d, detailWidth, "…")
}
