package termhost

import (
	"fmt"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// within fails t unless f returns before d.
func within(t *testing.T, d time.Duration, what string, f func()) {
	t.Helper()
	done := make(chan struct{})
	go func() { f(); close(done) }()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("%s did not return within %v", what, d)
	}
}

// TestVTStopWhileInputFlows: stop must return even when Responses has just
// taken input and seen stopping (it then never reads again). The pane
// forwards keys through the same pipe, so a flood of keys hits that window.
func TestVTStopWhileInputFlows(t *testing.T) {
	for i := range 300 {
		v := newVTScreen(Size{W: 20, H: 4}, 0, screenHooks{})
		go v.Responses(func([]byte) {})
		quit := make(chan struct{})
		sent := make(chan struct{})
		go func() {
			defer close(sent)
			k := tea.KeyPressMsg{Code: 'a', Text: "a"}
			for {
				select {
				case <-quit:
					return
				default:
					v.SendKey(k)
				}
			}
		}()
		time.Sleep(50 * time.Microsecond)
		within(t, 2*time.Second, fmt.Sprintf("stop (run %d)", i), v.stop)
		close(quit)
		within(t, 2*time.Second, "a key sent after stop", func() { <-sent })
	}
}

// TestVTInputAfterStopNeverBlocks: once stopped, nothing reads the input
// pipe; a key, a paste or a query in late output must not block on it.
func TestVTInputAfterStopNeverBlocks(t *testing.T) {
	v := newVTScreen(Size{W: 20, H: 4}, 0, screenHooks{})
	go v.Responses(func([]byte) {})
	within(t, 2*time.Second, "stop", v.stop)
	within(t, 2*time.Second, "SendKey after stop", func() { v.SendKey(tea.KeyPressMsg{Code: 'a', Text: "a"}) })
	within(t, 2*time.Second, "SendPaste after stop", func() { v.SendPaste("x") })
	within(t, 2*time.Second, "a DA query after stop", func() { v.Write([]byte("\x1b[c")) })
	within(t, 2*time.Second, "a second stop", v.stop)
}
