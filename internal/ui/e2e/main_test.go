// Package e2e is the last core test: the whole product (chat and sns-fun
// registered exactly as cmd/masil registers them) under the uitest harness
// (CA 10.5).
package e2e

import (
	_ "github.com/patrache/masil/internal/ui/chat"
	_ "github.com/patrache/masil/internal/ui/feed"
	_ "github.com/patrache/masil/internal/ui/fun"
)
