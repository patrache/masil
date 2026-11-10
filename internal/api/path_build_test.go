package api_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"io/fs"
	"sort"
	"strings"
	"testing"
)

// TestPathCallsCoverEveryPathMethod only sees caller values that reach a
// path through escPath. A path built another way ("/v1/x/" + id,
// fmt.Sprintf) would slip past it, and checkPath stops ".." but not a value
// holding "/" (pending-fixes 5). TestRequestPathsOnlyFromLiteralsOrEscPath
// keeps every request path in this package to a string literal or escPath.

// pathBuildAllowed are the functions that may join strings with a "/..."
// literal: they extend the configured base URL, never a caller value
// (MediaURL, WebSocketURL), or only compare against it (checkMediaPath).
// DocSocketURL and WorkspaceShareWSURL are dial URLs, not request paths: their
// one caller value is url.PathEscape'd and dot segments give ""
// (TestDocSocketURL, TestWorkspaceShareWSURL).
var pathBuildAllowed = map[string]bool{"MediaURL": true, "WebSocketURL": true, "DocSocketURL": true, "WorkspaceShareWSURL": true, "checkMediaPath": true}

// pathParamAllowed are the exported methods that may pass a parameter
// straight into request.Path / request.URL: FetchMedia takes a whole
// server-supplied URL, which checkMediaPath limits on the API origin.
var pathParamAllowed = map[string]bool{"FetchMedia": true}

func TestRequestPathsOnlyFromLiteralsOrEscPath(t *testing.T) {
	for _, v := range scanPathBuilds(t, ".") {
		t.Errorf("%s", v)
	}
}

// TestPathBuildScanFlagsConcatenation: the scanner flags every way of
// building a path the fixture tries except literals and escPath (directly,
// in a local, or through an unexported helper's call sites).
func TestPathBuildScanFlagsConcatenation(t *testing.T) {
	got := map[string]bool{}
	for _, v := range scanPathBuilds(t, "testdata/pathbuild") {
		got[strings.SplitN(v, ": ", 3)[1]] = true
	}
	for _, m := range []string{"Concat", "Sprintf", "LocalConcat", "AppendLater", "RawParam", "ViaBadHelper", "WithURL", "SetLater"} {
		if !got[m] {
			t.Errorf("scanner missed %s (flagged %v)", m, got)
		}
	}
	for _, m := range []string{"Literal", "Escaped", "LocalEscaped", "ViaHelperLiteral", "ViaHelperEscaped", "helper", "FetchMedia", "badHelper"} {
		if got[m] {
			t.Errorf("scanner flagged %s, which builds its path the allowed way", m)
		}
	}
}

// scanPathBuilds reports, as "file:line: func: why", every place in the Go
// package in dir where a request path is not a string literal or an escPath
// call: request{Path: ...} / x.Path = ... with another expression, a local
// or parameter fed from one, a request{URL: ...} outside pathParamAllowed,
// and any "+" or fmt.Sprintf joining a "/..." literal outside
// pathBuildAllowed.
func scanPathBuilds(t *testing.T, dir string) []string {
	t.Helper()
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, func(fi fs.FileInfo) bool { return !strings.HasSuffix(fi.Name(), "_test.go") }, 0)
	if err != nil {
		t.Fatal(err)
	}
	var decls []*ast.FuncDecl
	for _, p := range pkgs {
		for _, f := range p.Files {
			for _, d := range f.Decls {
				if fd, ok := d.(*ast.FuncDecl); ok && fd.Body != nil {
					decls = append(decls, fd)
				}
			}
		}
	}
	if len(decls) == 0 {
		t.Fatalf("no functions parsed in %s", dir)
	}
	var out []string
	report := func(fd *ast.FuncDecl, n ast.Node, why string, args ...any) {
		out = append(out, fmt.Sprintf("%s: %s: %s", fset.Position(n.Pos()), fd.Name.Name, fmt.Sprintf(why, args...)))
	}
	src := types.ExprString
	isStrLit := func(e ast.Expr) bool {
		l, ok := e.(*ast.BasicLit)
		return ok && l.Kind == token.STRING
	}
	isEscPath := func(e ast.Expr) bool {
		c, ok := e.(*ast.CallExpr)
		if !ok {
			return false
		}
		id, ok := c.Fun.(*ast.Ident)
		return ok && id.Name == "escPath"
	}
	okExpr := func(e ast.Expr) bool { return isStrLit(e) || isEscPath(e) }
	slashLit := func(e ast.Expr) bool {
		l, ok := e.(*ast.BasicLit)
		return ok && l.Kind == token.STRING && strings.HasPrefix(strings.Trim(l.Value, "`\""), "/")
	}

	// params maps a function key ("f" / ".m") to its parameter names in order.
	key := func(fd *ast.FuncDecl) string {
		if fd.Recv != nil {
			return "." + fd.Name.Name
		}
		return fd.Name.Name
	}
	params := map[string][]string{}
	for _, fd := range decls {
		var names []string
		for _, f := range fd.Type.Params.List {
			if len(f.Names) == 0 {
				names = append(names, "_")
			}
			for _, n := range f.Names {
				names = append(names, n.Name)
			}
		}
		params[key(fd)] = names
	}
	paramIndex := func(fd *ast.FuncDecl, name string) int {
		for i, n := range params[key(fd)] {
			if n == name {
				return i
			}
		}
		return -1
	}

	// needOK[k][i]: every call site of unexported function k must pass a
	// literal or escPath as argument i (it reaches request.Path as is).
	needOK := map[string]map[int]bool{}

	// checkIdent: a request path given as an identifier must be a local fed
	// only by literals / escPath, or a parameter (see needOK).
	checkIdent := func(fd *ast.FuncDecl, n ast.Node, id *ast.Ident, field string) {
		if i := paramIndex(fd, id.Name); i >= 0 {
			switch {
			case ast.IsExported(fd.Name.Name) && pathParamAllowed[fd.Name.Name]:
			case ast.IsExported(fd.Name.Name):
				report(fd, n, "request.%s is parameter %s of an exported method", field, id.Name)
			default:
				if needOK[key(fd)] == nil {
					needOK[key(fd)] = map[int]bool{}
				}
				needOK[key(fd)][i] = true
			}
			return
		}
		ast.Inspect(fd.Body, func(m ast.Node) bool {
			switch s := m.(type) {
			case *ast.AssignStmt:
				for i, l := range s.Lhs {
					if li, ok := l.(*ast.Ident); ok && li.Name == id.Name {
						if s.Tok != token.DEFINE && s.Tok != token.ASSIGN || len(s.Rhs) != len(s.Lhs) || !okExpr(s.Rhs[i]) {
							report(fd, s, "request.%s comes from %s, which is assigned something other than a literal or escPath", field, id.Name)
						}
					}
				}
			case *ast.ValueSpec:
				for i, vn := range s.Names {
					if vn.Name == id.Name && i < len(s.Values) && !okExpr(s.Values[i]) {
						report(fd, s, "request.%s comes from %s, which is assigned something other than a literal or escPath", field, id.Name)
					}
				}
			}
			return true
		})
	}
	checkPathExpr := func(fd *ast.FuncDecl, n ast.Node, e ast.Expr, field string) {
		switch v := e.(type) {
		case *ast.Ident:
			checkIdent(fd, n, v, field)
		default:
			if !okExpr(e) {
				report(fd, n, "request.%s is built from %s, not a literal or escPath", field, src(e))
			}
		}
	}

	for _, fd := range decls {
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			switch v := n.(type) {
			case *ast.CompositeLit:
				if id, ok := v.Type.(*ast.Ident); !ok || id.Name != "request" {
					return true
				}
				for _, el := range v.Elts {
					kv, ok := el.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					switch k, _ := kv.Key.(*ast.Ident); {
					case k == nil:
					case k.Name == "Path":
						checkPathExpr(fd, kv, kv.Value, "Path")
					case k.Name == "URL" && !pathParamAllowed[fd.Name.Name]:
						report(fd, kv, "request.URL is set outside %v", pathParamAllowed)
					}
				}
			case *ast.AssignStmt: // x.Path = ... (request or url.URL alike)
				if pathBuildAllowed[fd.Name.Name] {
					return true
				}
				for i, l := range v.Lhs {
					if sel, ok := l.(*ast.SelectorExpr); ok && sel.Sel.Name == "Path" && i < len(v.Rhs) {
						checkPathExpr(fd, v, v.Rhs[i], "Path")
					}
				}
			case *ast.BinaryExpr:
				if v.Op == token.ADD && !pathBuildAllowed[fd.Name.Name] && (slashLit(v.X) || slashLit(v.Y)) {
					report(fd, v, `"+" joins a "/..." literal: build request paths with escPath`)
				}
			case *ast.CallExpr:
				if sel, ok := v.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Sprintf" && len(v.Args) > 0 && slashLit(v.Args[0]) && !pathBuildAllowed[fd.Name.Name] {
					report(fd, v, `fmt.Sprintf formats a "/..." path: build request paths with escPath`)
				}
			}
			return true
		})
	}

	// Call sites of helpers that pass a parameter into request.Path.
	for _, fd := range decls {
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			k := ""
			switch f := call.Fun.(type) {
			case *ast.Ident:
				k = f.Name
			case *ast.SelectorExpr:
				k = "." + f.Sel.Name
			case *ast.IndexExpr: // generic helper[T](...)
				if id, ok := f.X.(*ast.Ident); ok {
					k = id.Name
				}
			}
			for i := range needOK[k] {
				if i < len(call.Args) && !okExpr(call.Args[i]) {
					report(fd, call, "passes %s to %s, whose argument %d becomes request.Path: use a literal or escPath", src(call.Args[i]), strings.TrimPrefix(k, "."), i)
				}
			}
			return true
		})
	}
	sort.Strings(out)
	return out
}
