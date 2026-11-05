package ui_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// Timer rule (CA 5.3.2): UI code waits only through ui.After. These imports
// and selectors are banned outside internal/ui/timers.go and internal/ui/uitest/.
const (
	teaPath     = "charm.land/bubbletea/v2"
	timePath    = "time"
	spinnerPath = "charm.land/bubbles/v2/spinner"
)

var bannedSelectors = map[string]map[string]bool{
	teaPath:  {"Tick": true, "Every": true, "Sequence": true},
	timePath: {"Sleep": true, "After": true, "NewTimer": true},
}

// rawTimerViolations returns "file:line: what" for every banned use in one file.
func rawTimerViolations(fset *token.FileSet, f *ast.File) []string {
	var out []string
	report := func(p token.Pos, what string) {
		pos := fset.Position(p)
		out = append(out, fmt.Sprintf("%s:%d: %s — use ui.After (CA 5.3.2)", pos.Filename, pos.Line, what))
	}
	names := map[string]string{} // local import name -> path
	importsSpinner := false
	for _, imp := range f.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		if path == spinnerPath {
			importsSpinner = true
		}
		if _, banned := bannedSelectors[path]; !banned {
			continue
		}
		name := ""
		if imp.Name != nil {
			name = imp.Name.Name
		} else if path == teaPath {
			name = "tea"
		} else {
			name = "time"
		}
		switch name {
		case "_":
			continue
		case ".":
			report(imp.Pos(), "dot import of "+path)
			continue
		}
		names[name] = path
	}
	ast.Inspect(f, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.SelectorExpr:
			id, ok := n.X.(*ast.Ident)
			// Obj == nil: the identifier is not a locally declared object, i.e. a package name.
			if !ok || id.Obj != nil {
				return true
			}
			if path, ok := names[id.Name]; ok && bannedSelectors[path][n.Sel.Name] {
				report(n.Pos(), id.Name+"."+n.Sel.Name)
			}
		case *ast.CallExpr:
			if !importsSpinner {
				return true
			}
			if sel, ok := n.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Tick" {
				report(n.Pos(), "spinner Tick (use widgets.Spinner)")
			}
		}
		return true
	})
	return out
}

func parseSrc(t *testing.T, src string) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	return rawTimerViolations(fset, f)
}

// The checker itself must catch every banned form and nothing else.
func TestRawTimerCheckerDetects(t *testing.T) {
	bad := map[string]string{
		"tea.Tick":      `package p; import tea "charm.land/bubbletea/v2"; var _ = tea.Tick(0, nil)`,
		"tea.Every":     `package p; import tea "charm.land/bubbletea/v2"; var _ = tea.Every(0, nil)`,
		"tea.Sequence":  `package p; import tea "charm.land/bubbletea/v2"; var _ = tea.Sequence()`,
		"aliased tea":   `package p; import bt "charm.land/bubbletea/v2"; var _ = bt.Tick(0, nil)`,
		"unaliased tea": `package p; import "charm.land/bubbletea/v2"; var _ = tea.Tick(0, nil)`,
		"time.Sleep":    `package p; import "time"; func f() { time.Sleep(1) }`,
		"time.After":    `package p; import "time"; func f() { <-time.After(1) }`,
		"time.NewTimer": `package p; import "time"; func f() { time.NewTimer(1) }`,
		"aliased time":  `package p; import tm "time"; func f() { tm.Sleep(1) }`,
		"func value":    `package p; import "time"; var s = time.Sleep`,
		"dot import":    `package p; import . "time"; func f() { Sleep(1) }`,
		"spinner tick":  `package p; import "charm.land/bubbles/v2/spinner"; func f(m spinner.Model) { m.Tick() }`,
	}
	for name, src := range bad {
		if v := parseSrc(t, src); len(v) == 0 {
			t.Errorf("%s: not detected", name)
		} else if !strings.Contains(v[0], "use ui.After (CA 5.3.2)") || !strings.Contains(v[0], "x.go:1") {
			t.Errorf("%s: message %q lacks file:line or hint", name, v[0])
		}
	}
	ok := map[string]string{
		"tea.Batch":           `package p; import tea "charm.land/bubbletea/v2"; var _ = tea.Batch()`,
		"time.Now":            `package p; import "time"; var _ = time.Now()`,
		"local shadow":        `package p; import "time"; type c struct{}; func (c) Sleep(int) {}; func f() { var time c; time.Sleep(1) }`,
		"tick w/o spinner":    `package p; type c struct{}; func (c) Tick() {}; func f(x c) { x.Tick() }`,
		"time not imported":   `package p; type c struct{}; func (c) After(int) {}; var time c; var _ = func() int { time.After(1); return 0 }`,
		"spinner frames only": `package p; import "charm.land/bubbles/v2/spinner"; var _ = spinner.Dot`,
	}
	for name, src := range ok {
		if v := parseSrc(t, src); len(v) != 0 {
			t.Errorf("%s: false positive %v", name, v)
		}
	}
}

func TestNoRawTimersInUI(t *testing.T) {
	// This test runs in internal/ui; walk the whole tree (chat/feed/fun included when present).
	root := "."
	fset := token.NewFileSet()
	var violations []string
	scanned := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel := filepath.ToSlash(path)
		if d.IsDir() {
			if rel == "uitest" || d.Name() == "testdata" || strings.HasPrefix(d.Name(), ".") && rel != "." {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") || rel == "timers.go" {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		f, err := parser.ParseFile(fset, "internal/ui/"+rel, src, 0)
		if err != nil {
			return err
		}
		scanned++
		violations = append(violations, rawTimerViolations(fset, f)...)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if scanned < 10 {
		t.Fatalf("scanned only %d files under internal/ui; walk root is wrong", scanned)
	}
	if len(violations) > 0 {
		sort.Strings(violations)
		t.Errorf("raw timers in UI code:\n%s", strings.Join(violations, "\n"))
	}
}
