package arch

import (
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/archdoc"
	"github.com/patrache/masil/internal/collab"
	"github.com/patrache/masil/internal/collab/collabtest"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/palette"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

func strp(s string) *string { return &s }

// cpuOp sets node key's cpu_cores.
func cpuOp(key string, n int) collab.Op {
	return collab.Op{Op: collab.OpSet, ID: key, Field: "cpu_cores", Value: json.RawMessage(strconvItoa(n))}
}

// snapWith is the fixture design with node key's cpu_cores = cpu, as a
// collab snapshot.
func snapWith(t *testing.T, key string, cpu int) json.RawMessage {
	t.Helper()
	objs := loadFixture(t, "pay_v3").ToObjects()
	objs[key]["cpu_cores"] = json.RawMessage(strconvItoa(cpu))
	d, _ := archdoc.FromObjects(objs, nil)
	b, err := json.Marshal(snapshotOf(d))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestVersionsListAndPreview(t *testing.T) {
	hub := collabtest.New(t)
	_, ka, _ := sharedFixture(t, hub, api.Doc{})
	h, c, sh := docHarness(t, hub)
	at := time.Date(2026, 11, 4, 9, 30, 0, 0, time.UTC)
	versions := []api.DocVersion{
		{No: 3, Seq: 9, Name: strp("리뷰용"), CreatedBy: &api.UserSummary{ID: fixtures.UserHana}, CreatedAt: at.Add(2 * time.Hour)},
		{No: 2, Seq: 5, Auto: true, CreatedAt: at.Add(time.Hour)},
		{No: 1, Seq: 1, Name: strp("초안"), CreatedBy: &api.UserSummary{ID: fixtures.UserSam}, CreatedAt: at},
	}
	h.API.JSON("GET /v1/docs/{id}/versions", 200, map[string]any{"items": versions})
	h.API.Handle("GET /v1/docs/{id}/versions/{no}", func(w http.ResponseWriter, r *http.Request) {
		no := r.PathValue("no")
		for _, v := range versions {
			if strconvItoa(v.No) == no {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{"version": v, "snapshot": snapWith(t, ka, 16)})
				return
			}
		}
		w.WriteHeader(404)
	})

	if c.shared() == nil {
		t.Fatal("not a room document")
	}
	h.Send(ui.RunCommandMsg{ID: "arch.versions"})
	if c.verDlg == nil || len(c.verDlg.items) != 3 {
		t.Fatalf("versions dialog %+v", c.verDlg)
	}
	screen := h.Screen()
	for _, want := range []string{"v3 — 리뷰용", "v2 — 자동 저장 11-04 19:30", "v1 — 초안", "하나 · 11-04 20:30"} {
		if !strings.Contains(screen, want) {
			t.Errorf("list misses %q:\n%s", want, screen)
		}
	}
	h.Golden("120x40")

	// enter: v3 read-only on the canvas, its label on the header line
	h.Press("enter")
	if sh.preview == nil {
		t.Fatalf("no preview:\n%s", h.Screen())
	}
	if got := c.node(ka[2:]).CPUCores; got != 16 {
		t.Errorf("the canvas shows cpu %d, want the version's 16", got)
	}
	if !strings.Contains(h.Screen(), "v3 — 리뷰용 미리보기") {
		t.Errorf("no preview marker:\n%s", h.Screen())
	}
	sub := sh.submits
	c.sel = ka[2:]
	h.Press("a")
	h.Press("d")
	if sh.submits != sub || c.node(ka[2:]) == nil {
		t.Error("the preview was edited")
	}
	if !shown(h.Screen(), h.Context().T.Archcanvas.PreviewReadonly()) {
		t.Errorf("no read-only toast:\n%s", h.Screen())
	}
	h.Press("esc")
	if sh.preview != nil {
		t.Fatal("esc did not end the preview")
	}
	if c.node(ka[2:]).CPUCores == 16 {
		t.Error("the canvas still shows the version after esc")
	}

	if !cmdAvailable(t, h.Context(), "arch.versions") {
		t.Error("arch.versions unavailable on a room document")
	}
}

// cmdAvailable is palette command id's Available on ctx.
func cmdAvailable(t *testing.T, ctx *ui.Context, id string) bool {
	t.Helper()
	c, ok := palette.Lookup(id)
	if !ok || c.Available == nil {
		t.Fatalf("no palette command %s", id)
	}
	return c.Available(ctx)
}

func TestVersionDiffList(t *testing.T) {
	hub := collabtest.New(t)
	d, ka, _ := sharedFixture(t, hub, api.Doc{})
	h, c, _ := docHarness(t, hub)
	cl := h.Context().API
	bg := context.Background()
	hub.Inject("D1", fixtures.UserHana, []collab.Op{cpuOp(ka, 2)})
	if _, err := cl.CreateDocVersion(bg, "D1", "전"); err != nil {
		t.Fatal(err)
	}
	// one node more, and cpu_cores 2 → 8
	var cache archdoc.Node
	for _, n := range d.Nodes {
		if n.ID == "cache" {
			cache = n
		}
	}
	fields := maps.Clone(d.ToObjects()["n:"+cache.ID])
	fields["label"] = json.RawMessage(`"검색 캐시"`)
	fields["pos"] = json.RawMessage(`[300,40]`)
	hub.Inject("D1", fixtures.UserHana, []collab.Op{
		{Op: collab.OpPut, ID: "n:cache2", Type: "node", Fields: fields},
		cpuOp(ka, 8),
	})
	if _, err := cl.CreateDocVersion(bg, "D1", "후"); err != nil {
		t.Fatal(err)
	}
	h.Send(ui.RunCommandMsg{ID: "arch.versions"})
	if c.verDlg == nil || len(c.verDlg.items) != 2 {
		t.Fatalf("versions %+v", c.verDlg)
	}
	h.Press("d")
	if c.verDlg.step != versionsDiff {
		t.Fatalf("d did not show the diff (hint %q):\n%s", c.verDlg.hint, h.Screen())
	}
	screen := h.Screen()
	label := c.node(ka[2:]).Label
	for _, want := range []string{"v1 → v2", "추가 1", "삭제 0", "바뀐 설정 1", "+ 검색 캐시", "~ " + label + " · CPU 코어 2 → 8"} {
		if !strings.Contains(screen, want) {
			t.Errorf("diff misses %q:\n%s", want, screen)
		}
	}
	h.Press("esc")
	if c.verDlg.step != versionsList {
		t.Error("esc did not go back to the list")
	}
	// the oldest version has nothing before it
	h.Press("j", "d")
	if c.verDlg.step != versionsList || c.verDlg.hint == "" {
		t.Error("d on the oldest version did not explain")
	}
	h.Check()
}

func TestRestoreMakesNewVersion(t *testing.T) {
	hub := collabtest.New(t)
	_, ka, _ := sharedFixture(t, hub, api.Doc{})
	h, c, sh := docHarness(t, hub)
	cl := h.Context().API
	bg := context.Background()
	for i, name := range []string{"하나", "둘", "셋"} {
		hub.Inject("D1", fixtures.UserHana, []collab.Op{cpuOp(ka, 2+2*i)})
		if _, err := cl.CreateDocVersion(bg, "D1", name); err != nil {
			t.Fatal(err)
		}
	}
	h.Send(ui.RunCommandMsg{ID: "arch.versions"})
	h.Press("j") // v2
	h.Press("r")
	if !strings.Contains(h.Screen(), "v2 버전으로 되돌릴까요?") {
		t.Fatalf("no restore confirm:\n%s", h.Screen())
	}
	h.Press("y")
	restored := false
	for _, rq := range h.API.Requests() {
		if rq.Method == "POST" && rq.Path == "/v1/docs/D1/versions/2/restore" {
			restored = true
		}
	}
	if !restored {
		t.Fatalf("no restore request: %+v", h.API.Requests())
	}
	items := c.verDlg.items
	if len(items) != 4 || items[0].No != 4 || items[0].RestoredFrom == nil || *items[0].RestoredFrom != 2 {
		t.Fatalf("after the restore: %+v", items)
	}
	for i, no := range []int{4, 3, 2, 1} {
		if items[i].No != no {
			t.Errorf("version %d is v%d, want v%d (old ones remain)", i, items[i].No, no)
		}
	}
	if !strings.Contains(h.Screen(), "v4 — v2 버전으로 되돌림") {
		t.Errorf("the new version is not listed:\n%s", h.Screen())
	}
	// the reset reaches the canvas: cpu_cores back to v2's 4
	waitUntil(t, h, "the restored state", func() bool { return field(sh.objs, ka, "cpu_cores") == "4" })
}

func TestVersionsNameSave(t *testing.T) {
	hub := collabtest.New(t)
	sharedFixture(t, hub, api.Doc{})
	h, c, _ := docHarness(t, hub)
	h.Send(ui.RunCommandMsg{ID: "arch.versions"})
	h.Press("n")
	h.Type("리뷰용")
	h.Press("enter")
	if items := c.verDlg.items; len(items) != 1 || items[0].Name == nil || *items[0].Name != "리뷰용" {
		t.Fatalf("named save: %+v", items)
	}
	// limit_exceeded: a notice in the dialog
	h.API.JSON("POST /v1/docs/{id}/versions", 409, map[string]any{"error": map[string]any{"code": "limit_exceeded", "message": "too many"}})
	h.Press("n")
	h.Type("또")
	h.Press("enter")
	if !strings.Contains(h.Screen(), h.Context().T.Archcanvas.VersionsLimit()) {
		t.Errorf("no limit notice:\n%s", h.Screen())
	}
}

func TestVersionsNotOnPersonal(t *testing.T) {
	r := toolRoute("arch:file:t1", map[string]string{"file": fixtureName})
	h, _ := keyHarness(t, r, withDesign(t, fixtureName, loadFixture(t, "pay_v3")))
	if cmdAvailable(t, h.Context(), "arch.versions") {
		t.Error("arch.versions offered on a personal design")
	}
	h.Send(ui.RunCommandMsg{ID: "arch.versions"})
	if c := canvasOf(t, h, r); c.verDlg != nil {
		t.Error("a personal design opened the versions dialog")
	}
}
