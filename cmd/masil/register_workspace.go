package main

// The workspace tool (v0.1.3 spec B6, P2) registers its tool, ws.* palette
// commands, kill switch and alert target from init(); all of it stays hidden
// while the "workspace" feature flag is off.
import (
	_ "github.com/patrache/masil/internal/ui/workspace"
)
