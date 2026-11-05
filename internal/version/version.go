// Package version holds build metadata and the User-Agent string (CA 4.1).
package version

// Build metadata, overridden by -ldflags -X at release time.
var (
	Version = "0.0.0-dev" // -ldflags -X
	Commit  = ""
	Date    = ""
)

// UserAgent returns "masil/<Version> (<runtime.GOOS>; <runtime.GOARCH>)", e.g. "masil/0.1.0 (darwin; arm64)".
func UserAgent() string {
	// TODO(core): Task 3
	return ""
}

// Semver returns Version without a "v" prefix and with a "-dev" suffix stripped, for comparisons.
func Semver() string {
	// TODO(core): Task 3
	return ""
}
