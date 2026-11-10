package config_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/patrache/masil/internal/config"
)

// [lsp.<lang>] command = [...] overrides the language server for a languageId.
func TestLoadLSPCommands(t *testing.T) {
	p := testPaths(t)
	writeConfig(t, p, "[lsp.go]\ncommand = [\"/opt/gopls\", \"-remote=auto\"]\n[lsp.Python]\ncommand = [\"pylsp\"]\n[lsp.rust]\ncommand = []\n[lsp.bad]\ncommand = \"gopls\"\n", 0o600)
	c, err := config.Load(p)
	if !errors.Is(err, config.ErrPartial) || !strings.Contains(err.Error(), "lsp.bad") {
		t.Fatalf("err = %v, want ErrPartial naming lsp.bad", err)
	}
	want := map[string][]string{"go": {"/opt/gopls", "-remote=auto"}, "python": {"pylsp"}}
	if got := c.LSPCommands(); !reflect.DeepEqual(got, want) {
		t.Fatalf("LSPCommands = %v, want %v", got, want)
	}
	// Defaults have none, and a config without [lsp] saves without the table.
	if got := config.Defaults().LSPCommands(); len(got) != 0 {
		t.Errorf("defaults: %v", got)
	}
}

func TestSaveKeepsLSPCommands(t *testing.T) {
	p := testPaths(t)
	c := config.Defaults()
	c.LSP = map[string]config.LSPConfig{"go": {Command: []string{"gopls", "serve"}}}
	if err := config.Save(p, c); err != nil {
		t.Fatal(err)
	}
	got, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.LSP, c.LSP) {
		t.Fatalf("round trip %v, want %v", got.LSP, c.LSP)
	}
}
