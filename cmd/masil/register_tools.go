package main

// The tool entry (v0.1.3 A8: the hub, the tool-instance route, tools.* palette
// commands) registers itself from init(); each tool adds its own
// register_<tool>.go. All of it stays hidden while no tool feature flag is on.
import _ "github.com/patrache/masil/internal/ui/tools"
