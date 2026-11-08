package api_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Pending-fixes 5 (review follow-up): pathMethods only finds methods that
// call escPath with a value. A path built some other way — "/v1/x/" + id,
// fmt.Sprintf("/v1/x/%s", id), or a request whose Path is neither a literal
// nor escPath(...) — would escape both that scan and escPath's escaping
// (checkPath stops ".." but not a value holding "/"). Request paths are
// therefore only ever literals or escPath calls; FetchMedia's checked
// server URL is the one exception.

// pathLint returns "file:line: why" for every request path in dir's non-test
// Go files that is not built from literals and escPath. A Path set from a
// variable is allowed when every assignment to it is a literal or escPath, or
// when it is a parameter of an unexported helper whose every caller passes one.
func pathLint(t *testing.T, dir string) []string {
	t.Helper()
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, func(fi fs.FileInfo) bool { return !strings.HasSuffix(fi.Name(), "_test.go") }, 0)
	if err != nil {
		t.Fatal(err)
	}
	var files []*ast.File
	var funcs []*ast.FuncDecl
	for _, p := range pkgs {
		for _, f := range p.Files {
			files = append(files, f)
			for _, d := range f.Decls {
				if fd, ok := d.(*ast.FuncDecl); ok && fd.Body != nil {
					funcs = append(funcs, fd)
				}
			}
		}
	}
	isV1Lit := func(e ast.Expr) bool {
		bl, ok := e.(*ast.BasicLit)
		return ok && bl.Kind == token.STRING && strings.HasPrefix(strings.Trim(bl.Value, "`\""), "/v1")
	}
	safe := func(e ast.Expr) bool {
		if bl, ok := e.(*ast.BasicLit); ok {
			return bl.Kind == token.STRING
		}
		if call, ok := e.(*ast.CallExpr); ok {
			id, ok := call.Fun.(*ast.Ident)
			return ok && id.Name == "escPath"
		}
		return false
	}
	// paramIndex is the position of name among fd's parameters, or -1.
	paramIndex := func(fd *ast.FuncDecl, name string) int {
		k := 0
		for _, field := range fd.Type.Params.List {
			for _, n := range field.Names {
				if n.Name == name {
					return k
				}
				k++
			}
		}
		return -1
	}
	// callersPassSafe: every call of fd in the package passes a literal or
	// escPath at argument k (and there is at least one call).
	callersPassSafe := func(fd *ast.FuncDecl, k int) bool {
		calls := 0
		ok := true
		for _, f := range files {
			ast.Inspect(f, func(n ast.Node) bool {
				call, isCall := n.(*ast.CallExpr)
				if !isCall {
					return true
				}
				name := ""
				switch fn := call.Fun.(type) {
				case *ast.Ident:
					if fd.Recv == nil {
						name = fn.Name
					}
				case *ast.SelectorExpr:
					if fd.Recv != nil {
						name = fn.Sel.Name
					}
				}
				if name != fd.Name.Name {
					return true
				}
				calls++
				if k >= len(call.Args) || !safe(call.Args[k]) {
					ok = false
				}
				return true
			})
		}
		return calls > 0 && ok
	}
	// assignedSafe: name is assigned in fd's body, only ever a literal or escPath.
	assignedSafe := func(fd *ast.FuncDecl, name string) bool {
		seen, ok := 0, true
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			as, isAssign := n.(*ast.AssignStmt)
			if !isAssign {
				return true
			}
			for i, l := range as.Lhs {
				if id, isIdent := l.(*ast.Ident); isIdent && id.Name == name {
					seen++
					if len(as.Rhs) != len(as.Lhs) || !safe(as.Rhs[i]) {
						ok = false
					}
				}
			}
			return true
		})
		return seen > 0 && ok
	}

	var bad []string
	report := func(n ast.Node, why string) {
		p := fset.Position(n.Pos())
		bad = append(bad, fmt.Sprintf("%s:%d: %s", filepath.Base(p.Filename), p.Line, why))
	}
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			switch v := n.(type) {
			case *ast.BinaryExpr:
				if v.Op == token.ADD && isV1Lit(v.X) {
					if _, lit := v.Y.(*ast.BasicLit); !lit {
						report(v, "request path concatenated with a value (use escPath)")
					}
				}
			case *ast.CallExpr:
				if sel, ok := v.Fun.(*ast.SelectorExpr); ok && len(v.Args) > 0 && isV1Lit(v.Args[0]) {
					if x, ok := sel.X.(*ast.Ident); ok && x.Name == "fmt" {
						report(v, "request path formatted with fmt (use escPath)")
					}
				}
			}
			return true
		})
	}
	for _, fd := range funcs {
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			cl, ok := n.(*ast.CompositeLit)
			if !ok {
				return true
			}
			if id, ok := cl.Type.(*ast.Ident); !ok || id.Name != "request" {
				return true
			}
			for _, el := range cl.Elts {
				kv, ok := el.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				if k, ok := kv.Key.(*ast.Ident); !ok || k.Name != "Path" {
					continue
				}
				switch val := kv.Value.(type) {
				case *ast.BinaryExpr: // reported above when it starts with "/v1"
					continue
				case *ast.Ident:
					if fd.Name.Name == "FetchMedia" && val.Name == "mediaURL" { // checkMediaPath'd server URL
						continue
					}
					if k := paramIndex(fd, val.Name); k >= 0 {
						if !ast.IsExported(fd.Name.Name) && callersPassSafe(fd, k) {
							continue
						}
					} else if assignedSafe(fd, val.Name) {
						continue
					}
				default:
					if safe(val) {
						continue
					}
				}
				report(kv, "request Path is neither a literal nor escPath(...)")
			}
			return true
		})
	}
	return bad
}

func TestRequestPathsOnlyFromEscPath(t *testing.T) {
	for _, b := range pathLint(t, ".") {
		t.Error(b)
	}
}

// TestPathLintCatchesOtherPathBuilders runs the lint on code that builds
// paths the three forbidden ways.
func TestPathLintCatchesOtherPathBuilders(t *testing.T) {
	dir := t.TempDir()
	src := `package api

import "fmt"

type request struct{ Method, Path string }

func escPath(segs ...string) string { return "" }

func ok(id string) []request {
	return []request{{Path: "/v1/meta"}, {Path: escPath("v1", "x", id)}}
}

func concat(id string) request { return request{Method: "GET", Path: "/v1/invites/" + id} }

func sprintf(id string) string { return fmt.Sprintf("/v1/invites/%s", id) }

func field(p string) request { return request{Path: p} }

func helper(p string) request { return request{Path: p} }

func Exported(id string) []request {
	path := escPath("v1", "y", id)
	return []request{helper("/v1/a"), helper(escPath("v1", id)), {Path: path}}
}

func leaky(id string) request {
	path := escPath("v1", "y", id)
	path = path + id
	return request{Path: path}
}

func viaHelper(p string) request { return request{Path: p} }

func callsViaHelper(id string) request { return viaHelper("/v1/x" + id) }
`
	if err := os.WriteFile(filepath.Join(dir, "x.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	got := pathLint(t, dir)
	want := []string{
		"x.go:13: request path concatenated", "x.go:15: request path formatted", "x.go:34: request path concatenated",
		"x.go:17: request Path is neither", "x.go:29: request Path is neither", "x.go:32: request Path is neither",
	}
	if len(got) != len(want) {
		t.Fatalf("lint = %q, want %d findings", got, len(want))
	}
	for i := range want {
		if !strings.HasPrefix(got[i], want[i]) {
			t.Errorf("finding %d = %q, want prefix %q", i, got[i], want[i])
		}
	}
}
