//go:build darwin

package notify

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestDarwinArgvRoundTripsThroughOsascript runs the real osascript with the
// exact argv darwinCommand builds, except that the display statement is
// swapped for one that returns the two items (so no notification pops during
// tests). Hostile text must come back verbatim and must not run anything.
func TestDarwinArgvRoundTripsThroughOsascript(t *testing.T) {
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
	for _, n := range payloads {
		p := prepare(n)
		name, args := darwinCommand(p)
		if args[3] != darwinShowStmt {
			t.Fatalf("args[3] = %q, want the display statement", args[3])
		}
		args[3] = `return (item 1 of argv) & linefeed & (item 2 of argv)`
		out, err := exec.Command(name, args...).Output()
		if err != nil {
			t.Fatalf("osascript %q: %v", args, err)
		}
		if got, want := string(out), p.Title+"\n"+p.Body+"\n"; got != want {
			t.Errorf("round trip = %q, want %q", got, want)
		}
		if _, err := os.Stat(pwn); err == nil {
			t.Fatalf("payload %+v ran a command", n)
		}
	}
}
