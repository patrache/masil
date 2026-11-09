package i18n_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
)

// Strings frozen at package level (a var initializer or init()) stay in the
// language the process started in after a live switch (P5 Review Focus 1).
// Registries take func(t *i18n.T) string instead.

func TestNoPackageLevelCatalog(t *testing.T) {
	root := repoRoot(t)
	err := walkGoSources(root, func(rel string) error {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, filepath.Join(root, filepath.FromSlash(rel)), nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		for _, p := range packageLevelLookups(f) {
			pos := fset.Position(p)
			t.Errorf("%s:%d: package-level catalog lookup; pass func(t *i18n.T) string", rel, pos.Line)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestNoPackageLevelCatalogDetects(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("testdata", "pkglevel", "pkglevel.go"))
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "pkglevel.go", src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	var lines []int
	for _, p := range packageLevelLookups(f) {
		lines = append(lines, fset.Position(p).Line)
	}
	// var x = i18n.For(...) (line 9), init's i18n.KoT() (line 11) and the two
	// func literals called on the spot (lines 17, 19); f's body and the held
	// func literal are allowed.
	if want := []int{9, 11, 17, 19}; !slices.Equal(lines, want) {
		t.Fatalf("lookups at lines %v, want %v", lines, want)
	}
}

// packageLevelLookups returns the positions of i18n.For(…) and i18n.KoT()
// calls that run at package initialization: inside a package-level var
// initializer or a func init() body. Function literals are skipped (their body
// runs when called, not at init) unless called on the spot (func(){…}()).
func packageLevelLookups(f *ast.File) []token.Pos {
	name := i18nImportName(f)
	if name == "" {
		return nil
	}
	var out []token.Pos
	called := map[*ast.FuncLit]bool{}
	find := func(n ast.Node) {
		ast.Inspect(n, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.FuncLit:
				return called[n]
			case *ast.CallExpr:
				fun := n.Fun
				for p, ok := fun.(*ast.ParenExpr); ok; p, ok = fun.(*ast.ParenExpr) {
					fun = p.X
				}
				if lit, ok := fun.(*ast.FuncLit); ok {
					called[lit] = true
					return true
				}
				sel, ok := fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				if id, ok := sel.X.(*ast.Ident); ok && id.Name == name && (sel.Sel.Name == "For" || sel.Sel.Name == "KoT") {
					out = append(out, n.Pos())
				}
			}
			return true
		})
	}
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *ast.GenDecl:
			if d.Tok != token.VAR {
				continue
			}
			for _, s := range d.Specs {
				for _, v := range s.(*ast.ValueSpec).Values {
					find(v)
				}
			}
		case *ast.FuncDecl:
			if d.Recv == nil && d.Name.Name == "init" && d.Body != nil {
				find(d.Body)
			}
		}
	}
	return out
}

// i18nImportName is the name the file uses for the i18n package ("" = not imported).
func i18nImportName(f *ast.File) string {
	for _, imp := range f.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil || p != modulePath+"/internal/i18n" {
			continue
		}
		if imp.Name != nil {
			if imp.Name.Name == "_" || imp.Name.Name == "." {
				return ""
			}
			return imp.Name.Name
		}
		return "i18n"
	}
	return ""
}
