package apitool

import (
	"net/url"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/apitool"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
)

// The left tree (C3.6): the personal collections ("내 컬렉션 (로컬)") with
// their folders and requests, each request with its last result (✓/✕), and
// under it the recent history.

type rowKind int

const (
	rowGroup rowKind = iota
	rowCollection
	rowFolder
	rowRequest
	rowEmpty
)

// treeRow is one visible row of the tree.
type treeRow struct {
	kind   rowKind
	depth  int
	col    string // collection name
	folder string // folder id (rowFolder)
	req    string // request id (rowRequest)
	text   string
}

// treeView keeps the tree's expansion and cursor.
type treeView struct {
	open map[string]bool // collection name / "<col>\x00<folder id>" / groupKey
	cur  int
	top  int
	h    int
}

const groupKey = "\x00local"

func newTreeView() *treeView { return &treeView{open: map[string]bool{groupKey: true}} }

func (v *treeView) expand(key string, on bool) { v.open[key] = on }

func folderKey(col, id string) string { return col + "\x00" + id }

// rows lists the visible rows.
func (v *treeView) rows(ctx *ui.Context, t *Tool) []treeRow {
	a := ctx.T.Apitest
	var out []treeRow
	for _, id := range t.docOrder {
		sd := t.docs[id]
		if sd == nil || sd.col == nil {
			continue
		}
		key := sharedKey(id)
		out = append(out, treeRow{kind: rowCollection, col: key, text: a.TreeShared(sd.title)})
		if v.open[key] {
			out = v.folder(t, out, sd.col, key, &sd.col.Root, 1)
		}
	}
	out = append(out, treeRow{kind: rowGroup, text: a.TreeLocal()})
	if !v.open[groupKey] {
		return out
	}
	if len(t.names) == 0 {
		return append(out, treeRow{kind: rowEmpty, depth: 1, text: a.TreeEmpty()})
	}
	for _, n := range t.names {
		out = append(out, treeRow{kind: rowCollection, depth: 1, col: n, text: n})
		if !v.open[n] {
			continue
		}
		if c := t.collection(n); c != nil {
			out = v.folder(t, out, c, n, &c.Root, 2)
		}
	}
	return out
}

func (v *treeView) folder(t *Tool, out []treeRow, c *apitool.Collection, col string, f *apitool.Folder, depth int) []treeRow {
	for _, it := range f.Items {
		switch {
		case it.Folder != nil:
			out = append(out, treeRow{kind: rowFolder, depth: depth, col: col, folder: it.Folder.ID, text: it.Folder.Name})
			if v.open[folderKey(col, it.Folder.ID)] {
				out = v.folder(t, out, c, col, it.Folder, depth+1)
			}
		case it.Request != "":
			r := c.Requests[it.Request]
			if r == nil {
				continue
			}
			out = append(out, treeRow{kind: rowRequest, depth: depth, col: col, req: r.ID, text: requestLabel(r)})
		}
	}
	return out
}

// requestLabel is "METHOD name" (the name, else the URL template).
func requestLabel(r *apitool.Request) string {
	name := r.Name
	if name == "" {
		name = shortPath(r.URL)
	}
	m := strings.ToUpper(r.Method)
	if m == "" {
		m = "GET"
	}
	return textwidth.PadRight(m, 4) + " " + name
}

// follow moves the cursor to the edited request.
func (v *treeView) follow(ctx *ui.Context, t *Tool) {
	if t.req == nil {
		return
	}
	for i, r := range v.rows(ctx, t) {
		if r.kind == rowRequest && r.col == t.colName && r.req == t.req.ID {
			v.cur = i
			return
		}
	}
}

func (v *treeView) view(ctx *ui.Context, t *Tool, w, h int, focused bool) []string {
	v.h = h
	rows := v.rows(ctx, t)
	v.cur = min(v.cur, max(len(rows)-1, 0))
	if v.cur < v.top {
		v.top = v.cur
	}
	if h > 0 && v.cur >= v.top+h {
		v.top = v.cur - h + 1
	}
	var out []string
	for i := v.top; i < len(rows) && len(out) < h; i++ {
		r := rows[i]
		ind := strings.Repeat("  ", r.depth)
		mark := "  "
		switch r.kind {
		case rowGroup, rowCollection:
			mark = "▸ "
			if (r.kind == rowGroup && v.open[groupKey]) || (r.kind == rowCollection && v.open[r.col]) {
				mark = "▾ "
			}
		case rowFolder:
			mark = "▸ "
			if v.open[folderKey(r.col, r.folder)] {
				mark = "▾ "
			}
		}
		suffix := ""
		if r.kind == rowRequest {
			if pass, ok := t.results[r.col+"/"+r.req]; ok {
				suffix = "  ✕"
				if pass {
					suffix = "  ✓"
				}
			}
		}
		text := textwidth.Sanitize(r.text)
		room := max(w-textwidth.Width(ind+mark)-textwidth.Width(suffix), 1)
		line := ind + mark + textwidth.Truncate(text, room, "…")
		if suffix != "" {
			line = textwidth.PadRight(line, w-textwidth.Width(suffix)) + suffix
		}
		line = textwidth.Truncate(line, w, "…")
		if i == v.cur && focused {
			line = selected(ctx.Theme, textwidth.PadRight(line, w))
		} else if r.kind == rowEmpty {
			line = dim(ctx.Theme, line)
		}
		out = append(out, line)
	}
	return textwidth.Fit(out, w, h)
}

// historyView is "─ 기록 ─" and the recent rows "15:04 GET /user 200".
func (t *Tool) historyView(ctx *ui.Context, w, h int) []string {
	if h <= 0 {
		return nil
	}
	th := ctx.Theme
	head := "─ " + ctx.T.Apitest.TreeHistory() + " "
	head += strings.Repeat("─", max(w-textwidth.Width(head), 0))
	out := []string{dim(th, textwidth.Truncate(head, w, ""))}
	for _, e := range t.recent {
		if len(out) >= h {
			break
		}
		at := e.At
		if ctx.Loc != nil {
			at = at.In(ctx.Loc)
		}
		line := at.Format("15:04") + " "
		if e.Kind == "run" && e.Run != nil {
			line += "▶ " + e.Run.Collection
		} else {
			line += methodOf(&e.Request) + " " + urlPath(e.URL)
		}
		status := ""
		if e.Status > 0 {
			status = " " + strconv.Itoa(e.Status)
		}
		line = textwidth.Sanitize(line)
		line = textwidth.Truncate(line, max(w-textwidth.Width(status), 1), "…")
		out = append(out, textwidth.PadRight(line, w-textwidth.Width(status))+status)
	}
	return textwidth.Fit(out, w, h)
}

// urlPath is the path of an expanded URL ("" -> "/").
func urlPath(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Path == "" {
		return "/"
	}
	return u.Path
}

// handleKey is the tree's keys.
func (v *treeView) handleKey(ctx *ui.Context, t *Tool, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	rows := v.rows(ctx, t)
	switch msg.String() {
	case "v":
		if v.cur < len(rows) {
			if id, ok := docOfKey(rows[v.cur].col); ok {
				return true, t.openVersions(ctx, id)
			}
		}
		if sd := t.curShared(); sd != nil {
			return true, t.openVersions(ctx, sd.id)
		}
		return false, nil
	case "j", "down":
		v.cur = min(v.cur+1, max(len(rows)-1, 0))
	case "k", "up":
		v.cur = max(v.cur-1, 0)
	case "enter", "space", "l", "h", "right", "left":
		if v.cur >= len(rows) {
			return true, nil
		}
		r := rows[v.cur]
		k := msg.String()
		switch r.kind {
		case rowGroup:
			v.open[groupKey] = toggleFor(k, v.open[groupKey])
		case rowCollection:
			v.open[r.col] = toggleFor(k, v.open[r.col])
		case rowFolder:
			fk := folderKey(r.col, r.folder)
			v.open[fk] = toggleFor(k, v.open[fk])
		case rowRequest:
			if k == "h" || k == "left" {
				return true, nil
			}
			t.selectRequest(r.col, r.req)
			t.focus = paneRequest
			t.save(ctx)
		}
	default:
		return false, nil
	}
	return true, nil
}

// toggleFor is the new open state for key k (l opens, h closes, others flip).
func toggleFor(k string, open bool) bool {
	switch k {
	case "l", "right":
		return true
	case "h", "left":
		return false
	}
	return !open
}

// click selects the row at y of the tree.
func (v *treeView) click(ctx *ui.Context, t *Tool, y int) {
	i := v.top + y
	if y < 0 || i >= len(v.rows(ctx, t)) || y >= v.h {
		return
	}
	v.cur = i
}
