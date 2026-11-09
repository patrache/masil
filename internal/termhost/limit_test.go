package termhost

import (
	"errors"
	"io"
	"slices"
	"sync"
	"testing"
	"time"
)

// fakeProc is a process that runs until closed.
type fakeProc struct {
	once sync.Once
	gone chan struct{}
}

func newFakeProc() *fakeProc { return &fakeProc{gone: make(chan struct{})} }

func (f *fakeProc) end() { f.once.Do(func() { close(f.gone) }) }
func (f *fakeProc) Read(b []byte) (int, error) {
	<-f.gone
	return 0, io.EOF
}
func (f *fakeProc) Write(b []byte) (int, error) { return len(b), nil }
func (f *fakeProc) Resize(Size) error           { return nil }
func (f *fakeProc) Signal(kill bool) error      { f.end(); return nil }
func (f *fakeProc) Wait() (int, error)          { <-f.gone; return 0, nil }
func (f *fakeProc) Close() error                { f.end(); return nil }
func (f *fakeProc) Pid() int                    { return 0 }

func useFakeStarter(t *testing.T) {
	t.Helper()
	restore := startProc
	startProc = func(Request, Size, []string) (proc, error) { return newFakeProc(), nil }
	t.Cleanup(func() {
		CloseAll(time.Second)
		startProc = restore
	})
}

func TestPaneLimit(t *testing.T) {
	if n := LivePanes(); n != 0 {
		t.Fatalf("LivePanes() = %d at start", n)
	}
	useFakeStarter(t)
	var panes []*Pane
	for i, prog := range []string{"/usr/bin/lazygit", "nvim", "bash", "claude"} {
		p, err := StartPane(Request{Program: prog}, Size{W: 80, H: 24}, PaneOptions{})
		if err != nil {
			t.Fatalf("pane %d: %v", i, err)
		}
		panes = append(panes, p)
	}
	if n := LivePanes(); n != MaxPanes {
		t.Fatalf("LivePanes() = %d, want %d", n, MaxPanes)
	}
	if got := Running(); !slices.Equal(got, []string{"lazygit", "nvim", "bash", "claude"}) {
		t.Fatalf("Running() = %v", got)
	}
	if _, err := StartPane(Request{Program: "vim"}, Size{W: 80, H: 24}, PaneOptions{}); !errors.Is(err, ErrPaneLimit) {
		t.Fatalf("5th pane err = %v, want ErrPaneLimit", err)
	}
	if err := panes[1].Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	waitExited(t, panes[1], 2*time.Second)
	if n := LivePanes(); n != MaxPanes-1 {
		t.Fatalf("LivePanes() after close = %d", n)
	}
	if _, err := StartPane(Request{Program: "vim"}, Size{W: 80, H: 24}, PaneOptions{}); err != nil {
		t.Fatalf("pane after close: %v", err)
	}
	CloseAll(time.Second)
	if n := LivePanes(); n != 0 {
		t.Fatalf("LivePanes() after CloseAll = %d", n)
	}
}

func TestStartFailureReleasesSlot(t *testing.T) {
	restore := startProc
	startProc = func(Request, Size, []string) (proc, error) { return nil, errors.New("no such program") }
	t.Cleanup(func() { startProc = restore })
	for range MaxPanes + 1 {
		if _, err := StartPane(Request{Program: "nope"}, Size{W: 10, H: 5}, PaneOptions{}); err == nil || errors.Is(err, ErrPaneLimit) {
			t.Fatalf("err = %v, want the start error", err)
		}
	}
	if n := LivePanes(); n != 0 {
		t.Fatalf("LivePanes() = %d after failed starts", n)
	}
}

func TestStartPaneRejectsBadSize(t *testing.T) {
	useFakeStarter(t)
	p, err := StartPane(Request{Program: "x"}, Size{}, PaneOptions{})
	if err != nil {
		t.Fatalf("zero size start: %v", err)
	}
	if s := p.Size(); s.W <= 0 || s.H <= 0 {
		t.Fatalf("Size() = %+v, want a positive default", s)
	}
}
