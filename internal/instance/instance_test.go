package instance_test

import (
	"bufio"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/patrache/masil/internal/config"
	"github.com/patrache/masil/internal/instance"
)

func pathsIn(t *testing.T, dir string) config.Paths {
	t.Helper()
	t.Setenv("MASIL_CONFIG_DIR", dir)
	t.Setenv("MASIL_CACHE_DIR", t.TempDir())
	p, err := config.ResolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestTUIRunningNoLockFile(t *testing.T) {
	p := pathsIn(t, filepath.Join(t.TempDir(), "masil"))
	running, err := instance.TUIRunning(p)
	if err != nil || running {
		t.Fatalf("running=%v err=%v", running, err)
	}
}

func TestTwoTUIsShareLock(t *testing.T) {
	p := pathsIn(t, filepath.Join(t.TempDir(), "masil"))
	r1, err1 := instance.AcquireTUI(p)
	r2, err2 := instance.AcquireTUI(p)
	if err1 != nil || err2 != nil {
		t.Fatalf("acquire: %v / %v", err1, err2)
	}
	running, err := instance.TUIRunning(p)
	if err != nil || !running {
		t.Fatalf("with two TUIs: running=%v err=%v", running, err)
	}
	r1()
	running, err = instance.TUIRunning(p)
	if err != nil || !running {
		t.Fatalf("with one TUI left: running=%v err=%v", running, err)
	}
	r2()
	running, err = instance.TUIRunning(p)
	if err != nil || running {
		t.Fatalf("after release: running=%v err=%v", running, err)
	}
	// Release is idempotent.
	r1()
	r2()
}

func TestDaemonSingleton(t *testing.T) {
	p := pathsIn(t, filepath.Join(t.TempDir(), "masil"))
	r1, err := instance.AcquireDaemon(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := instance.AcquireDaemon(p); !errors.Is(err, instance.ErrAlreadyRunning) {
		t.Fatalf("second daemon: err = %v", err)
	}
	r1()
	r2, err := instance.AcquireDaemon(p)
	if err != nil {
		t.Fatalf("after release: %v", err)
	}
	r2()
}

func TestTUIRunningAcrossProcesses(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "masil")
	p := pathsIn(t, dir)
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperHoldTUILock$", "-test.count=1")
	cmd.Env = append(os.Environ(), "MASIL_HELPER=1", "MASIL_CONFIG_DIR="+dir)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer stdin.Close()

	lines := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			if strings.TrimSpace(sc.Text()) == "locked" {
				lines <- "locked"
				return
			}
		}
		close(lines)
	}()
	select {
	case l, ok := <-lines:
		if !ok || l != "locked" {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			t.Fatal("helper exited before locking")
		}
	case <-time.After(30 * time.Second):
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatal("helper did not lock in time")
	}

	running, err := instance.TUIRunning(p)
	if err != nil || !running {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("helper holds lock: running=%v err=%v", running, err)
	}

	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()

	running, err = instance.TUIRunning(p)
	if err != nil || running {
		t.Fatalf("after helper died: running=%v err=%v", running, err)
	}
}

// TestHelperHoldTUILock is the child process of TestTUIRunningAcrossProcesses.
func TestHelperHoldTUILock(t *testing.T) {
	if os.Getenv("MASIL_HELPER") != "1" {
		t.Skip("helper process only")
	}
	p, err := config.ResolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	release, err := instance.AcquireTUI(p)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	os.Stdout.WriteString("locked\n")
	// Block until the parent closes stdin or kills us.
	buf := make([]byte, 1)
	for {
		if _, err := os.Stdin.Read(buf); err != nil {
			return
		}
	}
}
