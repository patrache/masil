//go:build !windows

package daemon

// daemonChildEnv marks the hidden child started by Detach (used on Windows only).
const daemonChildEnv = "MASIL_DAEMON_CHILD"

// Detach is a no-op outside Windows: launchd/systemd start `masil daemon run`
// without a terminal, so the daemon always runs in this process.
func Detach(args []string) (detached bool, err error) { return false, nil }
