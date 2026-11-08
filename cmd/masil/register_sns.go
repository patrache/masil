package main

// The sns-fun UI packages register their routes, overlays and palette
// commands in init() (CA 2.1 register_sns.go).
import (
	_ "github.com/patrache/masil/internal/ui/feed"
	_ "github.com/patrache/masil/internal/ui/fun"
)
