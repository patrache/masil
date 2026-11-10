package lsp

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/patrache/masil/internal/leetcode"
)

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func TestUTF16Columns(t *testing.T) {
	// U+D55C U+AE00 are Hangul (one unit each); U+1F600 is an emoji (two units).
	line := "a\uD55C\uAE00\U0001F600b"
	cases := []struct{ runes, units int }{
		{0, 0}, {1, 1}, {2, 2}, {3, 3}, {4, 5}, {5, 6},
	}
	for _, c := range cases {
		if got := UTF16Col(line, c.runes); got != c.units {
			t.Errorf("UTF16Col(%d) = %d, want %d", c.runes, got, c.units)
		}
		if got := RuneCol(line, c.units); got != c.runes {
			t.Errorf("RuneCol(%d) = %d, want %d", c.units, got, c.runes)
		}
	}
	if got := RuneCol(line, 4); got != 3 {
		t.Errorf("inside the surrogate pair: RuneCol(4) = %d, want 3 (the emoji's start)", got)
	}
	if UTF16Col(line, 99) != 6 || RuneCol(line, 99) != 5 || UTF16Col(line, -1) != 0 || RuneCol(line, -1) != 0 {
		t.Error("clamping")
	}
	if UTF16Col("", 3) != 0 || RuneCol("", 3) != 0 {
		t.Error("empty line")
	}
	bad := "a\xffb" // an invalid byte is one rune and one unit
	if UTF16Col(bad, 3) != 3 || RuneCol(bad, 2) != 2 {
		t.Error("invalid UTF-8")
	}
	// Family emoji: several code points joined, each above U+FFFF except the joiners.
	fam := "\U0001F468\u200D\U0001F469x"
	if UTF16Col(fam, 3) != 5 || RuneCol(fam, 5) != 3 || UTF16Col(fam, 4) != 6 {
		t.Errorf("ZWJ sequence: %d %d", UTF16Col(fam, 3), RuneCol(fam, 5))
	}
}

func TestFraming(t *testing.T) {
	b, err := encodeFrame(outNotification{JSONRPC: "2.0", Method: "x", Params: map[string]string{"k": "\uD55C"}})
	if err != nil {
		t.Fatal(err)
	}
	body := `{"jsonrpc":"2.0","method":"x","params":{"k":"` + "\uD55C" + `"}}`
	if want := "Content-Length: " + itoa(len(body)) + "\r\n\r\n" + body; string(b) != want {
		t.Fatalf("frame %q, want %q (length in bytes)", b, want)
	}
	stream := string(b) + "content-length: 2\r\nContent-Type: application/vscode-jsonrpc; charset=utf-8\r\n\r\n{}" +
		"Content-Length: 99\r\n\r\n{}"
	r := bufio.NewReader(strings.NewReader(stream))
	m, err := readMessage(r)
	if err != nil || m.Method != "x" {
		t.Fatalf("first: %+v %v", m, err)
	}
	if m, err := readMessage(r); err != nil || m.Method != "" {
		t.Fatalf("second (lower-case header, extra header): %+v %v", m, err)
	}
	if _, err := readMessage(r); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("truncated body: %v", err)
	}
	if _, err := readMessage(bufio.NewReader(strings.NewReader(""))); err != io.EOF {
		t.Errorf("clean EOF: %v", err)
	}
	for _, bad := range []string{"Content-Length: x\r\n\r\n", "garbage\r\n\r\n", "Content-Length: -1\r\n\r\n"} {
		if _, err := readMessage(bufio.NewReader(strings.NewReader(bad))); !errors.Is(err, errFrame) {
			t.Errorf("%q: %v", bad, err)
		}
	}
}

func itoa(n int) string {
	var b []byte
	for {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
		if n == 0 {
			return string(b)
		}
	}
}

func TestParseCompletionEdgeCases(t *testing.T) {
	if l, err := parseCompletion([]byte("null")); err != nil || len(l.Items) != 0 {
		t.Errorf("null: %+v %v", l, err)
	}
	// InsertReplaceEdit: the insert range is used.
	raw := `[{"label":"foo","textEdit":{"newText":"foo","insert":{"start":{"line":0,"character":1},"end":{"line":0,"character":2}},"replace":{"start":{"line":0,"character":1},"end":{"line":0,"character":5}}}}]`
	l, err := parseCompletion([]byte(raw))
	if err != nil || len(l.Items) != 1 || l.Items[0].Edit == nil || l.Items[0].Edit.Range.End.Character != 2 {
		t.Fatalf("insert/replace: %+v %v", l, err)
	}
	if _, err := parseCompletion([]byte(`"nope"`)); err == nil {
		t.Error("a string result parsed")
	}
	var many strings.Builder
	many.WriteString("[")
	for i := range maxItems + 5 {
		if i > 0 {
			many.WriteString(",")
		}
		many.WriteString(`{"label":"x"}`)
	}
	many.WriteString("]")
	l, _ = parseCompletion([]byte(many.String()))
	if len(l.Items) != maxItems || !l.Incomplete {
		t.Errorf("cap: %d items, incomplete %v", len(l.Items), l.Incomplete)
	}
}

func TestStripSnippet(t *testing.T) {
	for in, want := range map[string]string{
		"plain":                   "plain",
		"f($1)$0":                 "f()",
		"f(${1:a}, ${2:b})":       "f(a, b)",
		"${1|one,two|} x":         "one x",
		`cost \$5 \}`:             "cost $5 }",
		"if ${1:cond} {\n\t$0\n}": "if cond {\n\t\n}",
		"${1:outer ${2:inner}}":   "outer inner",
		"$":                       "$",
		"${TM_FILENAME}":          "",
	} {
		if got := stripSnippet(in); got != want {
			t.Errorf("stripSnippet(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestKindString(t *testing.T) {
	if KindText.String() != "text" || KindTypeParameter.String() != "typeParameter" || CompletionItemKind(99).String() != "" || CompletionItemKind(-1).String() != "" {
		t.Error("kind names")
	}
}

func TestURIs(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a b", "\uD55C.go")
	u := URIFromPath(p)
	if !strings.HasPrefix(u, "file:///") || strings.Contains(u, " ") {
		t.Errorf("uri %q", u)
	}
	back, err := PathFromURI(u)
	if err != nil || back != filepath.Clean(p) {
		t.Errorf("round trip %q -> %q -> %q (%v)", p, u, back, err)
	}
	if runtime.GOOS == "windows" {
		if got := URIFromPath(`C:\x\y.go`); got != "file:///C:/x/y.go" {
			t.Errorf("windows uri %q", got)
		}
	}
	if _, err := PathFromURI("http://x/y"); err == nil {
		t.Error("http URI accepted")
	}
}

func TestScrubEnvNamesMatchLeetCode(t *testing.T) {
	for _, n := range []string{leetcode.EnvSession, leetcode.EnvCSRF, leetcode.EnvCFClearance} {
		if !slices.Contains(scrubbedEnv, n) {
			t.Errorf("%s is not scrubbed", n)
		}
	}
	got := scrubEnv([]string{"PATH=/bin", "leetcode_session=x", "LEETCODE_CSRFTOKEN=y", "HOME=/h", "LEETCODE_SESSIONX=keep"})
	if want := []string{"PATH=/bin", "HOME=/h", "LEETCODE_SESSIONX=keep"}; !slices.Equal(got, want) {
		t.Errorf("scrubEnv = %v, want %v", got, want)
	}
}

func TestLanguages(t *testing.T) {
	for path, want := range map[string]string{
		"a/main.go": "go", "x.PY": "python", "s.cpp": "cpp", "s.c": "c", "s.rs": "rust", "s.ts": "typescript",
		"s.tsx": "typescriptreact", "s.js": "javascript", "S.java": "java", "s.cs": "csharp", "s.php": "php",
		"s.swift": "swift", "s.kt": "kotlin", "s.rb": "ruby", "s.scala": "scala", "s.txt": "", "Makefile": "",
	} {
		if got := LangForPath(path); got != want {
			t.Errorf("LangForPath(%q) = %q, want %q", path, got, want)
		}
	}
	// Every LeetCode language either maps to a language with a server or is
	// one of the known server-less ones; a new LeetCode language fails here.
	none := []string{"dart", "racket", "erlang", "elixir"}
	for _, l := range leetcode.Languages {
		lang := LangForLeetCode(l.Slug)
		if lang == "" {
			if !slices.Contains(none, l.Slug) {
				t.Errorf("LeetCode %s has no language", l.Slug)
			}
			continue
		}
		if _, ok := serverTable[lang]; !ok {
			t.Errorf("LeetCode %s -> %s has no server table entry", l.Slug, lang)
		}
		if got := LangForPath("solution" + l.Ext); got != lang {
			t.Errorf("LeetCode %s: ext %s -> %q, slug -> %q", l.Slug, l.Ext, got, lang)
		}
	}
}

func TestFind(t *testing.T) {
	onPath := map[string]string{
		"gopls": "/bin/gopls", "basedpyright-langserver": "/bin/basedpyright-langserver",
		"pylsp": "/bin/pylsp", "my-gopls": "/opt/my-gopls", "clangd": "/bin/clangd",
		"typescript-language-server": "/bin/tsls",
	}
	old := lookPath
	lookPath = func(name string) (string, error) {
		if p, ok := onPath[name]; ok {
			return filepath.FromSlash(p), nil
		}
		return "", errors.New("not found")
	}
	t.Cleanup(func() { lookPath = old; SetOverrides(nil) })
	abs := func(p string) string { a, _ := filepath.Abs(filepath.FromSlash(p)); return a }

	s, ok := Find("go")
	if !ok || s.Name != "gopls" || !slices.Equal(s.Command, []string{abs("/bin/gopls")}) || s.Lang != "go" {
		t.Errorf("go: %+v %v", s, ok)
	}
	// pyright-langserver is missing: the second python command wins, with its args.
	s, ok = Find("python")
	if !ok || s.Name != "basedpyright-langserver" || !slices.Equal(s.Command, []string{abs("/bin/basedpyright-langserver"), "--stdio"}) {
		t.Errorf("python: %+v %v", s, ok)
	}
	if s, ok := Find("cpp"); !ok || s.Name != "clangd" {
		t.Errorf("cpp: %+v", s)
	}
	if _, ok := Find("rust"); ok {
		t.Error("rust found without rust-analyzer")
	}
	if _, ok := Find("cobol"); ok {
		t.Error("unknown language found")
	}

	SetOverrides(map[string][]string{"Go": {"my-gopls", "-remote=auto"}, "rust": {"missing-ra"}, "typescript": {"typescript-language-server", "--stdio", "--log-level=4"}, "c": {}})
	if s, ok := Find("go"); !ok || !slices.Equal(s.Command, []string{abs("/opt/my-gopls"), "-remote=auto"}) {
		t.Errorf("override: %+v", s)
	}
	if _, ok := Find("rust"); ok {
		t.Error("a missing override program with no table fallback on PATH was found")
	}
	if s, ok := Find("typescriptreact"); !ok || len(s.Command) != 3 {
		t.Errorf("typescript override covers tsx: %+v", s)
	}
	if s, ok := Find("c"); !ok || s.Name != "clangd" {
		t.Errorf("empty override ignored: %+v", s)
	}
}

func TestStderrLogCap(t *testing.T) {
	var buf syncBuffer
	s := &stderrLog{log: slog.New(slog.NewTextHandler(&buf, nil)), left: maxStderrLines}
	for range maxStderrLines + 50 {
		_, _ = s.Write([]byte("noise\r\n"))
	}
	_, _ = s.Write([]byte("par"))
	out := buf.String()
	if n := strings.Count(out, "line=noise"); n != maxStderrLines {
		t.Errorf("%d lines logged, want %d", n, maxStderrLines)
	}
	if !strings.Contains(out, "further lines dropped") {
		t.Error("no drop note")
	}
}
