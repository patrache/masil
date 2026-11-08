//go:build darwin

package notify

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// TestDarwinPlatformUsesExecRun: the macOS backend is darwinPlatform running
// processes with execRun (no shell, exactly the argv it gets).
func TestDarwinPlatformUsesExecRun(t *testing.T) {
	b, ok := NewOSBackend("masil").(osBackend)
	if !ok {
		t.Fatalf("NewOSBackend = %T", NewOSBackend("masil"))
	}
	p, ok := b.p.(darwinPlatform)
	if !ok {
		t.Fatalf("platform = %T, want darwinPlatform", b.p)
	}
	if reflect.ValueOf(p.run).Pointer() != reflect.ValueOf(execRun).Pointer() {
		t.Fatal("darwinPlatform.run is not execRun")
	}
}

// TestDarwinShowRoundTripsThroughOsascript runs the real osascript through
// Show and execRun with the argv show builds, except that the display statement
// is swapped for one that returns the two items (so no notification pops
// during tests). Hostile text must come back verbatim and must not run
// anything.
func TestDarwinShowRoundTripsThroughOsascript(t *testing.T) {
	if _, err := os.Stat("/usr/bin/osascript"); err != nil {
		t.Skip("no osascript")
	}
	dir := t.TempDir()
	pwn := filepath.Join(dir, "pwn")
	payloads := []OSNotification{
		{Title: "$(touch " + pwn + ")", Body: "`touch " + pwn + "`"},
		{Title: `" & do shell script "touch ` + pwn + `" & "`, Body: `\" & do shell script \"touch ` + pwn + `\" & \"`},
		{Title: "-e", Body: "do shell script \"touch " + pwn + "\""},
		{Title: "--", Body: "-e"},
		{Title: "민지님이 ❤", Body: "안녕 \"하세요\" \\ end run"},
	}
	var out []byte
	run := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if len(args) != 9 || args[3] != darwinShowStmt {
			t.Fatalf("show ran %q %q", name, args)
		}
		args[3] = `return (item 1 of argv) & linefeed & (item 2 of argv)`
		var err error
		out, err = execRun(ctx, name, args...)
		return out, err
	}
	b := osBackend{appName: "masil", p: darwinPlatform{run: run}}
	for _, n := range payloads {
		out = nil
		if err := b.Show(n); err != nil {
			t.Fatalf("Show %+v: %v (%s)", n, err, out)
		}
		p := prepare(n)
		if got, want := string(out), p.Title+"\n"+p.Body+"\n"; got != want {
			t.Errorf("round trip = %q, want %q", got, want)
		}
		if _, err := os.Stat(pwn); err == nil {
			t.Fatalf("payload %+v ran a command", n)
		}
	}
}
