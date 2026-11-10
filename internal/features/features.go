// Package features holds the tool feature flags (v0.1.3 spec A8, JX9, JC21):
// every tool stays hidden — no hub, no ctrl+t, no palette rows, no help lines —
// until its flag is on.
//
// Without MASIL_FEATURES the release set is on (P0 Task 31: the five 0.1.3
// tools). A MASIL_FEATURES that is set wins over it for that run: a comma
// list of tool ids ("leet,arch"), "all", or "none" (an empty value is the
// same as "none") to turn every tool off.
package features

import (
	"os"
	"slices"
	"strings"
)

// known are the feature ids: the five tool ids (ui.ToolLeet …).
var known = []string{"leet", "workspace", "arch", "git", "api"}

// released are the flags a release turns on without MASIL_FEATURES: the
// 0.1.3 release set, all five tools (P0 Task 31; the owner confirms it).
var released = []string{"leet", "workspace", "arch", "git", "api"}

// envVar names the flags of this run.
const envVar = "MASIL_FEATURES"

// Known returns the feature ids (a fresh slice).
func Known() []string { return slices.Clone(known) }

// Released returns the release set (a fresh slice).
func Released() []string { return slices.Clone(released) }

// On reports whether feature id is on: named in MASIL_FEATURES or it is
// "all"; without MASIL_FEATURES, in the release set. The environment is read
// on every call so tests can use t.Setenv.
func On(id string) bool {
	env, set := os.LookupEnv(envVar)
	if !set {
		return slices.Contains(released, id)
	}
	for _, f := range strings.Split(env, ",") {
		f = strings.ToLower(strings.TrimSpace(f))
		if f == "all" || (f != "" && f != "none" && f == id) {
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
