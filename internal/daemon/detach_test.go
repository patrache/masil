package daemon

import (
	"runtime"
	"testing"
)

func TestDetach(t *testing.T) {
	if runtime.GOOS != "windows" {
		detached, err := Detach([]string{"daemon", "run"})
		if detached || err != nil {
			t.Fatalf("Detach = (%v, %v), want (false, nil) off Windows", detached, err)
		}
		return
	}
	// The hidden child must not respawn itself again.
	t.Setenv(daemonChildEnv, "1")
	detached, err := Detach([]string{"daemon", "run"})
	if detached || err != nil {
		t.Fatalf("child: Detach = (%v, %v), want (false, nil)", detached, err)
	}
}
