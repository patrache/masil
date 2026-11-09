package clipboard

import (
	"bytes"
	"context"
	"os/exec"
	"time"
)

// waitDelay bounds how long Run waits for the stdin copy after the program
// exited or the context ended.
const waitDelay = 500 * time.Millisecond

// execRun is the real runFunc: exactly c.name and c.args, never a shell; the
// text goes to stdin only (never argv, so ps cannot show it). Stdout and
// stderr stay unconnected (/dev/null): xclip, xsel and wl-copy fork a child
// that keeps serving the selection, and a pipe it inherited would hold Run
// until that child exits.
func execRun(ctx context.Context, c command) error {
	cmd := exec.CommandContext(ctx, c.name, c.args...)
	cmd.Env = c.env // nil = this process's environment
	cmd.Stdin = bytes.NewReader(c.input)
	cmd.WaitDelay = waitDelay
	return cmd.Run()
}

// lookPath resolves a clipboard tool on $PATH (a relative result is refused:
// exec.ErrDot).
func lookPath(name string) (string, error) { return exec.LookPath(name) }
