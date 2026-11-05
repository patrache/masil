// Package version holds build metadata and the User-Agent string (CA 4.1).
package version

import (
	"runtime"
	"strings"
)

// Build metadata, overridden by -ldflags -X at release time.
var (
	Version = "0.0.0-dev" // -ldflags -X
	Commit  = ""
	Date    = ""
)

// UserAgent returns "masil/<Version> (<runtime.GOOS>; <runtime.GOARCH>)", e.g. "masil/0.1.0 (darwin; arm64)".
func UserAgent() string {
	return "masil/" + Version + " (" + runtime.GOOS + "; " + runtime.GOARCH + ")"
}

// Semver returns Version without a "v" prefix and with a "-dev" suffix stripped, for comparisons.
func Semver() string {
	s := strings.TrimPrefix(Version, "v")
	return strings.TrimSuffix(s, "-dev")
}
