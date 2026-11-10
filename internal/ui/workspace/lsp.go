package workspace

import (
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/lsp"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/codelsp"
)

// The language server behind the code cell's completion menu (v0.1.3-1):
// only the folder's owner, on this machine, talks to one (it runs in the
// folder); a share guest never does. Every open document of a language
// with a server is opened in it, each change (mine, a co-editor's, the
// disk's) follows, and closing the document or the tool closes it there.

// lspManager is the language server manager this surface may use (nil: a
// guest, no folder, or none in this process).
func (s *Surface) lspManager() codelsp.Manager {
	if s.guest != nil || s.docs == nil || s.root == nil || s.dir == "" {
		return nil
	}
	return codelsp.Get()
}

// lspPath is d's absolute path for the server.
func (s *Surface) lspPath(d *openDoc) string {
	return filepath.Join(s.dir, filepath.FromSlash(d.rel))
}

// lspWanted reports whether d should be open in a language server now: the
// owner, code completion switched on (the "code completion" setting turns
// the server off too) and a language that has one.
func (s *Surface) lspWanted(ctx *ui.Context, d *openDoc) bool {
	if ctx != nil && ctx.Config != nil && !ctx.Config.UI.CodeComplete {
		return false
	}
	return lsp.LangForPath(d.rel) != ""
}

// lspSync opens, updates or closes d in the language server as it stands.
func (s *Surface) lspSync(ctx *ui.Context, d *openDoc) {
	m := s.lspManager()
	if m == nil || s.closed || !s.lspWanted(ctx, d) {
		s.lspClose(d)
		return
	}
	path := s.lspPath(d)
	text := d.ed.Text()
	switch {
	case d.lsp == "":
		if err := m.Open(s.dir, path, text); err != nil {
			d.ed.SetServer(false, nil)
			return
		}
		d.lsp, d.lspText = path, text
	case text != d.lspText:
		m.Change(path, text)
		d.lspText = text
	}
	d.ed.SetServer(true, m.TriggerCharacters(path))
}

// lspChanged follows an edit to d (only when it is open in a server).
func (s *Surface) lspChanged(d *openDoc) {
	if d.lsp == "" {
		return
	}
	if m := s.lspManager(); m != nil {
		if text := d.ed.Text(); text != d.lspText {
			m.Change(d.lsp, text)
			d.lspText = text
		}
	}
}

// lspClose closes d in its server (if it was open in one).
func (s *Surface) lspClose(d *openDoc) {
	d.ed.SetServer(false, nil)
	if d.lsp == "" {
		return
	}
	if m := s.lspManager(); m != nil {
		m.Close(d.lsp)
	}
	d.lsp, d.lspText = "", ""
}

// lspKey is a document's key for the server's answers.
func (s *Surface) lspKey(d *openDoc) string { return s.id + "\n" + d.rel }

// lspAsk is the request d's last key made, as a Cmd (nil: none).
func (s *Surface) lspAsk(d *openDoc) tea.Cmd {
	r, ok := d.ed.TakeCompletionRequest()
	if !ok || d.lsp == "" {
		return nil
	}
	return codelsp.Complete(s.lspManager(), codelsp.Request{Key: s.lspKey(d), Seq: r.Seq,
		Path: d.lsp, Line: r.Line, Col: r.Col, LineText: r.LineText})
}

// lspResult hands a server's answer to its document (a stale one is
// dropped by the editor).
func (s *Surface) lspResult(m codelsp.ResultMsg) {
	id, rel, ok := strings.Cut(m.Key, "\n")
	if !ok || id != s.id {
		return
	}
	if d := s.code.find(rel); d != nil && d.lsp != "" {
		d.ed.SetServerItems(m.Seq, m.Items, m.Incomplete, m.Err)
	}
}

// lspCloseAll closes every document in the servers (the tool closes).
func (s *Surface) lspCloseAll() {
	for _, d := range s.code.docs {
		s.lspClose(d)
	}
}
