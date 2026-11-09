package workspace

import (
	"sync"

	"github.com/patrache/masil/internal/termhost"
	"github.com/patrache/masil/internal/workspace/share"
)

// paneTerm adapts a P0 pane to share.Terminal: Tap = AttachTap (its
// snapshot is the first keyframe), Inject = Pane.Inject, the size calls as
// they are.
type paneTerm struct {
	p     *termhost.Pane
	mu    sync.Mutex
	first []byte // AttachTap's snapshot, the next Snapshot's answer
}

var _ share.Terminal = (*paneTerm)(nil)

func newPaneTerm(p *termhost.Pane) *paneTerm { return &paneTerm{p: p} }

// tapFunc is a termhost.Tap over a func.
type tapFunc func([]byte)

func (f tapFunc) Output(b []byte) { f(b) }

func (a *paneTerm) Tap(fn func([]byte)) (stop func()) {
	snap, detach := a.p.AttachTap(tapFunc(fn))
	a.mu.Lock()
	a.first = snap
	a.mu.Unlock()
	return detach
}

func (a *paneTerm) Snapshot() []byte {
	a.mu.Lock()
	first := a.first
	a.first = nil
	a.mu.Unlock()
	if first != nil {
		return first
	}
	snap, detach := a.p.AttachTap(tapFunc(func([]byte) {}))
	detach()
	return snap
}

func (a *paneTerm) Inject(p []byte) error {
	a.p.Inject("guest", p)
	return nil
}

func (a *paneTerm) Size() (cols, rows int) {
	s := a.p.Size()
	return s.W, s.H
}

func (a *paneTerm) SetFixedSize(cols, rows int) {
	a.p.SetFixedSize(termhost.Size{W: cols, H: rows})
}

func (a *paneTerm) BracketedPaste() bool { return a.p.BracketedPaste() }
func (a *paneTerm) Title() string        { return a.p.Title() }
