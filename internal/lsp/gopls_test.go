package lsp

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestGoplsCompletesPrintln runs the real gopls (when it is on PATH and the
// run is not -short) on a temp module: completing `fmt.Pr` offers Println.
func TestGoplsCompletesPrintln(t *testing.T) {
	if testing.Short() {
		t.Skip("-short: no real language server")
	}
	srv, ok := Find("go")
	if !ok {
		t.Skip("gopls not on PATH")
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/lsptest\n\ngo 1.21\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	src := "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Pr\n}\n"
	path := filepath.Join(root, "main.go")
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	// A cold gopls loads the standard library first: allow it far more than
	// the editor's 1.5 s, and ask again until it answers.
	m := NewManager(ManagerOptions{
		Client: Options{CompletionTimeout: 20 * time.Second, Logger: slog.New(slog.DiscardHandler)},
		Find:   func(string) (Server, bool) { return srv, true },
	})
	defer m.StopAll(context.Background())
	if err := m.Open(root, path, src); err != nil {
		t.Fatal(err)
	}
	line := "\tfmt.Pr"
	deadline := time.Now().Add(60 * time.Second)
	var last List
	for time.Now().Before(deadline) {
		l, err := m.Complete(context.Background(), path, 5, UTF16Col(line, len([]rune(line))))
		if err == nil {
			last = l
			for _, it := range l.Items {
				if it.Label == "Println" {
					t.Logf("gopls item: %+v edit %+v (incomplete %v, %d items)", it, it.Edit, l.Incomplete, len(l.Items))
					if it.InsertText != "Println" && (it.Edit == nil || it.Edit.NewText == "") {
						t.Errorf("Println item %+v has nothing to insert", it)
					}
					return
				}
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("gopls never offered Println; last answer %+v", last)
}
