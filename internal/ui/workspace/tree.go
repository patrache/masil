package workspace

import (
	"path"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/workspace/fsroot"
)

// The tree cell (spec B6.2): one level is listed when a folder opens; secret
// files show 🔒 and "(공유 안 됨)", ignored entries are dim and never
// expanded on their own.

type treeNode struct {
	name, rel       string
	dir             bool
	denied, ignored bool
	depth           int
	open, loaded    bool
	children        []*treeNode
	more            int // entries past fsroot.MaxDirEntries
	moreRow, errRow bool
	err             error
}

type treeCell struct {
	root   *treeNode
	rows   []*treeNode
	sel    int
	scroll int
	h      int
}

// load lists the folder's first level.
func (c *treeCell) load(r *fsroot.Root) {
	c.root = &treeNode{rel: ".", dir: true, open: true, depth: -1}
	c.expand(r, c.root)
	c.flatten()
}

// expand lists n's children (once).
func (c *treeCell) expand(r *fsroot.Root, n *treeNode) {
	n.open = true
	if n.loaded {
		return
	}
	n.loaded = true
	entries, more, err := r.List(n.rel, false)
	if err != nil {
		n.children = []*treeNode{{errRow: true, err: err, depth: n.depth + 1}}
		return
	}
	n.children = n.children[:0]
	for _, e := range entries {
		rel := e.Name
		if n.rel != "." {
			rel = path.Join(n.rel, e.Name)
		}
		n.children = append(n.children, &treeNode{name: e.Name, rel: rel, dir: e.Dir, denied: e.Denied, ignored: e.Ignored, depth: n.depth + 1})
	}
	if more > 0 {
		n.children = append(n.children, &treeNode{moreRow: true, more: more, depth: n.depth + 1})
	}
}

// flatten rebuilds the visible rows.
func (c *treeCell) flatten() {
	c.rows = c.rows[:0]
	var walk func(n *treeNode)
	walk = func(n *treeNode) {
		for _, ch := range n.children {
			c.rows = append(c.rows, ch)
			if ch.dir && ch.open {
				walk(ch)
			}
		}
	}
	if c.root != nil {
		walk(c.root)
	}
	c.sel = max(0, min(c.sel, len(c.rows)-1))
}

func (c *treeCell) selected() *treeNode {
	if c.sel < 0 || c.sel >= len(c.rows) {
		return nil
	}
	return c.rows[c.sel]
}

// parent is the row index of n's folder (-1 at the top level).
func (c *treeCell) parent(i int) int {
	d := c.rows[i].depth
	for j := i - 1; j >= 0; j-- {
		if c.rows[j].depth < d {
			return j
		}
	}
	return -1
}

// treeKey handles a key in the tree cell.
func (s *Surface) treeKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	c := s.tree
	n := c.selected()
	switch msg.String() {
	case "j", "down":
		c.sel = min(c.sel+1, len(c.rows)-1)
	case "k", "up":
		c.sel = max(c.sel-1, 0)
	case "g", "home":
		c.sel = 0
	case "G", "end":
		c.sel = len(c.rows) - 1
	case "pgdown", "ctrl+d":
		c.sel = min(c.sel+max(c.h/2, 1), len(c.rows)-1)
	case "pgup", "ctrl+u":
		c.sel = max(c.sel-max(c.h/2, 1), 0)
	case "enter", "l", "right", "space":
		if n == nil || n.moreRow || n.errRow {
			return true, nil
		}
		if n.dir {
			if n.open && msg.String() == "enter" {
				n.open = false
			} else {
				c.expand(s.root, n)
			}
			c.flatten()
			return true, nil
		}
		if msg.String() == "l" || msg.String() == "right" {
			return true, nil
		}
		return true, s.openDoc(ctx, n.rel)
	case "h", "left":
		if n == nil {
			return true, nil
		}
		if n.dir && n.open {
			n.open = false
			c.flatten()
		} else if p := c.parent(c.sel); p >= 0 {
			c.sel = p
		}
	case "n":
		s.openNewTerm(ctx, s.termMode())
	default:
		return false, nil
	}
	return true, nil
}

// mouse: a click selects a row; a second click on it opens it.
func (c *treeCell) mouse(ctx *ui.Context, s *Surface, msg tea.MouseMsg, y int) tea.Cmd {
	switch m := msg.(type) {
	case tea.MouseClickMsg:
		i := c.scroll + y
		if i < 0 || i >= len(c.rows) || m.Button != tea.MouseLeft {
			return nil
		}
		if i == c.sel {
			_, cmd := s.treeKey(ctx, tea.KeyPressMsg{Code: tea.KeyEnter})
			return cmd
		}
		c.sel = i
	case tea.MouseWheelMsg:
		switch m.Button {
		case tea.MouseWheelUp:
			c.sel = max(c.sel-3, 0)
		case tea.MouseWheelDown:
			c.sel = min(c.sel+3, len(c.rows)-1)
		}
	}
	return nil
}

// label is a row's text (sanitized).
func (n *treeNode) label(ctx *ui.Context) string {
	t := ctx.T.Workspace
	switch {
	case n.moreRow:
		return "  " + t.TreeMoreEntries(n.more)
	case n.errRow:
		return "  " + t.OpenFailed()
	}
	name := textwidth.Sanitize(n.name)
	switch {
	case n.denied:
		return t.TreeSecretHidden(name)
	case n.dir && n.open:
		return "▾ " + name + "/"
	case n.dir:
		return "▸ " + name + "/"
	}
	return "  " + name
}

// view draws the tree in w x h (its right column is the separator).
func (c *treeCell) view(ctx *ui.Context, s *Surface, w, h int, focused bool) []string {
	th := ctx.Theme
	c.h = h
	if c.sel < c.scroll {
		c.scroll = c.sel
	}
	if c.sel >= c.scroll+h {
		c.scroll = c.sel - h + 1
	}
	c.scroll = max(0, min(c.scroll, max(0, len(c.rows)-h)))
	inner := max(w-1, 0)
	sep := lipgloss.NewStyle().Foreground(th.P.Line).Render("│")
	if s.plan.Single == PaneTree { // the widget shows the tree alone
		inner, sep = w, ""
	}
	dim := lipgloss.NewStyle().Foreground(th.P.Dim)
	out := make([]string, 0, h)
	for i := c.scroll; i < len(c.rows) && len(out) < h; i++ {
		n := c.rows[i]
		text := strings.Repeat("  ", max(n.depth, 0)) + n.label(ctx)
		text = textwidth.Truncate(text, inner, "…")
		switch {
		case i == c.sel && focused:
			text = th.S.Selected.Render(textwidth.PadRight(text, inner))
		case i == c.sel:
			text = th.S.Cursorline.Render(textwidth.PadRight(text, inner))
		case n.ignored || n.moreRow || n.errRow:
			text = dim.Render(text)
		case n.denied:
			text = th.S.Warning.Render(text)
		}
		out = append(out, textwidth.PadRight(text, inner)+sep)
	}
	for len(out) < h {
		out = append(out, strings.Repeat(" ", inner)+sep)
	}
	return out
}
