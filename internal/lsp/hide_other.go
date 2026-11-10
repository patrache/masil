//go:build unix

package lsp

import (
	"os/exec"
	"sync"
	"syscall"
)

// procAttr puts the server in a process group of its own, so a kill reaches
// what it started too (a wrapper script that forks instead of exec'ing, a
// server's own helper processes).
func procAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// procTree is a started server's process group.
type procTree struct {
	mu   sync.Mutex
	pgid int // 0 once closed
}

func newProcTree(cmd *exec.Cmd) (*procTree, error) {
	return &procTree{pgid: cmd.Process.Pid}, nil
}

// kill sends SIGKILL to the whole group.
func (t *procTree) kill() {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.pgid > 0 {
		_ = syscall.Kill(-t.pgid, syscall.SIGKILL)
	}
}

// close runs once the server has exited and been reaped: whatever it left
// behind in its group is killed, and later kills do nothing (the group id
// may be reused once the group is empty).
func (t *procTree) close() {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.pgid > 0 {
		_ = syscall.Kill(-t.pgid, syscall.SIGKILL)
		t.pgid = 0
	}
}
