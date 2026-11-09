package apitool

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/patrache/masil/internal/i18n"
)

const sampleJSON = `{"id": 7, "customer": {"name": "Tom", "vip": true, "tags": null, "since": 2019}, "items": [{"sku": "a"}, {"sku": "b"}, {"sku": "c"}], "x-y": "z", "empty": {}}`

func rowTexts(tr *Tree, f FoldState, top, h int) []string {
	t := i18n.KoT()
	var out []string
	for _, r := range tr.Window(f, top, h, 2) {
		out = append(out, RowText(t, r))
	}
	return out
}

func TestJSONTreeFoldSummary(t *testing.T) {
	tr, err := BuildTree([]byte(sampleJSON))
	if err != nil {
		t.Fatal(err)
	}
	all := rowTexts(tr, nil, 0, 100)
	want := []string{
		`  "id": 7`,
		`▾ "customer": {`,
		`    "name": "Tom"`,
		`    "vip": true`,
		`    "tags": null`,
		`    "since": 2019`,
		`▾ "items": [`,
		`  ▾ [0]: {`,
		`      "sku": "a"`,
		`  ▾ [1]: {`,
		`      "sku": "b"`,
		`  ▾ [2]: {`,
		`      "sku": "c"`,
		`  "x-y": "z"`,
		`  "empty": {}`,
	}
	if strings.Join(all, "\n") != strings.Join(want, "\n") {
		t.Fatalf("rows:\n%s", strings.Join(all, "\n"))
	}
	if tr.Count(nil) != len(want) {
		t.Fatalf("Count = %d", tr.Count(nil))
	}
	f := FoldState{"$.customer": true, "$.items": true}
	got := rowTexts(tr, f, 0, 100)
	want = []string{
		`  "id": 7`,
		`▸ "customer": {…} 4개`,
		`▸ "items": […] 3개`,
		`  "x-y": "z"`,
		`  "empty": {}`,
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("folded:\n%s", strings.Join(got, "\n"))
	}
	if tr.Count(f) != 5 {
		t.Fatalf("Count(folded) = %d", tr.Count(f))
	}
	rows := tr.Window(f, 1, 2, 2)
	if rows[0].Path != "$.customer" || !rows[0].Foldable || !rows[0].Folded || rows[1].Path != "$.items" {
		t.Fatalf("rows = %+v", rows)
	}
	rows = tr.Window(nil, 0, 100, 2)
	if rows[7].Path != "$.items[0]" || rows[8].Path != "$.items[0].sku" || rows[13].Path != `$['x-y']` {
		t.Fatalf("paths = %q %q %q", rows[7].Path, rows[8].Path, rows[13].Path)
	}
	if en := RowText(i18n.For(i18n.En), tr.Window(f, 1, 1, 2)[0]); en != `▸ "customer": {…} 4 items` {
		t.Fatalf("en = %q", en)
	}
}

func TestJSONTreeScalarRootAndErrors(t *testing.T) {
	tr, err := BuildTree([]byte(` "hi" `))
	if err != nil {
		t.Fatal(err)
	}
	if got := rowTexts(tr, nil, 0, 5); len(got) != 1 || got[0] != `  "hi"` {
		t.Fatalf("scalar root = %q", got)
	}
	tr, _ = BuildTree([]byte(`[]`))
	if got := rowTexts(tr, nil, 0, 5); len(got) != 0 || tr.Count(nil) != 0 {
		t.Fatalf("empty array root = %q", got)
	}
	for _, bad := range []string{"", "<html>", `{"a":`, `{"a":1} x`, `{"a" 1}`} {
		if _, err := BuildTree([]byte(bad)); err == nil {
			t.Fatalf("BuildTree(%q) ok", bad)
		}
	}
}

func TestJSONTreeFoldStateByPath(t *testing.T) {
	f := FoldState{"$.items[1]": true}
	a, _ := BuildTree([]byte(sampleJSON))
	b, _ := BuildTree([]byte(strings.ReplaceAll(sampleJSON, `"b"`, `"bee"`)))
	ra, rb := rowTexts(a, f, 0, 100), rowTexts(b, f, 0, 100)
	if len(ra) != len(rb) || ra[9] != `  ▸ [1]: {…} 1개` || rb[9] != ra[9] {
		t.Fatalf("a:\n%s\nb:\n%s", strings.Join(ra, "\n"), strings.Join(rb, "\n"))
	}
	// A path folded in the state but missing in the body is ignored.
	f["$.nope"] = true
	if got := rowTexts(a, f, 0, 100); len(got) != len(ra) {
		t.Fatal("unknown path changed the rows")
	}
}

func TestJSONTreeWindowOnlyVisible(t *testing.T) {
	var b strings.Builder
	b.WriteString(`{"data":[`)
	for i := range 100_000 {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `{"id":%d}`, i)
	}
	b.WriteString(`]}`)
	start := time.Now()
	tr, err := BuildTree([]byte(b.String()))
	if err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("BuildTree took %v", d)
	}
	rows := tr.Window(nil, 50_000, 20, 2)
	if len(rows) != 20 {
		t.Fatalf("%d rows", len(rows))
	}
	// row 0 "data", then 2 rows per element: row 50000 is element 24999's "id"
	if rows[0].Path != "$.data[24999].id" || rows[1].Path != "$.data[25000]" {
		t.Fatalf("rows[0..1] = %q %q", rows[0].Path, rows[1].Path)
	}
	f := FoldState{"$.data[0]": true, "$.data[1]": true}
	rows = tr.Window(f, 3, 1, 2)
	if rows[0].Path != "$.data[2]" {
		t.Fatalf("folded window = %q", rows[0].Path)
	}
	if tr.Count(nil) != 200_001 || tr.Count(FoldState{"$.data": true}) != 1 {
		t.Fatalf("Count = %d / %d", tr.Count(nil), tr.Count(FoldState{"$.data": true}))
	}
	start = time.Now()
	for range 100 {
		tr.Window(f, 199_000, 40, 2)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("100 windows took %v", d)
	}
}

func TestMinifiedHugeStringLine(t *testing.T) {
	long := strings.Repeat("x", 10<<20)
	tr, err := BuildTree([]byte(`{"blob":"` + long + `"}`))
	if err != nil {
		t.Fatal(err)
	}
	r := tr.Window(nil, 0, 1, 2)[0]
	if len(r.Summary) > maxSummaryBytes+8 {
		t.Fatalf("summary kept %d bytes", len(r.Summary))
	}
}

func TestJSONTreeSanitizes(t *testing.T) {
	tr, err := BuildTree([]byte(`{"k\u001b[2Jey\u202e": "va\u001b]0;title\u0007lue\u202e", "a\nb": 1}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range rowTexts(tr, nil, 0, 10) {
		if strings.ContainsAny(s, "\x1b\u202e\a\n") {
			t.Fatalf("unsanitized row %q", s)
		}
	}
}
