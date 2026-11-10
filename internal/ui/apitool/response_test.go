package apitool

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/patrache/masil/internal/apitool"
	"github.com/patrache/masil/internal/ui/uitest"
)

// showOn shows resp as the edited request's response with the response
// pane focused.
func showOn(h *uitest.Harness, tl *Tool, resp *apitool.Response) {
	h.T.Helper()
	tl.showResponse(tl.respKey(), tl.req, resp)
	tl.focus = paneResponse
}

func jsonResponse(body string) *apitool.Response {
	return &apitool.Response{Status: 200, StatusText: "OK", Proto: "HTTP/1.1", ContentType: "application/json",
		Header: http.Header{"Content-Type": {"application/json"}}, Body: []byte(body), Size: int64(len(body))}
}

// selPath is the JSON path of the selected tree row.
func selPath(tl *Tool) string {
	s := tl.rv.current()
	rows := s.tree.Window(tl.rv.fold(), tl.rv.sel, 1, 0)
	if len(rows) == 0 {
		return ""
	}
	return rows[0].Path
}

func TestResponseTabs(t *testing.T) {
	h := openAPI(t)
	seedFirstScreen(h)
	tl := toolOf(h)
	tl.focus = paneResponse
	for _, c := range []struct{ key, want string }{
		{"2", "Cache-Control: private, max-age=60"},
		{"3", "✓ status"},
		{"4", "HTTP/1.1 200 OK"},
		{"1", `"login": "sam"`},
	} {
		h.Press(c.key)
		if s := h.Screen(); !strings.Contains(s, c.want) {
			t.Fatalf("tab %s lacks %q:\n%s", c.key, c.want, s)
		}
		h.Check()
	}
	h.Press("t")
	if tl.rv.tab != rtHeaders {
		t.Fatalf("t did not go to the next tab: %v", tl.rv.tab)
	}
}

func TestFoldKeepsAfterResend(t *testing.T) {
	srv, hits := hitServer(t, created)
	h := openAPI(t)
	tl := seedRequests(h, nil, paymentRequest(srv))
	h.Press("enter")
	waitScreen(t, h, "201 Created")
	h.Press("]") // the response pane
	if tl.focus != paneResponse {
		t.Fatalf("focus = %v", tl.focus)
	}
	h.Press("j", "j")
	if p := selPath(tl); p != "$.customer" {
		t.Fatalf("selected %q, want $.customer", p)
	}
	h.Press("enter")
	if !tl.rv.fold()["$.customer"] || !strings.Contains(h.Screen(), "{…}") {
		t.Fatalf("enter did not fold customer:\n%s", h.Screen())
	}
	h.Press("[", "enter") // back to the request, send again
	for range 400 {
		if hits.Load() == 2 && !tl.running(jobSend) {
			break
		}
		h.Advance(50 * time.Millisecond)
		time.Sleep(2 * time.Millisecond)
	}
	waitScreen(t, h, "201 Created")
	if !strings.Contains(h.Screen(), "{…}") {
		t.Fatalf("customer unfolded after the resend:\n%s", h.Screen())
	}
	h.Press("]", "j", "j", "l")
	if tl.rv.fold()["$.customer"] {
		t.Fatalf("l did not unfold")
	}
	h.Press("h")
	if !tl.rv.fold()["$.customer"] {
		t.Fatalf("h did not fold")
	}
}

func TestFindInBody(t *testing.T) {
	h := openAPI(t)
	tl := seedRequests(h, nil, &apitool.Request{ID: "x", Method: "GET", URL: "https://example.com/x"})
	var b strings.Builder
	b.WriteString("{")
	for i := range 120 {
		fmt.Fprintf(&b, `"filler_%03d":%d,`, i, i)
	}
	b.WriteString(`"amount":12000}`)
	showOn(h, tl, jsonResponse(b.String()))
	if strings.Contains(h.Screen(), `"amount"`) {
		t.Fatalf("amount already visible")
	}
	h.Press("/")
	h.Type("amount")
	h.Press("enter")
	if s := h.Screen(); !strings.Contains(s, `"amount": 12000`) {
		t.Fatalf("find did not scroll to amount:\n%s", s)
	}
	if p := selPath(tl); p != "$.amount" {
		t.Fatalf("selected %q, want $.amount", p)
	}
	h.Press("N")
	if p := selPath(tl); p != "$.amount" {
		t.Fatalf("N with one hit moved to %q", p)
	}

	// text view: the hit piece comes to the top.
	h.Press("o")
	h.Press("/")
	h.Type("filler_100")
	h.Press("enter")
	if s := h.Screen(); !strings.Contains(s, "filler_100") {
		t.Fatalf("text find:\n%s", s)
	}
}

func TestSaveBodyToFile(t *testing.T) {
	for _, c := range []struct{ cd, want string }{
		{`attachment; filename="../../etc/passwd"`, "passwd"},
		{`attachment; filename="..\\x.txt"`, "x.txt"},
		{`attachment; filename=".."`, "response.bin"},
		{``, "items.json"},
	} {
		r := jsonResponse(`{}`)
		if c.cd != "" {
			r.Header.Set("Content-Disposition", c.cd)
		}
		if got := saveName(r, "https://example.com/v1/items.json?x=1"); got != c.want {
			t.Errorf("saveName(%q) = %q, want %q", c.cd, got, c.want)
		}
	}

	h := openAPI(t)
	tl := seedRequests(h, nil, &apitool.Request{ID: "x", Method: "GET", URL: "https://example.com/x"})
	r := jsonResponse(`{"a":1}`)
	r.Header.Set("Content-Disposition", `attachment; filename="../../etc/passwd"`)
	showOn(h, tl, r)
	h.Press("w")
	if s := overlayText(h); !strings.Contains(s, "passwd") || strings.Contains(s, "etc") {
		t.Fatalf("save dialog default:\n%s", s)
	}
	out := filepath.Join(t.TempDir(), "body.json")
	h.Press("ctrl+u")
	h.Type(out)
	h.Press("enter")
	waitWhole(t, h, ko.BodySaved(out))
	assertFile(t, out, `{"a":1}`)

	// a spilled body: the whole spill file is saved, not the shown part.
	spill := filepath.Join(t.TempDir(), "masil-api-1.body")
	whole := strings.Repeat("x", 4096) + "TAIL"
	if err := os.WriteFile(spill, []byte(whole), 0o600); err != nil {
		t.Fatal(err)
	}
	big := &apitool.Response{Status: 200, StatusText: "OK", ContentType: "text/plain", Header: http.Header{},
		Body: []byte(whole[:4096]), Size: int64(len(whole)), Truncated: true, Spill: spill}
	showOn(h, tl, big)
	h.Press("esc") // the report dialog
	h.Press("w", "ctrl+u")
	out2 := filepath.Join(t.TempDir(), "big.txt")
	h.Type(out2)
	h.Press("enter")
	waitWhole(t, h, ko.BodySaved(out2))
	assertFile(t, out2, whole)
}

func assertFile(t *testing.T, path, want string) {
	t.Helper()
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	assertPrivateMode(t, path, st)
	b, _ := os.ReadFile(path)
	if string(b) != want {
		t.Fatalf("%s holds %d bytes, want %d", path, len(b), len(want))
	}
}

// assertPrivateMode checks that a file the tool saved (fillPrivate) is
// readable by me only, as far as the OS shows it. On Unix that is mode 0600.
// Windows has no POSIX modes: Go reports 0666 for every writable file and
// 0444 for a read-only one, and File.Chmod(0o600) only clears the read-only
// attribute. There the file is protected by the NTFS ACL it inherits from its
// folder — under %USERPROFILE% (the default Downloads folder) only the user,
// SYSTEM and Administrators — the same as every other masil file
// (config.WriteFileAtomic, credentials.json). So on Windows it checks what
// masil does decide there: a plain, writable (not read-only) file.
func assertPrivateMode(t *testing.T, path string, fi fs.FileInfo) {
	t.Helper()
	if runtime.GOOS == "windows" {
		if !fi.Mode().IsRegular() || fi.Mode().Perm() != 0o666 {
			t.Fatalf("%s mode = %v, want a plain writable file (Windows reports 0666)", path, fi.Mode())
		}
		return
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("%s mode = %v, want 0600", path, fi.Mode().Perm())
	}
}

func TestBinaryBodyNotice(t *testing.T) {
	png := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0, 0xff, 0x10}, 48*1024/3)...)
	png = png[:48*1024]
	srv, _ := hitServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(png)
	})
	h := openAPI(t)
	seedRequests(h, nil, &apitool.Request{ID: "img", Method: "GET", URL: srv.URL + "/a.png"})
	h.Press("enter")
	waitWhole(t, h, ko.BinaryBody("image/png", "48KB"))
	h.Check()
}

func TestHTMLShownAsText(t *testing.T) {
	srv, _ := hitServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<html><body>\n<h1>안녕</h1>\n</body></html>"))
	})
	h := openAPI(t)
	seedRequests(h, nil, &apitool.Request{ID: "page", Method: "GET", URL: srv.URL + "/"})
	h.Press("enter")
	waitScreen(t, h, "<h1>안녕</h1>")
	h.Check()
}

func TestCopyBody(t *testing.T) {
	h := openAPI(t)
	tl := seedRequests(h, nil, &apitool.Request{ID: "x", Method: "GET", URL: "https://example.com/x"})
	body := `{"a":1,"b":[1,2]}`
	showOn(h, tl, jsonResponse(body))
	h.Press("y")
	var pretty bytes.Buffer
	_ = json.Indent(&pretty, []byte(body), "", "  ")
	clips := h.Clipboards()
	if len(clips) == 0 || clips[len(clips)-1] != pretty.String() {
		t.Fatalf("tree view copy = %q, want the pretty body", clips)
	}
	h.Press("o", "y")
	clips = h.Clipboards()
	if clips[len(clips)-1] != body {
		t.Fatalf("text view copy = %q, want the body as received", clips[len(clips)-1])
	}
}

func TestBigResponseNotice(t *testing.T) {
	big := bytes.Repeat([]byte("0123456789abcdef\n"), 12<<20/17)
	srv, _ := hitServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write(big)
	})
	h := openAPI(t)
	tl := seedRequests(h, nil, &apitool.Request{ID: "big", Method: "GET", URL: srv.URL + "/big"})
	h.Press("enter")
	waitScreen(t, h, ko.BigBody())
	s := tl.rv.current()
	if s == nil || !s.resp.Truncated || s.resp.Spill == "" {
		t.Fatalf("not spilled: %+v", s)
	}
	h.Press("]", "j", "j")
	start := time.Now()
	_ = h.Screen()
	if d := time.Since(start); d > 500*time.Millisecond {
		t.Fatalf("drawing took %v", d)
	}
	h.Check()
}

func TestMinifiedHugeJSONWindowed(t *testing.T) {
	var b strings.Builder
	b.WriteString(`{"items":[`)
	for i := 0; b.Len() < 10_000_000; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `{"id":%d,"name":"item-%d","ok":true}`, i, i)
	}
	b.WriteString(`]}`)
	text := strings.Repeat("lorem ipsum dolor sit amet ", 10_000_000/27)
	for _, c := range []struct {
		name string
		resp *apitool.Response
	}{
		{"json", jsonResponse(b.String())},
		{"text", &apitool.Response{Status: 200, StatusText: "OK", ContentType: "text/plain", Header: http.Header{}, Body: []byte(text), Size: int64(len(text))}},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := openAPI(t)
			tl := seedRequests(h, nil, &apitool.Request{ID: "x", Method: "GET", URL: "https://example.com/x"})
			st := newRespState(tl.req, c.resp) // indexed in the send job
			tl.applyResponse(tl.respKey(), st)
			tl.focus = paneResponse
			best := time.Hour
			var view string
			for range 3 {
				start := time.Now()
				view = tl.View(h.Context(), true)
				best = min(best, time.Since(start))
			}
			if best >= 50*time.Millisecond {
				t.Fatalf("View took %v", best)
			}
			if n := strings.Count(view, "\n") + 1; n != tl.size.H {
				t.Fatalf("View has %d lines, want %d", n, tl.size.H)
			}
		})
	}
}
