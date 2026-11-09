//go:build !windows

package termhost

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/textwidth"
)

// startSh starts `sh -c script` in a pane closed at cleanup.
func startSh(t *testing.T, script string, size Size, opts PaneOptions) *Pane {
	t.Helper()
	p, err := StartPane(Request{Program: "sh", Args: []string{"-c", script}, Title: "sh"}, size, opts)
	if err != nil {
		t.Fatalf("StartPane: %v", err)
	}
	t.Cleanup(func() {
		_ = p.Close()
		select {
		case <-p.exited:
		case <-time.After(5 * time.Second):
			t.Errorf("pane did not exit at cleanup")
		}
	})
	return p
}

func screenHas(p *Pane, s string) func() bool {
	return func() bool { return strings.Contains(plain(p.Render()), s) }
}

func enter() tea.KeyPressMsg { return tea.KeyPressMsg{Code: tea.KeyEnter} }

func TestPaneRunsAndRenders(t *testing.T) {
	p := startSh(t, "printf '한글ab'; sleep 30", Size{W: 20, H: 5}, PaneOptions{})
	waitFor(t, 3*time.Second, "output", screenHas(p, "한글ab"))
	lines := strings.Split(p.Render(), "\n")
	if len(lines) != 5 {
		t.Fatalf("Render() has %d lines, want 5", len(lines))
	}
	for i, l := range lines {
		if w := textwidth.Width(l); w != 20 {
			t.Fatalf("line %d width %d, want 20: %q", i, w, l)
		}
	}
	if want := "한글ab" + strings.Repeat(" ", 14); lines[0] != want {
		t.Fatalf("first line = %q, want %q", lines[0], want)
	}
	if p.Program() != "sh" {
		t.Fatalf("Program() = %q", p.Program())
	}
	if x, y, vis := p.Cursor(); x != 6 || y != 0 || !vis {
		t.Fatalf("Cursor() = %d,%d,%v, want 6,0,true", x, y, vis)
	}
}

func TestPaneResizeSignalsChild(t *testing.T) {
	// Without Setsid/Setctty the child has no controlling terminal and never
	// gets SIGWINCH, so this test fails.
	p := startSh(t, `trap "echo WINCH" WINCH; echo READY; while :; do sleep 0.1; done`, Size{W: 80, H: 24}, PaneOptions{})
	waitFor(t, 3*time.Second, "READY", screenHas(p, "READY"))
	if err := p.Resize(Size{W: 100, H: 30}); err != nil {
		t.Fatalf("Resize: %v", err)
	}
	waitFor(t, time.Second, "WINCH", screenHas(p, "WINCH"))
	if s := p.Size(); s != (Size{W: 100, H: 30}) {
		t.Fatalf("Size() = %+v", s)
	}
	if n := len(strings.Split(p.Render(), "\n")); n != 30 {
		t.Fatalf("Render() lines = %d, want 30", n)
	}
}

func TestPaneNeverBlocksOnStuckChild(t *testing.T) {
	p, err := StartPane(Request{Program: "sleep", Args: []string{"1000"}}, Size{W: 80, H: 24}, PaneOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close(); <-p.exited })
	stop := make(chan struct{})
	renders := make(chan int, 1)
	go func() {
		n := 0
		for {
			select {
			case <-stop:
				renders <- n
				return
			default:
				_ = p.Render()
				n++
			}
		}
	}()
	start := time.Now()
	k := tea.KeyPressMsg{Code: 'a', Text: "a"}
	for range 10000 {
		p.SendKey(k)
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("10000 SendKey took %v, want < 1s", d)
	}
	time.Sleep(100 * time.Millisecond) // the pane works through the queued keys
	close(stop)
	if n := <-renders; n == 0 {
		t.Fatal("Render() never completed while the stuck child was flooded")
	}
	p.SendPaste(strings.Repeat("x", 100000))
	start = time.Now()
	_ = p.Render()
	if d := time.Since(start); d > 500*time.Millisecond {
		t.Fatalf("Render() blocked %v behind a stuck child", d)
	}
}

func TestPaneEnv(t *testing.T) {
	t.Setenv("TERM", "dumb")
	p, err := StartPane(Request{Program: "sh", Args: []string{"-c", `echo "T=$TERM C=$COLORTERM M=$MASIL_PANE E=$EXTRA"; sleep 30`}, Env: []string{"EXTRA=1", "TERM=vt100"}},
		Size{W: 80, H: 5}, PaneOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close(); <-p.exited })
	waitFor(t, 3*time.Second, "env line", screenHas(p, "T=xterm-256color C=truecolor M=1 E=1"))
}

func TestPaneIgnoresClipboardAndLinks(t *testing.T) {
	p := startSh(t, `printf '\033]52;c;aGk=\a\033]8;;http://example.com\ahello\033]8;;\a'; sleep 30`, Size{W: 40, H: 4}, PaneOptions{})
	waitFor(t, 3*time.Second, "hello", screenHas(p, "hello"))
	r := p.Render()
	if strings.Contains(r, "\x1b]52") || strings.Contains(r, "\x1b]8") {
		t.Fatalf("Render() leaks OSC 52/8: %q", r)
	}
	for _, l := range p.Scrollback() {
		if strings.Contains(l, "\x1b]") {
			t.Fatalf("Scrollback() leaks OSC: %q", l)
		}
	}
}

func TestPaneTitleSanitized(t *testing.T) {
	p := startSh(t, `printf '\033]2;a\342\200\256b\aT'; sleep 30`, Size{W: 20, H: 3}, PaneOptions{})
	waitFor(t, 3*time.Second, "title", func() bool { return p.Title() != "" })
	if got := p.Title(); got != "ab" {
		t.Fatalf("Title() = %q, want ab", got)
	}
}

func TestPaneSurvivesZeroSize(t *testing.T) {
	p := startSh(t, "echo READY; sleep 30", Size{W: 80, H: 24}, PaneOptions{})
	waitFor(t, 3*time.Second, "READY", screenHas(p, "READY"))
	for _, s := range []Size{{0, 0}, {-1, 5}, {10, 0}} {
		if err := p.Resize(s); err != nil {
			t.Fatalf("Resize(%+v) = %v, want nil (ignored)", s, err)
		}
	}
	if s := p.Size(); s != (Size{W: 80, H: 24}) {
		t.Fatalf("Size() after zero resize = %+v", s)
	}
	if err := p.Resize(Size{W: 80, H: 24}); err != nil {
		t.Fatal(err)
	}
	_ = p.Render()
	select {
	case <-p.exited:
		t.Fatal("process died after a zero-size resize")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestPaneNoBytesLogged(t *testing.T) {
	lw := &lockedWriter{w: &bytes.Buffer{}}
	logger := slog.New(slog.NewTextHandler(lw, &slog.HandlerOptions{Level: slog.LevelDebug}))
	p := startSh(t, "printf 'SECRET-123'; read x; printf 'SECRET-456'", Size{W: 40, H: 4}, PaneOptions{Logger: logger})
	waitFor(t, 3*time.Second, "secret", screenHas(p, "SECRET-123"))
	p.SendKey(tea.KeyPressMsg{Code: 'q', Text: "q"})
	p.SendPaste("TYPED-789")
	p.SendKey(enter())
	waitExited(t, p, 5*time.Second)
	out := lw.String()
	for _, s := range []string{"SECRET-123", "SECRET-456", "TYPED-789"} {
		if strings.Contains(out, s) {
			t.Fatalf("log contains pane bytes %q:\n%s", s, out)
		}
	}
	if !strings.Contains(out, "sh") {
		t.Fatalf("log has no program name (expected start/exit records):\n%s", out)
	}
}

type lockedWriter struct {
	mu sync.Mutex
	w  *bytes.Buffer
}

func (l *lockedWriter) Write(b []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(b)
}

func (l *lockedWriter) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.String()
}

func TestPaneExitCode(t *testing.T) {
	p, err := StartPane(Request{Program: "sh", Args: []string{"-c", "echo bye; exit 4"}, Title: "my sh"}, Size{W: 20, H: 3}, PaneOptions{})
	if err != nil {
		t.Fatal(err)
	}
	info := waitExited(t, p, 5*time.Second)
	if info.Code != 4 || info.Mode != ModePane || info.Title != "my sh" || info.Program != "sh" || info.Err != nil {
		t.Fatalf("ExitInfo = %+v", info)
	}
	if !strings.Contains(plain(p.Render()), "bye") {
		t.Fatalf("final output lost: %q", plain(p.Render()))
	}
	// Input after exit is dropped, never blocks.
	p.SendKey(enter())
	p.SendPaste("x")
	if err := p.Resize(Size{W: 30, H: 4}); err != nil {
		t.Fatal(err)
	}
	if n := len(strings.Split(p.Render(), "\n")); n != 4 {
		t.Fatalf("Render() after exit has %d lines", n)
	}
	if err := p.Close(); err != nil {
		t.Fatalf("Close after exit: %v", err)
	}
	if LivePanes() != 0 {
		t.Fatalf("LivePanes() = %d after exit", LivePanes())
	}
}

func TestCloseEscalates(t *testing.T) {
	restore := closeGrace
	closeGrace = 100 * time.Millisecond
	t.Cleanup(func() { closeGrace = restore })
	p, err := StartPane(Request{Program: "sh", Args: []string{"-c", `trap "" HUP; echo READY; sleep 1000`}}, Size{W: 20, H: 3}, PaneOptions{})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, 3*time.Second, "READY", screenHas(p, "READY"))
	start := time.Now()
	if err := p.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	info := waitExited(t, p, 3*time.Second)
	if d := time.Since(start); d < 100*time.Millisecond {
		t.Fatalf("exited after %v: HUP was not ignored, test is not exercising SIGKILL", d)
	}
	if info.Code != -1 {
		t.Fatalf("ExitInfo = %+v, want killed (-1)", info)
	}
}

func TestCloseHangsUp(t *testing.T) {
	p, err := StartPane(Request{Program: "sleep", Args: []string{"1000"}}, Size{W: 20, H: 3}, PaneOptions{})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	_ = p.Close()
	waitExited(t, p, 3*time.Second)
	if d := time.Since(start); d > time.Second {
		t.Fatalf("SIGHUP exit took %v", d)
	}
}

func TestCloseAllPanes(t *testing.T) {
	for range 2 {
		if _, err := StartPane(Request{Program: "sleep", Args: []string{"1000"}}, Size{W: 20, H: 3}, PaneOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	if LivePanes() != 2 {
		t.Fatalf("LivePanes() = %d", LivePanes())
	}
	CloseAll(500 * time.Millisecond)
	if n := LivePanes(); n != 0 {
		t.Fatalf("LivePanes() after CloseAll = %d", n)
	}
}

func TestPaneReportsOSC7(t *testing.T) {
	var mu sync.Mutex
	var got []string
	opts := PaneOptions{OnCwd: func(dir string) { mu.Lock(); got = append(got, dir); mu.Unlock() }}
	p := startSh(t, `printf '\033]7;file://host/tmp/x%%20y\a'; printf '\033]7;file://host/tmp/x%%20y\a'; printf '\033]7;http://host/etc\a'; printf '\033]7;/plain\a'; printf '\033]7;file://h/tmp/z\a'; echo DONE; sleep 30`,
		Size{W: 40, H: 4}, opts)
	waitFor(t, 3*time.Second, "DONE", screenHas(p, "DONE"))
	waitFor(t, time.Second, "two cwd reports", func() bool { mu.Lock(); defer mu.Unlock(); return len(got) >= 2 })
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(got, "|") != "/tmp/x y|/tmp/z" {
		t.Fatalf("OnCwd calls = %q, want [/tmp/x y /tmp/z]", got)
	}
}

func TestPaneFixedSize(t *testing.T) {
	p := startSh(t, `echo READY; read x; stty size; read x; stty size; sleep 30`, Size{W: 80, H: 24}, PaneOptions{})
	waitFor(t, 3*time.Second, "READY", screenHas(p, "READY"))
	p.SetFixedSize(Size{W: 100, H: 30})
	if err := p.Resize(Size{W: 80, H: 24}); err != nil {
		t.Fatal(err)
	}
	p.SendKey(enter())
	waitFor(t, 3*time.Second, "stty size", screenHas(p, "30 100"))
	lines := strings.Split(p.Render(), "\n")
	if len(lines) != 24 || textwidth.Width(lines[0]) != 80 {
		t.Fatalf("Render() = %d lines x %d cols, want 24x80", len(lines), textwidth.Width(lines[0]))
	}
	if s := p.Size(); s != (Size{W: 100, H: 30}) {
		t.Fatalf("Size() = %+v, want 100x30", s)
	}

	p.SetFixedSize(Size{})
	if err := p.Resize(Size{W: 90, H: 20}); err != nil {
		t.Fatal(err)
	}
	if s := p.Size(); s != (Size{W: 90, H: 20}) {
		t.Fatalf("Size() after unfix = %+v, want 90x20", s)
	}
	p.SendKey(enter())
	waitFor(t, 3*time.Second, "second stty size", screenHas(p, "20 90"))
}

func TestPaneFixedSmallerPads(t *testing.T) {
	p := startSh(t, "echo READY; sleep 30", Size{W: 80, H: 24}, PaneOptions{})
	waitFor(t, 3*time.Second, "READY", screenHas(p, "READY"))
	p.SetFixedSize(Size{W: 10, H: 3})
	_ = p.Resize(Size{W: 20, H: 6})
	lines := strings.Split(p.Render(), "\n")
	if len(lines) != 6 || textwidth.Width(lines[5]) != 20 {
		t.Fatalf("padded Render() = %d lines, last width %d", len(lines), textwidth.Width(lines[len(lines)-1]))
	}
}

func TestPaneNotifyBell(t *testing.T) {
	var mu sync.Mutex
	var got []string
	opts := PaneOptions{OnNotify: func(kind string) { mu.Lock(); got = append(got, kind); mu.Unlock() }}
	p := startSh(t, `printf '\a'; printf '\033]9;done\a'; printf '\033]777;notify;t;b\a'; printf '\a\033]9;again\a'; echo DONE; sleep 30`, Size{W: 40, H: 4}, opts)
	waitFor(t, 3*time.Second, "DONE", screenHas(p, "DONE"))
	waitFor(t, time.Second, "three notifications", func() bool { mu.Lock(); defer mu.Unlock(); return len(got) >= 3 })
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(got, ",") != "bel,osc9,osc777" {
		t.Fatalf("OnNotify = %v, want [bel osc9 osc777] (repeats within 1s folded)", got)
	}
	if strings.Contains(plain(p.Render()), "done") || strings.Contains(plain(p.Render()), "notify") {
		t.Fatal("notification bodies were drawn")
	}
}

func TestPaneBracketedPaste(t *testing.T) {
	p := startSh(t, `printf A; read x; printf '\033[?2004h'; printf B; read x; printf '\033[?2004l'; printf C; sleep 30`, Size{W: 20, H: 3}, PaneOptions{})
	waitFor(t, 3*time.Second, "A", screenHas(p, "A"))
	if p.BracketedPaste() {
		t.Fatal("BracketedPaste() true before the child enabled it")
	}
	p.SendKey(enter())
	waitFor(t, 3*time.Second, "B", screenHas(p, "B"))
	if !p.BracketedPaste() {
		t.Fatal("BracketedPaste() false after ?2004h")
	}
	p.SendKey(enter())
	waitFor(t, 3*time.Second, "C", screenHas(p, "C"))
	if p.BracketedPaste() {
		t.Fatal("BracketedPaste() true after ?2004l")
	}
}

func TestPaneMouseModeAndInput(t *testing.T) {
	p := startSh(t, `printf '\033[?1000h\033[?1006hM'; sleep 30`, Size{W: 20, H: 3}, PaneOptions{})
	waitFor(t, 3*time.Second, "M", screenHas(p, "M"))
	if !p.MouseMode() {
		t.Fatal("MouseMode() false after ?1000h")
	}
	p.SendMouse(tea.MouseClickMsg{X: 50, Y: 40, Button: tea.MouseLeft}, 3, 1)
	p.SetFocus(true)
	p.SetFocus(false)
}

func TestPaneOnDirtyBatches(t *testing.T) {
	var mu sync.Mutex
	var n int
	p := startSh(t, `i=0; while [ $i -lt 200 ]; do echo line$i; i=$((i+1)); done; sleep 30`, Size{W: 20, H: 5},
		PaneOptions{OnDirty: func() { mu.Lock(); n++; mu.Unlock() }})
	waitFor(t, 3*time.Second, "line199", screenHas(p, "line199"))
	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if n == 0 || n > 60 {
		t.Fatalf("OnDirty called %d times, want a few batched calls", n)
	}
	if len(p.Scrollback()) == 0 {
		t.Fatal("no scrollback after 200 lines")
	}
}

func TestStartPaneMissingProgram(t *testing.T) {
	_, err := StartPane(Request{Program: "/nonexistent/masil-no-such-program"}, Size{W: 10, H: 3}, PaneOptions{})
	if err == nil || errors.Is(err, ErrPaneLimit) {
		t.Fatalf("err = %v, want a start error", err)
	}
	if LivePanes() != 0 {
		t.Fatalf("LivePanes() = %d after a failed start", LivePanes())
	}
}
