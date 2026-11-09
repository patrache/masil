// Package features holds the tool feature flags (v0.1.3 spec A8, JX9): every
// tool stays hidden — no hub, no ctrl+t, no palette rows, no help lines —
// until its flag is on, so the 0.1.2 screens are unchanged byte for byte.
//
// MASIL_FEATURES turns flags on for a run: a comma list of tool ids
// ("leet,arch") or "all". The release PR (P0 Task 31) fills released.
package features

import (
	"os"
	"slices"
	"strings"
)

// known are the feature ids: the five tool ids (ui.ToolLeet …).
var known = []string{"leet", "workspace", "arch", "git", "api"}

// released are the flags a release turns on without MASIL_FEATURES. Empty in
// 0.1.3 until P0 Task 31.
var released []string

// envVar names the flags of this run.
const envVar = "MASIL_FEATURES"

// Known returns the feature ids (a fresh slice).
func Known() []string { return slices.Clone(known) }

// On reports whether feature id is on: released, named in MASIL_FEATURES, or
// MASIL_FEATURES is "all". The environment is read on every call so tests can
// use t.Setenv.
func On(id string) bool {
	if slices.Contains(released, id) {
		return true
	}
	for _, f := range strings.Split(os.Getenv(envVar), ",") {
		f = strings.ToLower(strings.TrimSpace(f))
		if f == "all" || (f != "" && f == id) {
			return true
		}
	}
	return false
}

// Any reports whether any known feature is on (the shared tool surface —
// ctrl+t, the hub, /tools — shows only then).
func Any() bool {
	return slices.ContainsFunc(known, On)
}
