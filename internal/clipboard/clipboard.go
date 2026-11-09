// Package clipboard writes the system clipboard through the platform's own
// program (pbcopy, wl-copy / xclip / xsel, clip.exe). The UI also sends OSC 52
// (tea.SetClipboard), which reaches the terminal across SSH but which many
// terminals ignore (macOS Terminal.app, iTerm2 by default, tmux without
// set-clipboard); this package is the path that works without the terminal.
//
// Every program is a constant (or $PATH-resolved constant name) with a
// constant argv; the text goes to its stdin only, never through a shell
// (commandFor builds the command, execRun in run.go is the only exec).
package clipboard

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"sync"
	"time"
	"unicode/utf8"
)

// MaxBytes caps one clipboard write (and the OSC 52 payload): enough for any
// chat selection, small enough for terminals and tmux that limit OSC 52.
const MaxBytes = 256 << 10

// writeTimeout bounds one program run (Write runs inside a tea.Cmd).
const writeTimeout = 2 * time.Second

var (
	// ErrRemote: the session is remote (SSH), so a local program would write
	// the remote machine's clipboard; OSC 52 is the only path.
	ErrRemote = errors.New("clipboard: remote session (OSC 52 only)")
	// ErrUnavailable: no clipboard program for this system.
	ErrUnavailable = errors.New("clipboard: no clipboard program")
)

// Writer puts text on the system clipboard. Write may block (up to a short
// timeout): call it off the Update goroutine.
type Writer interface {
	Write(text string) error
}

// System is the Writer for this machine.
func System() Writer {
	return &systemWriter{h: host{
		goos:    runtime.GOOS,
		getenv:  os.Getenv,
		environ: os.Environ,
		look:    lookPath,
		exists:  exists,
		sysDir:  systemDir,
	}, run: execRun}
}

// Clamp cuts s to at most n bytes on a rune boundary.
func Clamp(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// runFunc runs one command (execRun; tests use a fake).
type runFunc func(ctx context.Context, c command) error

// systemWriter writes through the program commandFor picks for h.
type systemWriter struct {
	h   host
	run runFunc
}

// Write runs the clipboard program with text (clamped to MaxBytes) on stdin.
func (w *systemWriter) Write(text string) error {
	c, err := commandFor(w.h, Clamp(text, MaxBytes))
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
	defer cancel()
	if err := w.run(ctx, c); err != nil {
		return fmt.Errorf("clipboard: %s: %w", c.name, err)
	}
	return nil
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// Recorder is a test Writer that records every write. Write may be called
// from several goroutines (tea.Cmds).
type Recorder struct {
	mu    sync.Mutex
	texts []string
	err   error
}

// Write records text and returns the error set by Fail (nil by default).
func (r *Recorder) Write(text string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.texts = append(r.texts, text)
	return r.err
}

// Fail makes every later Write return err (nil = succeed again).
func (r *Recorder) Fail(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.err = err
}

// Texts returns the recorded writes in order.
func (r *Recorder) Texts() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.texts...)
}
