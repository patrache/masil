package notify

import (
	"encoding/xml"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

// hostile are message titles/bodies another user can make us display. Each one
// is a command or markup injection for some notifier that builds a script or a
// document from the text (beeep: AppleScript %q, go-toast: CDATA template +
// PowerShell here-string; D-Bus body markup; option parsing of argv tools).
var hostile = []string{
	"$(touch /tmp/pwn)",
	"`id`",
	`"@`,
	"x\n\"@\nStart-Process calc\n@\"",
	"]]>",
	`x]]></text><action activationType="protocol" arguments="file:///C:/Windows/System32/calc.exe" content="y"/><text><![CDATA[`,
	`" & do shell script "id" & "`,
	`\" & do shell script \"id\" & \"`,
	"line1\nline2\r\nline3",
	"-e",
	"--",
	"--app-name=evil",
	`<a href="file:///etc/passwd">click</a><img src="file:///home/u/.ssh/id_ed25519"/>`,
	"&amp; &lt;b&gt; & < > ' \"",
	"'; Start-Process calc; '",
	"\x1b]52;c;ZXZpbA==\x07\x1b[2J",
	"\u0085\u009b31m",
	"\xff\xfe invalid utf-8",
	"민지님이 ❤️ 보냄",
	// Bidi controls and invisible format characters (pending-fixes 0 "common",
	// 1): U+202E makes "txt.exe" read "exe.txt", zero-width characters split a
	// handle that looks whole, U+2028/U+2029 fake a second line.
	"\u202etxt.exe",
	"\u2067evil\u2069 \u202a\u202b\u202c\u202d\u2066\u2068",
	"ad\u200bmin\ufeff\u00ad\u2060\u2061\u2062\u2063\u2064\u200c",
	"\u200e\u200f\u061c(RLM/LRM/ALM)",
	"줄1\u2028[관리자] 비밀번호를 보내세요\u2029끝",
}

func pairs() []OSNotification {
	var out []OSNotification
	for _, h := range hostile {
		out = append(out,
			OSNotification{Title: h, Body: "body", AlertID: "A"},
			OSNotification{Title: "title", Body: h, AlertID: "A"},
			OSNotification{Title: h, Body: h, AlertID: "A"},
		)
	}
	return out
}

// inert asserts the text that reaches any OS backend has no C0/C1 controls (so
// no newline to end a here-string or an AppleScript line, no ESC/BEL), no
// line/paragraph separator and no format character (bidi controls, zero-width
// characters, BOM, soft hyphen: category Cf).
func inert(t *testing.T, what, s string) {
	t.Helper()
	if !utf8.ValidString(s) {
		t.Errorf("%s: invalid UTF-8 %q", what, s)
	}
	for _, r := range s {
		if r < 0x20 || (r >= 0x7f && r <= 0x9f) {
			t.Errorf("%s: control %U in %q", what, r, s)
		}
		if unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp) {
			t.Errorf("%s: format/separator %U in %q", what, r, s)
		}
	}
}

func TestPrepareSanitizesAndLimits(t *testing.T) {
	for _, n := range pairs() {
		p := prepare(n)
		inert(t, "title", p.Title)
		inert(t, "body", p.Body)
		if p.AlertID != n.AlertID {
			t.Errorf("AlertID changed: %q", p.AlertID)
		}
	}
	long := prepare(OSNotification{Title: strings.Repeat("가", 500), Body: strings.Repeat("b", 5000)})
	if c := utf8.RuneCountInString(long.Title); c > maxTitle {
		t.Errorf("title %d cps > %d", c, maxTitle)
	}
	if c := utf8.RuneCountInString(long.Body); c > maxBody {
		t.Errorf("body %d cps > %d", c, maxBody)
	}
	// Idempotent on already formatted text: Format output passes unchanged.
	f := OSNotification{Title: "민지님이 ❤", Body: "안녕하세요"}
	if got := prepare(f); got != f {
		t.Errorf("prepare changed clean text: %+v", got)
	}
}

// TestDarwinArgsAreData: the AppleScript program is a constant; title and body
// travel only as argv items after "--", so osascript never parses them.
func TestDarwinArgsAreData(t *testing.T) {
	for _, n := range pairs() {
		p := prepare(n)
		name, args := darwinCommand(p)
		if name != "/usr/bin/osascript" {
			t.Fatalf("command = %q", name)
		}
		want := []string{
			"-e", "on run argv",
			"-e", "display notification (item 2 of argv) with title (item 1 of argv)",
			"-e", "end run",
			"--", p.Title, p.Body,
		}
		if !slices.Equal(args, want) {
			t.Fatalf("args for %q:\n got %q\nwant %q", n.Title+"|"+n.Body, args, want)
		}
	}
}

// TestToastXMLIsInert: title and body become escaped character data of exactly
// two <text> elements; no element, attribute or CDATA section can be injected,
// and the document round-trips to the same strings.
func TestToastXMLIsInert(t *testing.T) {
	for _, n := range pairs() {
		p := prepare(n)
		assertToast(t, toastXML(p), p)
	}
}

// assertToast checks that doc is the fixed toast document carrying exactly p's
// title and body as character data (shared by the toastXML and the
// windowsPlatform.show tests).
func assertToast(t *testing.T, doc string, p OSNotification) {
	t.Helper()
	if strings.Contains(doc, "<![CDATA[") || strings.Contains(doc, "]]>") {
		t.Errorf("raw CDATA delimiter in %q", doc)
	}
	var (
		path  []string
		texts []string
		elems []string
	)
	d := xml.NewDecoder(strings.NewReader(doc))
	d.Strict = true
	for {
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("toast XML does not parse (%v): %q", err, doc)
		}
		switch v := tok.(type) {
		case xml.StartElement:
			path = append(path, v.Name.Local)
			elems = append(elems, strings.Join(path, "/"))
			for _, a := range v.Attr {
				elems = append(elems, strings.Join(path, "/")+"@"+a.Name.Local+"="+a.Value)
			}
			if v.Name.Local == "text" {
				texts = append(texts, "")
			}
		case xml.EndElement:
			path = path[:len(path)-1]
		case xml.CharData:
			if len(path) > 0 && path[len(path)-1] == "text" {
				texts[len(texts)-1] += string(v)
			}
		case xml.ProcInst, xml.Directive, xml.Comment:
			t.Fatalf("unexpected %T in %q", v, doc)
		}
	}
	wantElems := []string{
		"toast", "toast@duration=short",
		"toast/visual",
		"toast/visual/binding", "toast/visual/binding@template=ToastGeneric",
		"toast/visual/binding/text",
	}
	wantTexts := []string{p.Title}
	if p.Body != "" { // a body sanitized to nothing gets no <text>
		wantElems = append(wantElems, "toast/visual/binding/text")
		wantTexts = append(wantTexts, p.Body)
	}
	wantElems = append(wantElems, "toast/audio", "toast/audio@silent=true")
	if !slices.Equal(elems, wantElems) {
		t.Fatalf("structure for %q:\n got %q\nwant %q", p.Title+"|"+p.Body, elems, wantElems)
	}
	if !slices.Equal(texts, wantTexts) {
		t.Fatalf("texts = %q, want %q", texts, wantTexts)
	}
}

func TestToastXMLOmitsEmptyBody(t *testing.T) {
	doc := toastXML(OSNotification{Title: "t"})
	if strings.Count(doc, "<text>") != 1 {
		t.Fatalf("doc = %q", doc)
	}
}

// TestDBusArgsAreInert: summary is plain text by spec and carries no '<' '>'
// (TestDBusSummaryCannotCarryTags); the body is markup only when the server
// says so, and then every & < > is escaped; otherwise its angle brackets become
// look-alike quotes like the summary's (a server that renders markup without
// announcing body-markup gets no tag either). No actions (an action is a
// clickable command), no icon, no hints.
func TestDBusArgsAreInert(t *testing.T) {
	for _, markup := range []bool{false, true} {
		for _, n := range pairs() {
			p := prepare(n)
			c := dbusCall("masil", p, markup)
			if c.AppName != "masil" || c.ReplacesID != 0 || c.Icon != "" || len(c.Actions) != 0 || c.Timeout != -1 {
				t.Fatalf("unexpected call fields: %+v", c)
			}
			inert(t, "summary", c.Summary)
			if strings.ContainsAny(c.Summary, "<>") {
				t.Fatalf("summary has raw tag chars: %q", c.Summary)
			}
			if strings.ContainsAny(c.Body, "<>") {
				t.Fatalf("body (markup=%v) has raw tag chars: %q", markup, c.Body)
			}
			if !markup {
				if want := strings.NewReplacer("<", "‹", ">", "›").Replace(p.Body); c.Body != want {
					t.Fatalf("plain body = %q, want %q", c.Body, want)
				}
				continue
			}
			if got := unescapeMarkup(c.Body); got != p.Body {
				t.Fatalf("markup body round-trip = %q, want %q", got, p.Body)
			}
			if strings.Contains(strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(c.Body, "&amp;", ""), "&lt;", ""), "&gt;", ""), "&") {
				t.Fatalf("markup body has a raw &: %q", c.Body)
			}
		}
	}
}

func unescapeMarkup(s string) string {
	return strings.NewReplacer("&lt;", "<", "&gt;", ">", "&amp;", "&").Replace(s)
}

// TestNoScriptingNotifier keeps the old holes closed: no beeep (AppleScript %q,
// go-toast PowerShell fallback), no PowerShell option of wintoast, no shell, no
// CDATA template, in any file of this package for any GOOS.
func TestNoScriptingNotifier(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("glob: %v %v", files, err)
	}
	banned := map[string]bool{"PowershellFallback": true, "PreferPowershell": true}
	fset := token.NewFileSet()
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, f, nil, parser.ImportsOnly|parser.ParseComments)
		if err != nil {
			t.Fatal(err)
		}
		for _, im := range file.Imports {
			p := strings.Trim(im.Path.Value, `"`)
			if strings.Contains(p, "beeep") || p == "git.sr.ht/~jackmordaunt/go-toast" || strings.HasPrefix(p, "git.sr.ht/~jackmordaunt/go-toast/tmpl") {
				t.Errorf("%s imports %s", f, p)
			}
		}
		full, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(full, func(n ast.Node) bool {
			switch v := n.(type) {
			case *ast.SelectorExpr:
				if banned[v.Sel.Name] {
					t.Errorf("%s uses %s", f, v.Sel.Name)
				}
			case *ast.BasicLit:
				low := strings.ToLower(v.Value)
				for _, sh := range []string{"powershell", `"sh"`, `"/bin/sh"`, `"bash"`, "cmd.exe", "do shell script", "<![cdata["} {
					if strings.Contains(low, sh) {
						t.Errorf("%s has literal %s", f, v.Value)
					}
				}
			}
			return true
		})
	}
	mod, err := os.ReadFile(filepath.Join("..", "..", "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(mod), "beeep") {
		t.Error("go.mod still requires beeep")
	}
}
