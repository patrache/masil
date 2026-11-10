//go:build windows

package lsp

import (
	"fmt"
	"os/exec"
	"sync"
	"syscall"
	"unsafe"
)

// procAttr keeps a console server from flashing a window on Windows.
func procAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
}

// kernel32 is a KnownDLL: it always loads from System32.
var (
	kernel32                     = syscall.NewLazyDLL("kernel32.dll")
	procCreateJobObjectW         = kernel32.NewProc("CreateJobObjectW")
	procSetInformationJobObject  = kernel32.NewProc("SetInformationJobObject")
	procAssignProcessToJobObject = kernel32.NewProc("AssignProcessToJobObject")
	procTerminateJobObject       = kernel32.NewProc("TerminateJobObject")
)

const (
	jobObjectExtendedLimitInformation = 9      // JOBOBJECTINFOCLASS
	jobObjectLimitKillOnJobClose      = 0x2000 // JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	processSetQuota                   = 0x0100 // PROCESS_SET_QUOTA
	processTerminate                  = 0x0001 // PROCESS_TERMINATE
)

// jobExtendedLimitInformation is JOBOBJECT_EXTENDED_LIMIT_INFORMATION
// (the layout matches on amd64 and arm64).
type jobExtendedLimitInformation struct {
	PerProcessUserTimeLimit int64
	PerJobUserTimeLimit     int64
	LimitFlags              uint32
	MinimumWorkingSetSize   uintptr
	MaximumWorkingSetSize   uintptr
	ActiveProcessLimit      uint32
	Affinity                uintptr
	PriorityClass           uint32
	SchedulingClass         uint32
	IoInfo                  [6]uint64 // IO_COUNTERS
	ProcessMemoryLimit      uintptr
	JobMemoryLimit          uintptr
	PeakProcessMemoryUsed   uintptr
	PeakJobMemoryUsed       uintptr
}

// procTree is a Job object holding the server and every process it starts.
// npm servers (typescript-language-server, pyright-langserver, intelephense)
// are .cmd shims: the direct child is cmd.exe and the server is its node.exe,
// which TerminateProcess on cmd.exe would leave running. The job is
// KILL_ON_JOB_CLOSE, so the tree also ends when masil exits in any way.
type procTree struct {
	mu  sync.Mutex
	job syscall.Handle // 0 once closed
}

func newProcTree(cmd *exec.Cmd) (*procTree, error) {
	if err := kernel32.Load(); err != nil {
		return nil, err
	}
	r, _, e := procCreateJobObjectW.Call(0, 0)
	if r == 0 {
		return nil, fmt.Errorf("CreateJobObject: %w", e)
	}
	job := syscall.Handle(r)
	var info jobExtendedLimitInformation
	info.LimitFlags = jobObjectLimitKillOnJobClose
	r, _, e = procSetInformationJobObject.Call(uintptr(job), jobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), unsafe.Sizeof(info))
	if r == 0 {
		_ = syscall.CloseHandle(job)
		return nil, fmt.Errorf("SetInformationJobObject: %w", e)
	}
	h, err := syscall.OpenProcess(processSetQuota|processTerminate, false, uint32(cmd.Process.Pid))
	if err != nil {
		_ = syscall.CloseHandle(job)
		return nil, fmt.Errorf("OpenProcess: %w", err)
	}
	defer syscall.CloseHandle(h)
	r, _, e = procAssignProcessToJobObject.Call(uintptr(job), uintptr(h))
	if r == 0 {
		_ = syscall.CloseHandle(job)
		return nil, fmt.Errorf("AssignProcessToJobObject: %w", e)
	}
	return &procTree{job: job}, nil
}

// kill terminates every process in the job.
func (t *procTree) kill() {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.job != 0 {
		_, _, _ = procTerminateJobObject.Call(uintptr(t.job), 1)
	}
}

// close runs once the server has exited: closing the last job handle kills
// whatever it left behind (KILL_ON_JOB_CLOSE).
func (t *procTree) close() {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.job != 0 {
		_ = syscall.CloseHandle(t.job)
		t.job = 0
	}
}
