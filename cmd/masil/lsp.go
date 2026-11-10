package main

import (
	"context"
	"time"

	"github.com/patrache/masil/internal/config"
	"github.com/patrache/masil/internal/lsp"
)

// lspStopBudget bounds stopping the language servers on exit (each gets
// lsp.ShutdownGrace before it is killed; they stop in parallel).
const lspStopBudget = lsp.ShutdownGrace + time.Second

// setupLSP applies the [lsp.<lang>] commands of cfg and returns the stop that
// realMain defers: every language server the editors started ends with masil.
func setupLSP(cfg *config.Config) (stop func()) {
	lsp.SetOverrides(cfg.LSPCommands())
	return func() {
		ctx, cancel := context.WithTimeout(context.Background(), lspStopBudget)
		defer cancel()
		lsp.StopAll(ctx)
	}
}
