package arch

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/patrache/masil/internal/archdoc"
	"github.com/patrache/masil/internal/archexport"
	"github.com/patrache/masil/internal/archsim"
	"github.com/patrache/masil/internal/store"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

// withDesigns seeds the store fixtures and saves each design under its name,
// modified at the given time.
func withDesigns(t *testing.T, ds []savedDesign) uitest.Option {
	return uitest.WithSeed(func(s *store.Store) {
		fixtures.Seed(s)
		dir := filepath.Join(os.Getenv("MASIL_CONFIG_DIR"), "arch")
		st, err := archdoc.OpenStore(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range ds {
			if err := st.Save(d.name, d.d, true); err != nil {
				t.Fatal(err)
			}
			if err := os.Chtimes(filepath.Join(dir, d.name+".json"), d.at, d.at); err != nil {
				t.Fatal(err)
			}
		}
	})
}

type savedDesign struct {
	name string
	d    *archdoc.Design
	at   time.Time
}

func storeNames(t *testing.T, h *uitest.Harness) []string {
	t.Helper()
	st, err := storeOf(h.Context())
	if err != nil {
		t.Fatal(err)
	}
	es, err := st.List()
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range es {
		out = append(out, e.Name)
	}
	return out
}

func TestOpenSavedDesign(t *testing.T) {
	a := archdoc.New("설계 A")
	b := archdoc.New("설계 B")
	n := archdoc.Defaults(archdoc.KindAPI)
	n.ID, n.Label = "api", "api"
	b.Nodes = []archdoc.Node{n}
	h := newHarness(t, withDesigns(t, []savedDesign{
		{"older", a, time.Date(2026, 1, 1, 9, 0, 0, 0, time.Local)},
		{"newer", b, time.Date(2026, 2, 1, 9, 0, 0, 0, time.Local)},
	}))
	h.Resize(120, 40)
	h.Send(ui.RunCommandMsg{ID: "arch.open"})
	a8 := h.Context().T.Archcanvas
	s := h.Screen()
	if !strings.Contains(s, a8.FilesTitle()) {
		t.Fatalf("arch.open showed no files dialog:\n%s", s)
	}
	io, in := strings.Index(s, "older"), strings.Index(s, "newer")
	if io < 0 || in < 0 || in > io {
		t.Fatalf("want newer before older:\n%s", s)
	}
	for _, want := range []string{"설계 B", a8.FilesNodes(1), "2026-02-01 09:00"} {
		if !strings.Contains(s, want) {
			t.Errorf("row lacks %q:\n%s", want, s)
		}
	}
	h.Press("enter")
	c := canvasOf(t, h, toolRoute(keyFile+"newer", map[string]string{queryFile: "newer"}))
	if c.design.Title != "설계 B" || len(c.design.Nodes) != 1 {
		t.Errorf("opened %+v", c.design)
	}
	if strings.Contains(h.Screen(), a8.FilesTitle()) {
		t.Error("the dialog stayed open")
	}
}

func TestImportJSONErrors(t *testing.T) {
	h := newHarness(t, withDesign(t, "keep", archdoc.New("keep")))
	h.Resize(120, 40)
	a8 := h.Context().T.Archcanvas
	dir := t.TempDir()
	write := func(name string, data []byte) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, data, 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	big := write("big.json", []byte(`{"schema_version":1,"title":"`+strings.Repeat("x", 2<<20)+`"}`))
	broken := write("broken.json", []byte(`{"schema_version": 1, "nodes": [`))
	bad := write("bad.json", []byte(`{"schema_version":1,"nodes":[{"id":"n1","kind":"warp"}]}`))
	cases := []struct{ path, want string }{
		{big, a8.ImportTooLarge()},
		{broken, a8.ImportSyntax()},
		{bad, "unknown_kind"},
	}
	h.Send(ui.RunCommandMsg{ID: "arch.open"})
	for _, tc := range cases {
		h.Press("i")
		h.Type(tc.path)
		h.Press("enter")
		if s := h.Screen(); !strings.Contains(s, tc.want) {
			t.Errorf("%s: no %q:\n%s", filepath.Base(tc.path), tc.want, s)
		}
		h.Press("esc") // back to the list
	}
	if got := storeNames(t, h); len(got) != 1 || got[0] != "keep" {
		t.Errorf("store after failed imports: %v", got)
	}
	// a good file is imported under its title and opened
	good := archdoc.New("가져온 설계")
	h.Press("i")
	h.Type(write("good.json", mustEncode(t, good)))
	h.Press("enter")
	if got := storeNames(t, h); len(got) != 2 {
		t.Fatalf("store after the import: %v", got)
	}
	c := canvasOf(t, h, toolRoute(keyFile+"가져온 설계", nil))
	if c.design.Title != "가져온 설계" {
		t.Errorf("opened %q", c.design.Title)
	}
}

// exportHarness opens the panel design titled "결제 시스템" with export_dir set.
func exportHarness(t *testing.T) (*uitest.Harness, *canvasModel, string) {
	t.Helper()
	d := panelDesign()
	d.Title = "결제 시스템"
	h, c := panelHarness(t, d)
	dir := filepath.Join(t.TempDir(), "out")
	h.Context().Config.Tools = map[string]map[string]any{"arch": {"export_dir": dir}}
	return h, c, dir
}

// exportKeys runs arch.export and picks format i, then target j (0 = file).
func exportKeys(h *uitest.Harness, c *canvasModel, format, target int) {
	h.Send(commandMsg{instance: c.route.ID, id: "arch.export"})
	h.Press(repeat("j", format)...)
	h.Press("enter")
	h.Press(repeat("j", target)...)
	h.Press("enter")
}

func TestExportWritesFiles(t *testing.T) {
	h, c, dir := exportHarness(t)
	ctx := h.Context()
	a8 := ctx.T.Archcanvas
	d := designOf(t, c)
	r := archsim.Analyze(d, archsim.Options{})
	js, err := d.Encode()
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		file string
		data []byte
	}{
		{"결제 시스템.mmd", archexport.Mermaid(d, archexport.Options{})},
		{"결제 시스템.d2", archexport.D2(d, archexport.Options{})},
		{"결제 시스템.tf", archexport.Terraform(d, archexport.Options{Header: []string{a8.TfHeader(), a8.EstimateDisclaimer()}})},
		{"결제 시스템.txt", archexport.Snapshot(d, r, 80, snapText(ctx.T))},
		{"결제 시스템.txt", archexport.Snapshot(d, r, 120, snapText(ctx.T))},
		{"결제 시스템.json", js},
	}
	for i, w := range want {
		exportKeys(h, c, i, 0)
		if i == 4 { // the 80-column snapshot is there: confirm the overwrite
			if s := h.Screen(); !strings.Contains(s, a8.ExportOverwriteYes()) {
				t.Fatalf("no overwrite question for the second .txt:\n%s", s)
			}
			h.Press("y")
		}
		got, err := os.ReadFile(filepath.Join(dir, w.file))
		if err != nil {
			t.Fatalf("format %d: %v", i, err)
		}
		if string(got) != string(w.data) {
			t.Errorf("format %d (%s): content differs\n got %q\nwant %q", i, w.file, firstLine(got), firstLine(w.data))
		}
	}
	tf, _ := os.ReadFile(filepath.Join(dir, "결제 시스템.tf"))
	if !strings.HasPrefix(string(tf), "# "+a8.TfHeader()) {
		t.Errorf("the .tf does not start with the notice:\n%s", firstLine(tf))
	}
}

func firstLine(b []byte) string {
	s, _, _ := strings.Cut(string(b), "\n")
	return s
}

func TestExportAsksBeforeOverwrite(t *testing.T) {
	h, c, dir := exportHarness(t)
	a8 := h.Context().T.Archcanvas
	p := filepath.Join(dir, "결제 시스템.mmd")
	exportKeys(h, c, 0, 0)
	if err := os.WriteFile(p, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	exportKeys(h, c, 0, 0)
	s := h.Screen()
	if !strings.Contains(s, a8.ExportOverwriteYes()) || !strings.Contains(s, "결제 시스템.mmd") {
		t.Fatalf("no overwrite question:\n%s", s)
	}
	h.Press("n") // 취소
	if got, _ := os.ReadFile(p); string(got) != "old" {
		t.Errorf("취소 wrote the file: %q", got)
	}
	if !strings.Contains(h.Screen(), a8.ExportTitle()) {
		t.Errorf("취소 closed the export dialog too:\n%s", h.Screen())
	}
	h.Press("esc")
	exportKeys(h, c, 0, 0)
	h.Press("y")
	if got, _ := os.ReadFile(p); string(got) != string(archexport.Mermaid(designOf(t, c), archexport.Options{})) {
		t.Errorf("덮어쓰기 left %q", got)
	}
	if s := h.Screen(); strings.Contains(s, a8.ExportTitle()) {
		t.Errorf("the export dialog stayed open:\n%s", s)
	}
}

func TestExportClipboard(t *testing.T) {
	h, c, dir := exportHarness(t)
	exportKeys(h, c, 0, 1)
	want := string(archexport.Mermaid(designOf(t, c), archexport.Options{}))
	found := false
	for _, m := range h.TeaMsgs() {
		v := reflect.ValueOf(m)
		if v.Type().Name() == "setClipboardMsg" && v.Kind() == reflect.String && v.String() == want {
			found = true
		}
	}
	if !found {
		t.Errorf("no tea.SetClipboard with the Mermaid text; msgs %v", h.TeaMsgs())
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("the clipboard export touched the export folder: %v", err)
	}
}
