package main

import (
	"os"
	"testing"

	"github.com/patrache/masil/internal/config"
	"github.com/patrache/masil/internal/lsp"
)

// setupLSP hands [lsp.<lang>] commands to the lsp package and returns the
// exit-time stop.
func TestSetupLSP(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lsp.SetOverrides(nil) })
	cfg := config.Defaults()
	cfg.LSP = map[string]config.LSPConfig{"cobol": {Command: []string{exe, "--stdio"}}}
	stop := setupLSP(cfg)
	s, ok := lsp.Find("cobol")
	if !ok || len(s.Command) != 2 || s.Command[1] != "--stdio" {
		t.Fatalf("Find(cobol) = %+v, %v: the config override was not applied", s, ok)
	}
	stop() // no server ran: returns at once
}
