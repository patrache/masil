//go:build windows

package termhost

// Embedded panes are macOS/Linux only (spec A7.2): on Windows every tool opens
// in exec mode. Replay screens (no PTY) still work.
func init() {
	startProc = func(Request, Size, []string) (proc, error) { return nil, ErrUnsupported }
}
