//go:build !windows

package termhost

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

const orphanHelperEnv = "MASIL_TERMHOST_ORPHAN_HELPER"

func TestMain(m *testing.M) {
	if os.Getenv(orphanHelperEnv) == "1" {
		runOrphanHelper()
		return
	}
	os.Exit(m.Run())
}

// runOrphanHelper starts a `sleep 1000` pane, prints the child pid and waits
// to be killed.
func runOrphanHelper() {
	p, err := StartPane(Request{Program: "sleep", Args: []string{"1000"}}, Size{W: 20, H: 3}, PaneOptions{})
	if err != nil {
		fmt.Println("ERR", err)
		os.Exit(1)
	}
	fmt.Println("PID", p.proc.Pid())
	select {}
}

func TestOrphansDieWithParent(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), orphanHelperEnv+"=1")
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(out).ReadString('\n')
	if err != nil {
		_ = cmd.Process.Kill()
		t.Fatalf("helper output: %v", err)
	}
	f := strings.Fields(line)
	if len(f) != 2 || f[0] != "PID" {
		_ = cmd.Process.Kill()
		t.Fatalf("helper said %q", line)
	}
	pid, err := strconv.Atoi(f[1])
	if err != nil || pid <= 0 {
		_ = cmd.Process.Kill()
		t.Fatalf("bad pid %q", f[1])
	}
	if err := syscall.Kill(pid, 0); err != nil {
		t.Fatalf("child %d not running before the kill: %v", pid, err)
	}
	if err := cmd.Process.Signal(syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	deadline := time.Now().Add(10 * time.Second)
	for {
		err := syscall.Kill(pid, 0)
		if errors.Is(err, syscall.ESRCH) {
			return
		}
		if time.Now().After(deadline) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
			t.Fatalf("pane child %d still alive 10s after its parent was killed (kill(0) = %v)", pid, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
