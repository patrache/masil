package notify

import (
	"context"
	"errors"
	"go/ast"
	"go/build/constraint"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// These tests drive each platform's show — the code that really reaches the
// OS — through osBackend.Show with hostile text, with only the last OS call
// (exec, wintoast, the D-Bus connection) replaced. Testing the builders alone
// would let show stop using them (pending-fixes 0 review: pasting the text
// into an AppleScript string or a CDATA template inside show passed every
// builder test). They run on every GOOS: platform show code lives in
// backend_platform.go, which has no build constraint; the GOOS files only wire
// the real OS calls in (TestPlatformFilesOnlyWire).

type runCall struct {
	name     string
	args     []string
	deadline bool
}

// TestDarwinShowRunsConstantOsascript: show execs /usr/bin/osascript with the
// constant program and the prepared title and body as the two argv items after
// "--" — no other process, no shell, no text in the program.
func TestDarwinShowRunsConstantOsascript(t *testing.T) {
	var calls []runCall
	run := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		_, ok := ctx.Deadline()
		calls = append(calls, runCall{name, slices.Clone(args), ok})
		return nil, nil
	}
	b := osBackend{appName: "masil", p: darwinPlatform{run: run}}
	for _, n := range pairs() {
		calls = nil
		if err := b.Show(n); err != nil {
			t.Fatal(err)
		}
		if len(calls) != 1 {
			t.Fatalf("show ran %d processes, want 1", len(calls))
		}
		p := prepare(n)
		c := calls[0]
		want := []string{
			"-e", "on run argv",
			"-e", "display notification (item 2 of argv) with title (item 1 of argv)",
			"-e", "end run",
			"--", p.Title, p.Body,
		}
		if c.name != "/usr/bin/osascript" || !slices.Equal(c.args, want) {
			t.Fatalf("show ran %q %q\nwant /usr/bin/osascript %q", c.name, c.args, want)
		}
		if gotName, gotArgs := darwinCommand(p); gotName != c.name || !slices.Equal(gotArgs, c.args) {
			t.Fatalf("show did not use darwinCommand: %q %q vs %q %q", c.name, c.args, gotName, gotArgs)
		}
		if !c.deadline {
			t.Fatal("osascript ran without a timeout")
		}
		inert(t, "title argv", c.args[7])
		inert(t, "body argv", c.args[8])
	}
}

func TestDarwinShowReportsFailureWithoutOutput(t *testing.T) {
	run := func(context.Context, string, ...string) ([]byte, error) {
		return []byte("secret text"), errors.New("exit status 1")
	}
	err := darwinPlatform{run: run}.show(OSNotification{Title: "t", Body: "b"})
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("err = %v", err)
	}
}

// TestWindowsShowPushesEscapedToast: show sets the app id and pushes exactly
// toastXML of the prepared text — a fixed document with the text as escaped
// character data, never CDATA.
func TestWindowsShowPushesEscapedToast(t *testing.T) {
	var ids, docs []string
	w := windowsPlatform{
		appName:  "masil",
		setAppID: func(id string) error { ids = append(ids, id); return nil },
		push:     func(doc string) error { docs = append(docs, doc); return nil },
	}
	b := osBackend{appName: "masil", p: w}
	for _, n := range pairs() {
		ids, docs = nil, nil
		if err := b.Show(n); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(ids, []string{"masil"}) || len(docs) != 1 {
			t.Fatalf("app ids %q, %d documents pushed", ids, len(docs))
		}
		p := prepare(n)
		if docs[0] != toastXML(p) {
			t.Fatalf("show did not push toastXML:\n got %q\nwant %q", docs[0], toastXML(p))
		}
		assertToast(t, docs[0], p)
	}
}

func TestWindowsShowStopsOnAppIDError(t *testing.T) {
	pushed := 0
	w := windowsPlatform{
		appName:  "masil",
		setAppID: func(string) error { return errors.New("registry") },
		push:     func(string) error { pushed++; return nil },
	}
	if err := w.show(OSNotification{Title: "t"}); err == nil || pushed != 0 {
		t.Fatalf("err = %v, pushed %d", err, pushed)
	}
	w.setAppID = func(string) error { return nil }
	w.push = func(string) error { return errors.New("com") }
	if err := w.show(OSNotification{Title: "t"}); err == nil {
		t.Fatal("push error dropped")
	}
}

// fakeConn is a D-Bus connection to a notification server.
type fakeConn struct {
	fakeBus
	dead      bool
	closed    int
	notifyErr error
}

func (f *fakeConn) notify(ctx context.Context, c dbusNotify) error {
	if f.notifyErr != nil {
		return f.notifyErr
	}
	return f.fakeBus.notify(ctx, c)
}
func (f *fakeConn) alive() bool { return !f.dead && f.closed == 0 }
func (f *fakeConn) close()      { f.closed++ }

// TestDBusShowSendsBuiltCall: show sends exactly dbusCall of the prepared text
// with the capabilities the server reports for that notification; neither
// summary nor body carries '<' '>' in any mode.
func TestDBusShowSendsBuiltCall(t *testing.T) {
	conn := &fakeConn{}
	dials := 0
	p := &dbusPlatform{appName: "masil", dial: func() (dbusConn, error) { dials++; return conn, nil }}
	b := osBackend{appName: "masil", p: p}
	for i, n := range pairs() {
		markup := i%2 == 1
		conn.caps = []string{"body"}
		if markup {
			conn.caps = append(conn.caps, "body-markup")
		}
		conn.sent = nil
		if err := b.Show(n); err != nil {
			t.Fatal(err)
		}
		if len(conn.sent) != 1 {
			t.Fatalf("%d Notify calls", len(conn.sent))
		}
		got, want := conn.sent[0], dbusCall("masil", prepare(n), markup)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("show sent %+v\nwant %+v", got, want)
		}
		if strings.ContainsAny(got.Summary+got.Body, "<>") {
			t.Fatalf("tag chars reached the server: %+v", got)
		}
		inert(t, "summary", got.Summary)
		inert(t, "body", got.Body)
	}
	if dials != 1 {
		t.Fatalf("dialed %d times for one live connection", dials)
	}
}

// TestDBusShowReconnects: a failed call or a dead connection is closed and
// redialed on the next notification; a dial error is returned.
func TestDBusShowReconnects(t *testing.T) {
	var conns []*fakeConn
	dialErr := error(nil)
	p := &dbusPlatform{appName: "masil", dial: func() (dbusConn, error) {
		if dialErr != nil {
			return nil, dialErr
		}
		c := &fakeConn{}
		conns = append(conns, c)
		return c, nil
	}}
	n := OSNotification{Title: "t", Body: "b"}
	if err := p.show(n); err != nil {
		t.Fatal(err)
	}
	conns[0].notifyErr = errors.New("gone")
	if err := p.show(n); err == nil {
		t.Fatal("notify error dropped")
	}
	if conns[0].closed != 1 {
		t.Fatalf("failed connection closed %d times", conns[0].closed)
	}
	if err := p.show(n); err != nil || len(conns) != 2 || len(conns[1].sent) != 1 {
		t.Fatalf("no redial after failure: err %v, %d dials", err, len(conns))
	}
	conns[1].dead = true
	if err := p.show(n); err != nil || len(conns) != 3 || len(conns[2].sent) != 1 {
		t.Fatalf("no redial after disconnect: err %v, %d dials", err, len(conns))
	}
	conns[2].dead = true
	dialErr = errors.New("no session bus")
	if err := p.show(n); !errors.Is(err, dialErr) {
		t.Fatalf("err = %v, want the dial error", err)
	}
}

// TestPlatformFilesOnlyWire pins the split the tests above rely on, so a show
// that bypasses its builder cannot hide in a GOOS-only file this machine never
// compiles:
//   - every platform's show is declared in backend_platform.go (the stub in
//     backend_other.go only returns an error);
//   - no GOOS-constrained file names OSNotification or a text builder, so the
//     user's text reaches them only as the builders' output;
//   - os/exec is used only by execRun, which runs exactly the name and argv it
//     is given;
//   - wintoast.Push gets one plain argument (the document; no options).
func TestPlatformFilesOnlyWire(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("glob: %v %v", files, err)
	}
	fset := token.NewFileSet()
	builders := map[string]bool{"OSNotification": true, "darwinCommand": true, "toastXML": true, "writeText": true, "dbusCall": true, "summaryText": true, "escapeMarkup": true, "dbusShow": true, "prepare": true, "clamp": true}
	sawExecRun, sawPush := false, false
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, f, nil, parser.ParseComments)
		if err != nil {
			t.Fatal(err)
		}
		constrained := false
		for _, cg := range file.Comments {
			for _, c := range cg.List {
				if constraint.IsGoBuild(c.Text) {
					constrained = true
				}
			}
		}
		for _, im := range file.Imports {
			if strings.Trim(im.Path.Value, `"`) == "os/exec" && f != "backend_platform.go" {
				t.Errorf("%s imports os/exec (only execRun in backend_platform.go may)", f)
			}
		}
		for _, d := range file.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok {
				continue
			}
			if fd.Recv != nil && fd.Name.Name == "show" && f != "backend_platform.go" && f != "backend_other.go" {
				t.Errorf("%s declares a show method; platform show code belongs in backend_platform.go", f)
			}
			if fd.Recv == nil && fd.Name.Name == "execRun" {
				sawExecRun = true
				checkExecRun(t, fd)
			}
			ast.Inspect(fd, func(n ast.Node) bool {
				switch v := n.(type) {
				case *ast.SelectorExpr:
					if x, ok := v.X.(*ast.Ident); ok && x.Name == "exec" && fd.Name.Name != "execRun" {
						t.Errorf("%s: %s uses exec.%s outside execRun", f, fd.Name.Name, v.Sel.Name)
					}
				case *ast.CallExpr:
					if sel, ok := v.Fun.(*ast.SelectorExpr); ok {
						if x, ok := sel.X.(*ast.Ident); ok && x.Name == "wintoast" && sel.Sel.Name == "Push" {
							sawPush = true
							if len(v.Args) != 1 || v.Ellipsis.IsValid() {
								t.Errorf("%s: wintoast.Push must get only the document", f)
							} else if _, ok := v.Args[0].(*ast.Ident); !ok {
								t.Errorf("%s: wintoast.Push argument must be the document parameter, not an expression", f)
							}
						}
					}
				}
				return true
			})
		}
		if constrained && f != "backend_other.go" {
			ast.Inspect(file, func(n ast.Node) bool {
				if id, ok := n.(*ast.Ident); ok && builders[id.Name] {
					t.Errorf("%s (GOOS-only) refers to %s; it may only wire OS calls", f, id.Name)
				}
				return true
			})
		}
	}
	if !sawExecRun {
		t.Error("execRun not found")
	}
	if !sawPush {
		t.Error("no wintoast.Push call found (windows wiring moved?)")
	}
}

// checkExecRun: the body is exactly
// return exec.CommandContext(ctx, name, args...).CombinedOutput().
func checkExecRun(t *testing.T, fd *ast.FuncDecl) {
	t.Helper()
	bad := func() { t.Errorf("execRun must be `return exec.CommandContext(ctx, name, args...).CombinedOutput()`") }
	if len(fd.Body.List) != 1 {
		bad()
		return
	}
	ret, ok := fd.Body.List[0].(*ast.ReturnStmt)
	if !ok || len(ret.Results) != 1 {
		bad()
		return
	}
	outer, ok := ret.Results[0].(*ast.CallExpr)
	if !ok || len(outer.Args) != 0 {
		bad()
		return
	}
	sel, ok := outer.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "CombinedOutput" {
		bad()
		return
	}
	inner, ok := sel.X.(*ast.CallExpr)
	if !ok || !inner.Ellipsis.IsValid() || len(inner.Args) != 3 {
		bad()
		return
	}
	if s, ok := inner.Fun.(*ast.SelectorExpr); !ok || s.Sel.Name != "CommandContext" {
		bad()
		return
	}
	for i, want := range []string{"ctx", "name", "args"} {
		if id, ok := inner.Args[i].(*ast.Ident); !ok || id.Name != want {
			bad()
			return
		}
	}
}
