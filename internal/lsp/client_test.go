package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// startFake starts the fake in a temp root with the fake clock (debounce only
// fires on Advance) and registers a Shutdown.
func startFake(t *testing.T, clk Clock, modes ...string) *Client {
	t.Helper()
	if clk == nil {
		clk = newFakeClock()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c, err := Options{Clock: clk, Logger: slog.New(slog.DiscardHandler)}.Start(ctx, fakeServer(t, modes...), t.TempDir())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = c.Shutdown(context.Background()) })
	return c
}

func state(t *testing.T, c *Client) fakeState {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	raw, err := c.call(ctx, "fake/state", nil)
	if err != nil {
		t.Fatalf("fake/state: %v", err)
	}
	var st fakeState
	if err := json.Unmarshal(raw, &st); err != nil {
		t.Fatal(err)
	}
	return st
}

func TestInitializeHandshake(t *testing.T) {
	c := startFake(t, nil)
	st := state(t, c)
	if !st.Initialized {
		t.Error("initialized notification not sent")
	}
	var p struct {
		ProcessID    int    `json:"processId"`
		RootURI      string `json:"rootUri"`
		Capabilities struct {
			TextDocument struct {
				Completion struct {
					CompletionItem struct {
						SnippetSupport *bool `json:"snippetSupport"`
					} `json:"completionItem"`
				} `json:"completion"`
			} `json:"textDocument"`
		} `json:"capabilities"`
	}
	if err := json.Unmarshal(st.Init, &p); err != nil {
		t.Fatal(err)
	}
	if s := p.Capabilities.TextDocument.Completion.CompletionItem.SnippetSupport; s == nil || *s {
		t.Errorf("snippetSupport = %v, want false", s)
	}
	if p.RootURI != URIFromPath(c.Root()) || p.ProcessID == 0 {
		t.Errorf("rootUri %q processId %d", p.RootURI, p.ProcessID)
	}
	if got := c.TriggerCharacters(); len(got) != 1 || got[0] != "." {
		t.Errorf("trigger characters %v", got)
	}
	if want := []string{"initialize", "initialized", "fake/state"}; strings.Join(st.Methods, " ") != strings.Join(want, " ") {
		t.Errorf("methods %v, want %v", st.Methods, want)
	}
}

func TestServerRequestsAnswered(t *testing.T) {
	c := startFake(t, nil, "ask") // initialize only answers once all three are answered
	st := state(t, c)
	if got := st.Answers["c1"]; got != "[null,null]" {
		t.Errorf("workspace/configuration answer %q", got)
	}
	if got := st.Answers["2"]; got != "null" {
		t.Errorf("workDoneProgress/create answer %q", got)
	}
	if got := st.Answers["3"]; got != "error -32601" {
		t.Errorf("unknown method answer %q", got)
	}
}

func TestDocumentSync(t *testing.T) {
	clk := newFakeClock()
	c := startFake(t, clk)
	uri := URIFromPath(filepath.Join(c.Root(), "main.go"))
	c.DidOpen(uri, "go", "package main\n")
	if d := state(t, c).Docs[uri]; d.Version != 1 || d.Text != "package main\n" || d.Lang != "go" {
		t.Fatalf("after open: %+v", d)
	}

	// Changes are debounced: nothing is sent until 150 ms after the last one,
	// and only the last text goes out, as version 2.
	c.DidChange(uri, "package main\n\nfunc a() {}\n")
	clk.Advance(100 * time.Millisecond)
	c.DidChange(uri, "package main\n\nfunc b() {}\n")
	clk.Advance(100 * time.Millisecond) // 200 ms after the first change, 100 after the last
	if d := state(t, c).Docs[uri]; d.Version != 1 {
		t.Fatalf("change sent before the debounce: %+v", d)
	}
	clk.Advance(50 * time.Millisecond)
	if d := state(t, c).Docs[uri]; d.Version != 2 || !strings.Contains(d.Text, "func b") {
		t.Fatalf("after debounce: %+v", d)
	}
	if c.Version(uri) != 2 {
		t.Errorf("Version = %d", c.Version(uri))
	}

	// A change to a document that is not open is dropped.
	other := URIFromPath(filepath.Join(c.Root(), "other.go"))
	c.DidChange(other, "x")
	clk.Advance(time.Second)
	if _, ok := state(t, c).Docs[other]; ok {
		t.Error("change to an unopened document was sent")
	}

	// Close drops a pending change and closes the document.
	c.DidChange(uri, "dropped")
	c.DidClose(uri)
	clk.Advance(time.Second)
	st := state(t, c)
	if _, ok := st.Docs[uri]; ok {
		t.Error("document still open after DidClose")
	}
	if n := strings.Count(strings.Join(st.Methods, " "), "textDocument/didChange"); n != 1 {
		t.Fatalf("%d didChange sent (methods %v): the pending change must be dropped by close", n, st.Methods)
	}
	if c.Version(uri) != 0 {
		t.Error("closed document keeps a version")
	}
}

func TestCompleteSendsPendingChange(t *testing.T) {
	c := startFake(t, nil) // the fake clock never advances: only Complete can flush
	uri := URIFromPath(filepath.Join(c.Root(), "main.go"))
	c.DidOpen(uri, "go", "")
	c.DidChange(uri, "fmt.Pr")
	items, err := c.Complete(context.Background(), uri, 0, 6)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) == 0 || items[0].Detail != "prefix=Pr" {
		t.Fatalf("items %+v: completion did not see the pending text", items)
	}
}

func TestCompleteShapes(t *testing.T) {
	cases := []struct {
		modes      []string
		incomplete bool
		edit       bool
		insert     string
	}{
		{modes: nil, insert: "Println"},
		{modes: []string{"array"}, insert: "Println"},
		{modes: []string{"incomplete"}, incomplete: true, insert: "Println"},
		{modes: []string{"textedit"}, edit: true, insert: "Println"},
		{modes: []string{"snippet"}, insert: "Println(a, )"},
	}
	for _, tc := range cases {
		t.Run(strings.Join(append([]string{"list"}, tc.modes...), "+"), func(t *testing.T) {
			c := startFake(t, nil, tc.modes...)
			uri := URIFromPath(filepath.Join(c.Root(), "main.go"))
			line := "\tx := \"\uD55C\uAE00\U0001F600\" + fmt.Pr" // Hangul and an emoji before the word
			c.DidOpen(uri, "go", "package main\n"+line+"\n")
			col := UTF16Col(line, len([]rune(line)))
			l, err := c.CompleteList(context.Background(), uri, 1, col)
			if err != nil {
				t.Fatal(err)
			}
			if l.Incomplete != tc.incomplete {
				t.Errorf("Incomplete = %v", l.Incomplete)
			}
			if len(l.Items) != 2 {
				t.Fatalf("items %+v (the unlabeled one must be dropped)", l.Items)
			}
			it := l.Items[0]
			if it.Label != "Println" || l.Items[1].Label != "Printf" {
				t.Errorf("order %q %q, want sortText order", it.Label, l.Items[1].Label)
			}
			if it.InsertText != tc.insert || it.Kind != KindFunction || it.Kind.String() != "function" ||
				it.Detail != "prefix=Pr" || it.FilterText != "Println" || it.SortText != "a" {
				t.Errorf("item %+v", it)
			}
			if !tc.edit {
				if it.Edit != nil {
					t.Errorf("unexpected edit %+v", it.Edit)
				}
				return
			}
			if it.Edit == nil {
				t.Fatal("no edit")
			}
			// The fake's range starts at the word: rune column len-2, in UTF-16 one more for the emoji.
			wantStart := UTF16Col(line, len([]rune(line))-2)
			if it.Edit.Range.Start != (Position{1, wantStart}) || it.Edit.Range.End != (Position{1, col}) || it.Edit.NewText != "Println" {
				t.Errorf("edit %+v, want start {1 %d} end {1 %d}", it.Edit, wantStart, col)
			}
			if got := RuneCol(line, it.Edit.Range.Start.Character); got != len([]rune(line))-2 {
				t.Errorf("rune start %d", got)
			}
		})
	}
}

func TestCompleteNoProvider(t *testing.T) {
	c := startFake(t, nil, "nocomplete")
	uri := URIFromPath(filepath.Join(c.Root(), "main.go"))
	c.DidOpen(uri, "go", "x")
	if c.HasCompletion() {
		t.Error("HasCompletion")
	}
	items, err := c.Complete(context.Background(), uri, 0, 1)
	if err != nil || items != nil {
		t.Errorf("Complete = %v, %v", items, err)
	}
	for _, m := range state(t, c).Methods {
		if m == "textDocument/completion" {
			t.Error("completion asked of a server without it")
		}
	}
}

func TestCompleteTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c, err := Options{CompletionTimeout: 200 * time.Millisecond, Logger: slog.New(slog.DiscardHandler)}.
		Start(ctx, fakeServer(t, "slow"), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Shutdown(context.Background())
	uri := URIFromPath(filepath.Join(c.Root(), "main.go"))
	c.DidOpen(uri, "go", "x")
	start := time.Now()
	items, err := c.Complete(context.Background(), uri, 0, 1)
	if !errors.Is(err, ErrTimeout) || items != nil {
		t.Fatalf("Complete = %v, %v; want nothing and ErrTimeout", items, err)
	}
	if d := time.Since(start); d < 150*time.Millisecond || d > 5*time.Second {
		t.Errorf("took %v", d)
	}
	st := state(t, c)
	if len(st.Cancelled) != 1 {
		t.Errorf("cancelRequest not sent: %+v", st.Cancelled)
	}
	// The late cancel answer must not confuse the next call.
	if _, err := c.call(ctx, "fake/state", nil); err != nil {
		t.Fatal(err)
	}
	if CompletionTimeout != 1500*time.Millisecond || ChangeDebounce != 150*time.Millisecond ||
		ShutdownGrace != 2*time.Second || IdleTimeout != 10*time.Minute || MaxRestarts != 3 {
		t.Error("spec defaults changed")
	}
}

func TestShutdownExit(t *testing.T) {
	c := startFake(t, nil)
	if err := c.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-c.Done():
	default:
		t.Fatal("Shutdown returned before the process exited")
	}
	if c.Err() != nil {
		t.Errorf("exit error %v: the fake exits 0 only on shutdown then exit", c.Err())
	}
	if !c.Stopping() {
		t.Error("Stopping")
	}
	if _, err := c.Complete(context.Background(), "file:///x", 0, 0); !errors.Is(err, ErrClosed) {
		t.Errorf("Complete after shutdown: %v", err)
	}
	c.DidOpen("file:///x", "go", "") // no panic, no block
	if err := c.Shutdown(context.Background()); err != nil {
		t.Error(err)
	}
}

func TestShutdownKillsStuckServer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c, err := Options{ShutdownGrace: 300 * time.Millisecond, Logger: slog.New(slog.DiscardHandler)}.
		Start(ctx, fakeServer(t, "noexit"), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	_ = c.Shutdown(context.Background())
	if d := time.Since(start); d < 250*time.Millisecond || d > 10*time.Second {
		t.Errorf("Shutdown took %v", d)
	}
	select {
	case <-c.Done():
	default:
		t.Fatal("process still running")
	}
	if c.Err() == nil {
		t.Error("a killed process should report an exit error")
	}
}

func TestCrashFailsRequests(t *testing.T) {
	c := startFake(t, nil, "crash")
	uri := URIFromPath(filepath.Join(c.Root(), "main.go"))
	c.DidOpen(uri, "go", "x")
	_, err := c.Complete(context.Background(), uri, 0, 1)
	if !errors.Is(err, ErrClosed) {
		t.Fatalf("Complete on a crashing server: %v", err)
	}
	select {
	case <-c.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("Done not closed after the crash")
	}
	if c.Stopping() || c.Err() == nil {
		t.Errorf("crash: stopping %v err %v", c.Stopping(), c.Err())
	}
}

func TestStartFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := (Options{Logger: slog.New(slog.DiscardHandler)}).Start(ctx, fakeServer(t, "crashstart"), t.TempDir()); err == nil {
		t.Fatal("Start of a server that exits at once succeeded")
	}
	if _, err := Start(ctx, Server{Name: "none"}, t.TempDir()); !errors.Is(err, ErrUnavailable) {
		t.Errorf("empty command: %v", err)
	}
	if _, err := Start(ctx, Server{Name: "missing", Command: []string{filepath.Join(t.TempDir(), "no-such-server")}}, t.TempDir()); err == nil {
		t.Error("missing program started")
	}
}

func TestStderrGoesToLog(t *testing.T) {
	var buf syncBuffer
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c, err := Options{Logger: slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))}.
		Start(ctx, fakeServer(t, "ask"), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_ = c.Shutdown(context.Background())
	out := buf.String()
	if !strings.Contains(out, "fake: started") || !strings.Contains(out, "lsp stderr") {
		t.Errorf("stderr not logged:\n%s", out)
	}
	if !strings.Contains(out, "hello") {
		t.Errorf("window/logMessage not logged:\n%s", out)
	}
}

func TestEnvScrubbed(t *testing.T) {
	t.Setenv("LEETCODE_SESSION", "secret-session")
	t.Setenv("LEETCODE_CSRFTOKEN", "secret-csrf")
	t.Setenv("FAKE_LSP_KEEP", "kept")
	c := startFake(t, nil)
	env := state(t, c).Env
	if env["LEETCODE_SESSION"] != "" || env["LEETCODE_CSRFTOKEN"] != "" {
		t.Errorf("secrets reached the server: %v", env)
	}
	if env["FAKE_LSP_KEEP"] != "kept" {
		t.Errorf("ordinary variable lost: %v", env)
	}
}
