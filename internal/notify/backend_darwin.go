//go:build darwin

package notify

import (
	"context"
	"fmt"
	"os/exec"
	"time"
)

// showTimeout bounds one OS notifier call (it runs inside a tea.Cmd / the
// daemon's notify goroutine).
const showTimeout = 10 * time.Second

type darwinPlatform struct{}

func newPlatform(string) platform { return darwinPlatform{} }

// show runs osascript directly (no shell) with the text as argv data.
func (darwinPlatform) show(n OSNotification) error {
	ctx, cancel := context.WithTimeout(context.Background(), showTimeout)
	defer cancel()
	name, args := darwinCommand(n)
	if out, err := exec.CommandContext(ctx, name, args...).CombinedOutput(); err != nil {
		return fmt.Errorf("notify: osascript: %w (%d bytes of output)", err, len(out))
	}
	return nil
}
