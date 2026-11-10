package i18n_test

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// The Korean string-literal ratchet (i18n spec 9.3): per package, the number of
// Go string literals containing Hangul may only go down. Packages not listed in
// legacy_literals.txt (every new 0.1.3 package) must have none — use the catalog.

var writeRatchet = flag.Bool("write-ratchet", false,
	"create legacy_literals.txt from current counts, or lower it (never raises)")

const ratchetFile = "legacy_literals.txt"

const modulePath = "github.com/patrache/masil"

func TestHangulLiteralRatchet(t *testing.T) {
	root := repoRoot(t)
	counts, err := countHangulLiterals(root)
	if err != nil {
		t.Fatal(err)
	}
	if *writeRatchet {
		var old []ratchetLine
		if data, err := os.ReadFile(ratchetFile); err == nil {
			if old, err = parseRatchet(data); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(ratchetFile, renderRatchet(lowerRatchet(old, counts)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(ratchetFile)
	if err != nil {
		t.Fatalf("%s missing: %v (create it with -args -write-ratchet)", ratchetFile, err)
	}
	lines, err := parseRatchet(data)
	if err != nil {
		t.Fatal(err)
	}
	allowed := map[string]int{}
	for _, l := range lines {
		allowed[l.Path] = l.N
	}
	for _, pkg := range slices.Sorted(maps.Keys(unionKeys(counts, allowed))) {
		got, max := counts[pkg], allowed[pkg]
		switch {
		case got > max:
			t.Errorf("%s: %d Korean literals, ratchet allows %d — use the catalog", pkg, got, max)
		case got < max:
			t.Errorf("%s: down to %d, lower legacy_literals.txt (-write-ratchet)", pkg, got)
		}
	}
}

func TestRatchetFileFormat(t *testing.T) {
	good := "# comment\ninternal/a 3\n# permanent: stays\ninternal/b 1\n"
	if _, err := parseRatchet([]byte(good)); err != nil {
		t.Fatalf("good file rejected: %v", err)
	}
	for name, body := range map[string]string{
		"module prefix": modulePath + "/internal/a 3\n",
		"unsorted":      "internal/b 1\ninternal/a 3\n",
		"duplicate":     "internal/a 1\ninternal/a 1\n",
		"bad count":     "internal/a x\n",
		"zero":          "internal/a 0\n",
		"extra field":   "internal/a 1 2\n",
	} {
		if _, err := parseRatchet([]byte(body)); err == nil {
			t.Errorf("%s: %q accepted", name, body)
		}
	}
	// The committed file passes the same rules.
	if data, err := os.ReadFile(ratchetFile); err == nil {
		if _, err := parseRatchet(data); err != nil {
			t.Fatalf("%s: %v", ratchetFile, err)
		}
	}
}

func TestRatchetWriteOnlyLowers(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ratchetFile)
	old := "# header\n\ninternal/down 5\n# permanent: operator CLI\ninternal/keep 2\ninternal/up 1\ninternal/zero 4\n"
	if err := os.WriteFile(p, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	lines, err := parseRatchet([]byte(old))
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{"internal/down": 3, "internal/keep": 2, "internal/up": 4, "internal/new": 7}
	got := string(renderRatchet(lowerRatchet(lines, counts)))
	want := "# header\n\ninternal/down 3\n# permanent: operator CLI\ninternal/keep 2\ninternal/up 1\n"
	if got != want {
		t.Fatalf("lowered =\n%s\nwant\n%s", got, want)
	}
	// Creating from nothing writes current counts, sorted, zeros skipped.
	got = string(renderRatchet(lowerRatchet(nil, map[string]int{"b/x": 2, "a/y": 1, "c": 0})))
	if got != "a/y 1\nb/x 2\n" {
		t.Fatalf("created = %q", got)
	}
}

func TestHangulLiteralCounting(t *testing.T) {
	src := "package p\n// 주석은 세지 않는다\nvar a = \"한글\"\nvar b = `백틱 한글`\nvar c = \"english\"\nvar d = 'ㄱ'\nvar e = \"ㅋㅋ\"\nvar f = \"\\uAC00\"\n"
	n, err := hangulLiterals([]byte(src), "p.go")
	if err != nil {
		t.Fatal(err)
	}
	if n != 4 {
		t.Fatalf("hangulLiterals = %d, want 4 (a, b, e, f)", n)
	}
	for rel, want := range map[string]bool{
		"internal/ui/chat/x.go":         false,
		"internal/ui/chat/x_test.go":    true,
		"internal/i18n/zz_tools_gen.go": true,
		"internal/foo/foo_gen.go":       true,
		"internal/ui/uitest/harness.go": true,
		"internal/i18n/locale.go":       true,
		"cmd/masil/testdata/x.go":       true,
		"server/node_modules/a/b.go":    true,
		".claude/worktrees/x/a.go":      true,
	} {
		if got := isExcluded(rel); got != want {
			t.Errorf("isExcluded(%q) = %v, want %v", rel, got, want)
		}
	}
}

// --- helpers shared with P5's TestNoPackageLevelCatalog ---

// repoRoot walks up from the test's directory to the go.mod.
func repoRoot(t testing.TB) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}

// isExcluded reports whether a repo-relative slash path is outside the ratchet:
// tests, generated files, testdata, the test harness, i18n itself, node_modules
// and dot directories (.claude worktrees, .git, .github).
func isExcluded(rel string) bool {
	if strings.HasSuffix(rel, "_test.go") || strings.HasSuffix(rel, "_gen.go") {
		return true
	}
	if strings.HasPrefix(rel, "internal/ui/uitest/") || strings.HasPrefix(rel, "internal/i18n/") {
		return true
	}
	for _, part := range strings.Split(path.Dir(rel), "/") {
		if part == "testdata" || part == "node_modules" || (strings.HasPrefix(part, ".") && part != ".") {
			return true
		}
	}
	return false
}

// walkGoSources calls fn with every non-excluded .go file (repo-relative slash path).
func walkGoSources(root string, fn func(rel string) error) error {
	return filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel != "." && isExcluded(rel+"/x.go") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(rel, ".go") || isExcluded(rel) {
			return nil
		}
		return fn(rel)
	})
}

// hangulLiterals counts string literals (quoted or raw) containing Hangul.
func hangulLiterals(src []byte, filename string) (int, error) {
	f, err := parser.ParseFile(token.NewFileSet(), filename, src, parser.SkipObjectResolution)
	if err != nil {
		return 0, err
	}
	n := 0
	ast.Inspect(f, func(node ast.Node) bool {
		lit, ok := node.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		s, err := strconv.Unquote(lit.Value)
		if err != nil {
			s = lit.Value
		}
		if hasHangul(s) {
			n++
		}
		return true
	})
	return n, nil
}

func hasHangul(s string) bool {
	for _, r := range s {
		if (r >= 0xAC00 && r <= 0xD7A3) || (r >= 0x1100 && r <= 0x11FF) || (r >= 0x3130 && r <= 0x318F) {
			return true
		}
	}
	return false
}

// countHangulLiterals returns package dir => count for packages with any.
func countHangulLiterals(root string) (map[string]int, error) {
	counts := map[string]int{}
	err := walkGoSources(root, func(rel string) error {
		src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			return err
		}
		n, err := hangulLiterals(src, rel)
		if err != nil {
			return err
		}
		if n > 0 {
			counts[path.Dir(rel)] += n
		}
		return nil
	})
	return counts, err
}

// ratchetLine is one "path count" line with the comment lines right above it.
type ratchetLine struct {
	Header   []string // file header block (first line only), kept by rewrites
	Comments []string
	Path     string
	N        int
}

func parseRatchet(data []byte) ([]ratchetLine, error) {
	var out []ratchetLine
	var comments, headerComments []string
	sc := bufio.NewScanner(bytes.NewReader(data))
	for no := 1; sc.Scan(); no++ {
		line := strings.TrimSpace(sc.Text())
		switch {
		case line == "":
			if len(out) == 0 && len(comments) > 0 && headerComments == nil {
				headerComments = comments // the file's header block
			}
			comments = nil
			continue
		case strings.HasPrefix(line, "#"):
			comments = append(comments, line)
			continue
		}
		f := strings.Fields(line)
		if len(f) != 2 {
			return nil, fmt.Errorf("%s:%d: want \"<package path> <count>\", got %q", ratchetFile, no, line)
		}
		n, err := strconv.Atoi(f[1])
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("%s:%d: count %q must be a positive integer (delete the line at 0)", ratchetFile, no, f[1])
		}
		if strings.HasPrefix(f[0], modulePath) || strings.Contains(f[0], ".") {
			return nil, fmt.Errorf("%s:%d: %q: drop the module prefix (internal/ui/chat, not %s/internal/ui/chat)", ratchetFile, no, f[0], modulePath)
		}
		if len(out) > 0 {
			prev := out[len(out)-1].Path
			if f[0] == prev {
				return nil, fmt.Errorf("%s:%d: %s listed twice", ratchetFile, no, f[0])
			}
			if f[0] < prev {
				return nil, fmt.Errorf("%s:%d: %s must sort before %s", ratchetFile, no, f[0], prev)
			}
		}
		out = append(out, ratchetLine{Comments: comments, Path: f[0], N: n})
		comments = nil
	}
	if len(out) > 0 && headerComments != nil {
		out[0].Header = headerComments
	}
	return out, sc.Err()
}

// lowerRatchet applies -write-ratchet: with no file, today's counts; with a
// file, each count can only go down and lines reaching 0 go away. Packages
// that grew or are new keep their old allowance (0 when absent).
func lowerRatchet(old []ratchetLine, counts map[string]int) []ratchetLine {
	if old == nil {
		var out []ratchetLine
		for _, p := range slices.Sorted(maps.Keys(counts)) {
			if counts[p] > 0 {
				out = append(out, ratchetLine{Path: p, N: counts[p]})
			}
		}
		return out
	}
	var out []ratchetLine
	header := old[0].Header
	for _, l := range old {
		l.Header = nil
		l.N = min(l.N, counts[l.Path])
		if l.N > 0 {
			out = append(out, l)
		}
	}
	if len(out) > 0 {
		out[0].Header = header
	}
	return out
}

func renderRatchet(lines []ratchetLine) []byte {
	var b bytes.Buffer
	for _, l := range lines {
		if len(l.Header) > 0 {
			b.WriteString(strings.Join(l.Header, "\n") + "\n\n")
		}
		for _, c := range l.Comments {
			b.WriteString(c + "\n")
		}
		fmt.Fprintf(&b, "%s %d\n", l.Path, l.N)
	}
	return b.Bytes()
}

func unionKeys(a, b map[string]int) map[string]bool {
	out := map[string]bool{}
	for k := range a {
		out[k] = true
	}
	for k := range b {
		out[k] = true
	}
	return out
}
