package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/api/apitest"
	"github.com/patrache/masil/internal/errtext"
	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/ui"
)

func TestCLIWorkspace(t *testing.T) {
	srv := apitest.New(t)
	e := newEnv(t, srv.URL)
	calls := fakeTUI(t)

	t.Setenv("MASIL_FEATURES", "")
	if code := e.run("workspace", "open", t.TempDir()); code != 2 || len(*calls) != 0 {
		t.Fatalf("flag off: open => %d (tui calls %d), want 2", code, len(*calls))
	}
	t.Setenv("MASIL_FEATURES", "workspace")

	if code := e.run("workspace", "open"); code != 2 {
		t.Fatalf("open without a path => %d, want 2", code)
	}
	if code := e.run("workspace", "open", filepath.Join(t.TempDir(), "not", "a", "dir")); code != 2 {
		t.Fatalf("open of a missing folder => %d, want 2", code)
	}
	file := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := e.run("workspace", "open", file); code != 2 {
		t.Fatalf("open of a file => %d, want 2", code)
	}
	if code := e.run("workspace"); code != 2 {
		t.Fatalf("workspace alone => %d, want 2", code)
	}
	if len(*calls) != 0 {
		t.Fatalf("usage errors started the TUI")
	}

	dir := t.TempDir()
	if code := e.run("workspace", "open", dir); code != 0 {
		t.Fatalf("open %s => %d, stderr %q", dir, code, e.err)
	}
	if len(*calls) != 1 {
		t.Fatalf("runTUI calls = %d", len(*calls))
	}
	abs, _ := filepath.Abs(dir)
	r := (*calls)[0].InitialRoute
	if r == nil || r.Kind != ui.RouteTool || r.Arg != ui.ToolWorkspace || r.Query["open"] != abs {
		t.Fatalf("InitialRoute = %+v", r)
	}

	// stop: logged out is an error, not a request.
	if code := e.run("workspace", "stop"); code != 1 {
		t.Fatalf("stop logged out => %d, want 1", code)
	}
	if n := len(srv.Requests()); n != 0 {
		t.Fatalf("logged-out stop sent %d requests", n)
	}

	saveCreds(t, e.Paths, srv.URL)
	srv.JSON("GET /v1/workspace-shares", 200, map[string]any{"items": []api.WorkspaceShare{
		{ID: "ws_a", Status: "live"}, {ID: "ws_b", Status: "live"},
	}, "next_cursor": nil})
	srv.JSON("DELETE /v1/workspace-shares/{id}", 204, nil)
	if code := e.run("workspace", "stop"); code != 0 {
		t.Fatalf("stop => %d, stderr %q", code, e.err)
	}
	var deletes []string
	var listQuery string
	for _, q := range srv.Requests() {
		switch q.Method {
		case "DELETE":
			deletes = append(deletes, q.Path)
		case "GET":
			listQuery = q.Query
		}
	}
	if len(deletes) != 2 || deletes[0] != "/v1/workspace-shares/ws_a" || deletes[1] != "/v1/workspace-shares/ws_b" {
		t.Fatalf("DELETEs = %v", deletes)
	}
	if listQuery != "role=owner&status=live" {
		t.Fatalf("list query = %q", listQuery)
	}
	if got := e.out.String(); got != "공유 2개를 끝냈어요\n" {
		t.Fatalf("stdout = %q", got)
	}

	if code := e.run("workspace", "stop", "ws_c"); code != 0 {
		t.Fatalf("stop ws_c => %d, stderr %q", code, e.err)
	}
	reqs := srv.Requests()
	if last := reqs[len(reqs)-1]; last.Method != "DELETE" || last.Path != "/v1/workspace-shares/ws_c" {
		t.Fatalf("last request = %s %s", last.Method, last.Path)
	}
	if code := e.run("workspace", "stop", "a", "b"); code != 2 {
		t.Fatalf("stop with two ids => %d, want 2", code)
	}

	srv.JSON("GET /v1/workspace-shares", 200, map[string]any{"items": []api.WorkspaceShare{}, "next_cursor": nil})
	if code := e.run("workspace", "stop"); code != 0 {
		t.Fatalf("stop with none => %d", code)
	}
	if got := e.out.String(); got != "끝낼 공유가 없어요\n" {
		t.Fatalf("stdout = %q", got)
	}
}

// A failed end is said with errtext.Describe (the catalog-driven error
// line), and the stop exits 1.
func TestCLIWorkspaceStopFailure(t *testing.T) {
	srv := apitest.New(t)
	e := newEnv(t, srv.URL)
	t.Setenv("MASIL_FEATURES", "workspace")
	saveCreds(t, e.Paths, srv.URL)
	srv.JSON("DELETE /v1/workspace-shares/{id}", 500, map[string]any{"error": map[string]any{"code": "internal_error", "message": "boom"}})
	if code := e.run("workspace", "stop", "ws_x"); code != 1 {
		t.Fatalf("failed stop => %d, want 1", code)
	}
	tr := i18n.For(i18n.Ko)
	want := tr.Workspace.CliStopFailed(errtext.Describe(tr, &api.Error{Status: 500, Code: "internal_error", Message: "boom"}))
	if got := e.err.String(); !strings.Contains(got, want) {
		t.Fatalf("stderr = %q, want %q", got, want)
	}
}
