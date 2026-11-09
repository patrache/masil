package arch

import (
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/archdoc"
	"github.com/patrache/masil/internal/archexport"
	"github.com/patrache/masil/internal/archsim"
	"github.com/patrache/masil/internal/archsim/pricing"
	"github.com/patrache/masil/internal/clipboard"
	"github.com/patrache/masil/internal/config"
	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/widgets"
)

// exportKind is one export format.
type exportKind int

const (
	exportMermaid exportKind = iota
	exportD2
	exportTerraform
	exportText80
	exportText120
	exportJSON
)

// exportFormats are the formats in the order the dialog lists them.
var exportFormats = []struct {
	kind  exportKind
	ext   string
	label func(i18n.ArchcanvasT) string
}{
	{exportMermaid, ".mmd", i18n.ArchcanvasT.ExportMermaid},
	{exportD2, ".d2", i18n.ArchcanvasT.ExportD2},
	{exportTerraform, ".tf", i18n.ArchcanvasT.ExportTerraform},
	{exportText80, ".txt", i18n.ArchcanvasT.ExportText80},
	{exportText120, ".txt", i18n.ArchcanvasT.ExportText120},
	{exportJSON, ".json", i18n.ArchcanvasT.ExportJson},
}

// exportDialog is arch.export (P3 Task 20): pick a format, then "파일로
// 저장" (export_dir/<SafeName(title)><ext>, asking before it overwrites) or
// "클립보드" (ui.Copy; past its limit it says to save a file instead).
type exportDialog struct {
	design *archdoc.Design
	result *archsim.Result
	title  string // the design's title on screen (the file name comes from it)
	cur    int    // the format
	step   int    // 0 = format, 1 = target
	target int    // 0 = file, 1 = clipboard
	hint   string
	size   ui.Size
}

var (
	_ ui.Overlay    = (*exportDialog)(nil)
	_ ui.KeyHandler = (*exportDialog)(nil)
	_ ui.Titled     = (*exportDialog)(nil)
)

// exportDoneMsg reports a finished file export of dialog dlg.
type exportDoneMsg struct {
	dlg  *exportDialog
	path string
	err  error
}

// newExportDialog exports the design as it is now.
func (m *canvasModel) newExportDialog(ctx *ui.Context) *exportDialog {
	return &exportDialog{design: m.design, result: m.result, title: m.title(ctx)}
}

func (d *exportDialog) Init(*ui.Context) tea.Cmd            { return nil }
func (d *exportDialog) Resize(_ *ui.Context, size ui.Size)  { d.size = size }
func (d *exportDialog) Modal() bool                         { return true }
func (d *exportDialog) Bounds(body layout.Rect) layout.Rect { return centeredRect(body, 64, 14) }
func (d *exportDialog) Title(ctx *ui.Context) (string, ui.Badge) {
	return ctx.T.Archcanvas.ExportTitle(), ui.Badge{}
}

func (d *exportDialog) Update(ctx *ui.Context, msg tea.Msg) tea.Cmd {
	m, ok := msg.(exportDoneMsg)
	if !ok || m.dlg != d {
		return nil
	}
	if m.err != nil {
		d.hint = ctx.T.Archcanvas.ExportSaveFailed()
		return nil
	}
	return tea.Batch(closeOverlay, ui.Toast(ctx.T.Archcanvas.ExportSaved(shortPath(m.path)), ui.ToastSuccess))
}

// snapText is the catalog text of a text snapshot.
func snapText(t *i18n.T) archexport.SnapshotText {
	a := t.Archcanvas
	return archexport.SnapshotText{
		Columns:  [6]string{a.SnapColName(), a.SnapColKind(), a.SnapColInstances(), a.SnapColUtil(), a.SnapColP99(), a.SnapColCost()},
		Kind:     func(k archdoc.NodeKind) string { return kindName(t, k) },
		CostNote: a.SnapCostNote(pricing.AsOf()),
	}
}

// exportBytes is design d in format k.
func exportBytes(t *i18n.T, d *archdoc.Design, r *archsim.Result, k exportKind) []byte {
	a := t.Archcanvas
	switch k {
	case exportMermaid:
		return archexport.Mermaid(d, archexport.Options{})
	case exportD2:
		return archexport.D2(d, archexport.Options{})
	case exportTerraform:
		return archexport.Terraform(d, archexport.Options{Header: []string{a.TfHeader(), a.EstimateDisclaimer()}})
	case exportText80:
		return archexport.Snapshot(d, r, 80, snapText(t))
	case exportText120:
		return archexport.Snapshot(d, r, 120, snapText(t))
	}
	b, err := d.Encode()
	if err != nil {
		return nil
	}
	return b
}

// exportDir is where exports are saved: [tools.arch] export_dir, else
// ~/masil/arch-exports.
func exportDir(ctx *ui.Context) (string, error) {
	v := ""
	if ctx != nil && ctx.Config != nil {
		v = strings.TrimSpace(ctx.Config.ToolString(ui.ToolArch, "export_dir", ""))
	}
	if v != "" {
		return expandHome(v), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "masil", "arch-exports"), nil
}

// shortPath shows a path under the home folder as "~/…".
func shortPath(p string) string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if rel, err := filepath.Rel(home, p); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
			return "~" + string(filepath.Separator) + rel
		}
	}
	return p
}

// fileName is the export's file name: SafeName(title) + ext.
func (d *exportDialog) fileName(ext string) string {
	n := fileSafe(d.title)
	if n == "" {
		n = "arch"
	}
	return n + ext
}

// path is where format i is saved ("" when there is no export folder).
func (d *exportDialog) path(ctx *ui.Context) string {
	dir, err := exportDir(ctx)
	if err != nil {
		return ""
	}
	return filepath.Join(dir, d.fileName(exportFormats[d.cur].ext))
}

// writeExport writes data to path (creating the folder), atomically.
func writeExport(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return config.WriteFileAtomic(path, data, 0o644)
}

func (d *exportDialog) HandleKey(ctx *ui.Context, k tea.KeyPressMsg) (bool, tea.Cmd) {
	switch k.String() {
	case "j", "down":
		if d.step == 0 {
			d.cur = min(d.cur+1, len(exportFormats)-1)
		} else {
			d.target = 1
		}
	case "k", "up":
		if d.step == 0 {
			d.cur = max(d.cur-1, 0)
		} else {
			d.target = 0
		}
	case "backspace", "left", "h":
		d.step, d.hint = 0, ""
	case "enter":
		if d.step == 0 {
			d.step, d.target, d.hint = 1, 0, ""
			return true, nil
		}
		return true, d.run(ctx)
	default:
		return false, nil
	}
	return true, nil
}

// run exports the picked format to the picked target.
func (d *exportDialog) run(ctx *ui.Context) tea.Cmd {
	a := ctx.T.Archcanvas
	data := exportBytes(ctx.T, d.design, d.result, exportFormats[d.cur].kind)
	if d.target == 1 {
		if len(data) > clipboard.MaxBytes {
			d.hint = a.ExportUseFile()
			return nil
		}
		return tea.Batch(closeOverlay, ui.Copy(ctx, string(data), a.ExportCopied()))
	}
	path := d.path(ctx)
	if path == "" {
		d.hint = a.ExportSaveFailed()
		return nil
	}
	write := func() tea.Msg { return exportDoneMsg{dlg: d, path: path, err: writeExport(path, data)} }
	if _, err := os.Lstat(path); err == nil {
		name := filepath.Base(path)
		return ui.OpenOverlay(widgets.Confirm(a.ExportOverwriteTitle(name), shortPath(path), a.ExportOverwriteYes(), a.KeyCancel(), write))
	}
	return write
}

// lines is the dialog's content.
func (d *exportDialog) lines(ctx *ui.Context) []formLine {
	a := ctx.T.Archcanvas
	var out []formLine
	opt := func(i int, s string, on bool) {
		st, mark := sBase, "  "
		if on {
			st, mark = sSel, "▸ "
		}
		out = append(out, formLine{s: mark + s, st: st, row: i})
	}
	if d.step == 0 {
		for i, f := range exportFormats {
			opt(i, f.label(a), i == d.cur)
		}
	} else {
		out = append(out, formLine{s: exportFormats[d.cur].label(a), st: sDim, row: -1}, formLine{s: a.ExportWhere(), st: sAccBold, row: -1})
		opt(0, a.ExportToFile(), d.target == 0)
		opt(1, a.ExportToClipboard(), d.target == 1)
		if d.target == 0 {
			if p := d.path(ctx); p != "" {
				out = append(out, formLine{s: a.ExportPath(shortPath(p)), st: sDim, row: -1})
			}
		}
	}
	out = append(out, formLine{row: -1})
	if d.hint != "" {
		out = append(out, formLine{s: d.hint, st: sWarn, row: -1})
	}
	return append(out, formLine{s: a.ExportRights(), st: sDim, row: -1}, formLine{s: a.ExportHint(), st: sDim, row: -1})
}

func (d *exportDialog) View(ctx *ui.Context, _ bool) string {
	w, h := d.size.W, d.size.H
	if w <= 0 || h <= 0 {
		return ""
	}
	b := newBuf(w, h)
	b.fill(layout.Rect{W: w, H: h}, sBase)
	lines := d.lines(ctx)
	// the key hint and the rights notice stay at the bottom when rows run out
	if len(lines) > h {
		lines = append(lines[:max(h-2, 0)], lines[len(lines)-2:]...)
		lines = lines[len(lines)-min(h, len(lines)):]
	}
	for i, l := range lines {
		b.line(1, i, w-2, l.s, l.st)
	}
	return b.render(ctx.Theme)
}
