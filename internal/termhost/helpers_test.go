package termhost

import (
	"strings"
	"testing"
	"time"
)

// waitFor polls cond every 10ms until it is true or timeout passes.
func waitFor(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out after %v waiting for %s", timeout, what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// plain strips SGR sequences from a rendered screen.
func plain(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
				j++
			}
			i = j
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// waitExited waits for p's exit info.
func waitExited(t *testing.T, p *Pane, timeout time.Duration) ExitInfo {
	t.Helper()
	select {
	case info := <-p.Done():
		return info
	case <-time.After(timeout):
		t.Fatalf("pane %s did not exit within %v", p.Program(), timeout)
		return ExitInfo{}
	}
}
