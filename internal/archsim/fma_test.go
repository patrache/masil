package archsim

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// sources parses the non-test .go files of dir with absolute file names.
func sources(t *testing.T, fset *token.FileSet, dir string) []*ast.File {
	t.Helper()
	abs, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	ents, err := os.ReadDir(abs)
	if err != nil {
		t.Fatal(err)
	}
	var files []*ast.File
	for _, e := range ents {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(abs, n), nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, f)
	}
	return files
}

func isFloat(info *types.Info, e ast.Expr) bool {
	tv, ok := info.Types[e]
	if !ok || tv.Value != nil { // constants fold at compile time
		return false
	}
	b, ok := tv.Type.Underlying().(*types.Basic)
	return ok && b.Info()&types.IsFloat != 0
}

// fmaSites returns file:line of every float multiplication a compiler may fuse
// into a multiply-add. The Go spec allows fusion even across statements
// ("t = x*y; r = t + z"); only an explicit conversion rounds the product. So a
// non-constant float product must be the direct operand of a conversion
// (float64(x*y)), of another multiplication or division, or of a comparison.
// Anything else — x*y + z, z -= x*y, t := x*y, return x*y, f(x*y), x *= y — is
// reported.
func fmaSites(t *testing.T, dir string) []string {
	t.Helper()
	fset := token.NewFileSet()
	files := sources(t, fset, dir)
	info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}}
	conf := types.Config{Importer: importer.ForCompiler(fset, "source", nil)}
	if _, err := conf.Check("archsim", fset, files, info); err != nil {
		t.Fatalf("type check %s: %v", dir, err)
	}
	var out []string
	report := func(n ast.Node) {
		p := fset.Position(n.Pos())
		out = append(out, filepath.Base(p.Filename)+":"+strconv.Itoa(p.Line))
	}
	safeParent := func(stack []ast.Node) bool {
		for i := len(stack) - 1; i >= 0; i-- {
			switch p := stack[i].(type) {
			case *ast.ParenExpr:
				continue
			case *ast.CallExpr:
				return info.Types[p.Fun].IsType() // a conversion rounds
			case *ast.BinaryExpr:
				switch p.Op {
				case token.MUL, token.QUO, token.EQL, token.NEQ, token.LSS, token.LEQ, token.GTR, token.GEQ:
					return true
				}
			}
			return false
		}
		return false
	}
	for _, f := range files {
		ast.PreorderStack(f, nil, func(n ast.Node, stack []ast.Node) bool {
			switch x := n.(type) {
			case *ast.BinaryExpr:
				if x.Op == token.MUL && isFloat(info, x) && !safeParent(stack) {
					report(x)
				}
			case *ast.AssignStmt:
				if x.Tok == token.MUL_ASSIGN && isFloat(info, x.Lhs[0]) {
					report(x)
				}
			}
			return true
		})
	}
	return out
}

func TestNoFusedMultiplyAdd(t *testing.T) {
	if sites := fmaSites(t, "."); len(sites) > 0 {
		t.Fatalf("fusable float multiply-add (wrap the product in float64(...)):\n%s", strings.Join(sites, "\n"))
	}
}

// TestFMADetectorFindsFusable keeps the detector honest on a known-bad file.
func TestFMADetectorFindsFusable(t *testing.T) {
	dir := t.TempDir()
	src := `package p

func f(a, b, c float64, n int) (float64, int, bool) {
	x := a*b + c         // bad: x*y + z
	y := c - (a * b)     // bad
	z := float64(a*b) + c
	w := -a*b + c        // bad: (-a)*b + c
	c += a * b           // bad
	t := a * b           // bad: may fuse with a later add
	c *= a               // bad
	k := n*n + n         // ints never fuse
	const q = 2.0*3 + 1  // constant
	u := a * b / c       // division in between
	v := a*b < c         // comparison
	return x + y + z + w + c + q + t + u, k, v
}
`
	if err := os.WriteFile(filepath.Join(dir, "p.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	got := fmaSites(t, dir)
	want := []string{"p.go:4", "p.go:5", "p.go:7", "p.go:8", "p.go:9", "p.go:10"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("sites = %v, want %v", got, want)
	}
}

func TestNoForbiddenMath(t *testing.T) {
	forbidden := map[string]bool{
		"Exp": true, "Exp2": true, "Expm1": true, "Log": true, "Log10": true, "Log1p": true, "Log2": true, "Logb": true,
		"Pow": true, "Pow10": true, "Sin": true, "Cos": true, "Tan": true, "Sincos": true, "Asin": true, "Acos": true,
		"Atan": true, "Atan2": true, "Sinh": true, "Cosh": true, "Tanh": true,
	}
	err := filepath.WalkDir(".", func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == "testdata" {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(token.NewFileSet(), p, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		mathName := ""
		for _, imp := range f.Imports {
			path, _ := strconv.Unquote(imp.Path.Value)
			if path == "math/rand" || path == "math/rand/v2" {
				t.Errorf("%s imports %s", p, path)
			}
			if path == "math" {
				mathName = "math"
				if imp.Name != nil {
					mathName = imp.Name.Name
				}
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok {
				if id, ok := sel.X.(*ast.Ident); ok && mathName != "" && id.Name == mathName && forbidden[sel.Sel.Name] {
					t.Errorf("%s uses math.%s (architecture-specific results)", p, sel.Sel.Name)
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
