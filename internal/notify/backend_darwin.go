//go:build darwin

package notify

// newPlatform runs osascript with execRun (show: backend_platform.go).
func newPlatform(string) platform { return darwinPlatform{run: execRun} }
