package termhost

import (
	"errors"
	"sync"
	"time"
)

// MaxPanes caps live pane processes across every tool (JA23·JX13).
const MaxPanes = 4

var (
	// ErrPaneLimit is returned by StartPane when MaxPanes panes are alive.
	ErrPaneLimit = errors.New("termhost: pane limit reached")
	// ErrUnsupported is returned by StartPane where embedded panes do not
	// exist (Windows): open the program in exec mode instead.
	ErrUnsupported = errors.New("termhost: embedded panes are not supported on this OS")
)

var registry struct {
	sync.Mutex
	panes    []*Pane // live, in start order
	reserved int     // slots taken by starts in progress
}

func reserveSlot() bool {
	registry.Lock()
	defer registry.Unlock()
	if len(registry.panes)+registry.reserved >= MaxPanes {
		return false
	}
	registry.reserved++
	return true
}

func releaseSlot() {
	registry.Lock()
	defer registry.Unlock()
	registry.reserved--
}

// registerPane turns a reservation into a live pane.
func registerPane(p *Pane) {
	registry.Lock()
	defer registry.Unlock()
	registry.reserved--
	registry.panes = append(registry.panes, p)
}

func unregisterPane(p *Pane) {
	registry.Lock()
	defer registry.Unlock()
	for i, q := range registry.panes {
		if q == p {
			registry.panes = append(registry.panes[:i], registry.panes[i+1:]...)
			return
		}
	}
}

func livePanes() []*Pane {
	registry.Lock()
	defer registry.Unlock()
	return append([]*Pane(nil), registry.panes...)
}

// LivePanes returns how many pane processes are alive. Replay screens are not counted.
func LivePanes() int {
	registry.Lock()
	defer registry.Unlock()
	return len(registry.panes)
}

// Running returns the program base names of live panes in start order (the
// quit confirmation lists them).
func Running() []string {
	ps := livePanes()
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.name()
	}
	return out
}

// CloseAll hangs up every live pane, waits up to grace for them to exit,
// SIGKILLs the rest and waits (briefly) for those too. Used when masil quits.
func CloseAll(grace time.Duration) {
	ps := livePanes()
	for _, p := range ps {
		p.closing.Store(true)
		_ = p.proc.Signal(false)
	}
	deadline := time.After(grace)
	for _, p := range ps {
		select {
		case <-p.exited:
		case <-deadline:
		}
	}
	after := time.After(drainAfterExit * 3)
	for _, p := range ps {
		select {
		case <-p.exited:
			continue
		default:
		}
		_ = p.proc.Signal(true)
		select {
		case <-p.exited:
		case <-after:
		}
	}
}
