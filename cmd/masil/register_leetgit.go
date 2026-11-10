package main

// The P1 tools register themselves from init() (v0.1.3 A10·A11): the
// LeetCode daily problem tool (hub row, problem view, editor, login, cards,
// leet.* palette commands) and the lazygit tool (hub row, picker, install
// guide, git.* palette commands). They stay hidden while their feature flag
// is off.
import (
	_ "github.com/patrache/masil/internal/ui/gittool"
	_ "github.com/patrache/masil/internal/ui/leet"
)
