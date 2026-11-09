//go:build unix

package clipboard

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestExecRunFeedsStdin: the text reaches the program on stdin only.
func TestExecRunFeedsStdin(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out")
	c := command{name: "/bin/sh", args: []string{"-c", `cat > "$1"`, "sh", out}, input: []byte("한글 ok")}
	if err := execRun(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(out)
	if err != nil || string(b) != "한글 ok" {
		t.Fatalf("read %q %v", b, err)
	}
}

// TestExecRunDoesNotWaitForForkedChild: xclip / xsel / wl-copy fork a child
// that keeps serving the selection. With stdout and stderr not piped, Run
// returns once the program itself exits instead of waiting for that child.
func TestExecRunDoesNotWaitForForkedChild(t *testing.T) {
	c := command{name: "/bin/sh", args: []string{"-c", "cat >/dev/null; sleep 5 & exit 0"}, input: []byte("x")}
	start := time.Now()
	if err := execRun(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("execRun waited %v for the forked child", d)
	}
}

// TestExecRunHonorsDeadline: a stuck program is killed at the deadline.
func TestExecRunHonorsDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err := execRun(ctx, command{name: "/bin/sleep", args: []string{"5"}}); err == nil {
		t.Fatal("want an error")
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("took %v", d)
	}
}
