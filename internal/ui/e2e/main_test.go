// Package e2e is the last core test: the whole product (chat, sns-fun and
// the 0.1.3 tools, registered exactly as cmd/masil's register_*.go register
// them) under the uitest harness (CA 10.5).
package e2e

import (
	_ "github.com/patrache/masil/internal/ui/apitool"
	_ "github.com/patrache/masil/internal/ui/arch"
	_ "github.com/patrache/masil/internal/ui/chat"
	_ "github.com/patrache/masil/internal/ui/feed"
	_ "github.com/patrache/masil/internal/ui/fun"
	_ "github.com/patrache/masil/internal/ui/gittool"
	_ "github.com/patrache/masil/internal/ui/leet"
	_ "github.com/patrache/masil/internal/ui/secretsui"
	_ "github.com/patrache/masil/internal/ui/tools"
	_ "github.com/patrache/masil/internal/ui/workspace"
)
