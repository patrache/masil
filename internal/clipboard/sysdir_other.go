//go:build !windows

package clipboard

// systemDir is only used on Windows.
func systemDir() string { return "" }
