package daemon

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/patrache/masil/internal/config"
)

// logFileName is the daemon's log under config.Paths.LogDir (CA 3.2).
const logFileName = "daemon.log"

// logRotateMax: a write that would push daemon.log past this rotates it to
// daemon.log.1 first (CA 3.2 "5MB 넘으면 .1 로 한 번 회전").
const logRotateMax = 5 << 20

func logFilePath(p config.Paths) string {
	return filepath.Join(p.LogDir, logFileName)
}

// NewLogger writes slog text to <LogDir>/daemon.log (rotated once to daemon.log.1
// when it would exceed 5 MiB) and, if extra != nil, also to extra (foreground
// --verbose).
func NewLogger(p config.Paths, extra io.Writer) (*slog.Logger, io.Closer, error) {
	if err := config.EnsureDir(p); err != nil {
		return nil, nil, err
	}
	f, err := openRotating(logFilePath(p), logRotateMax)
	if err != nil {
		return nil, nil, err
	}
	var w io.Writer = f
	if extra != nil {
		w = io.MultiWriter(f, extra)
	}
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: slog.LevelInfo})), f, nil
}

// rotatingFile is an append-only 0600 file that moves itself to "<path>.1"
// (replacing any previous one) before a write that would make it exceed max.
// A daemon runs for days, so rotating only at start (as the TUI does) would
// let daemon.log grow without bound.
type rotatingFile struct {
	mu   sync.Mutex
	path string
	max  int64
	f    *os.File
	size int64
}

func openRotating(path string, max int64) (*rotatingFile, error) {
	r := &rotatingFile{path: path, max: max}
	if err := r.open(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *rotatingFile) open() error {
	f, err := os.OpenFile(r.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("daemon: open log: %w", err)
	}
	_ = f.Chmod(0o600) // an existing file may be wider
	st, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return fmt.Errorf("daemon: stat log: %w", err)
	}
	r.f, r.size = f, st.Size()
	return nil
}

// Write appends p, rotating first when the file is non-empty and p would push
// it past max. A single write larger than max still lands whole in a fresh file.
func (r *rotatingFile) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.f == nil {
		return 0, os.ErrClosed
	}
	if r.size > 0 && r.size+int64(len(p)) > r.max {
		if err := r.rotate(); err != nil {
			return 0, err
		}
	}
	n, err := r.f.Write(p)
	r.size += int64(n)
	return n, err
}

// rotate closes the current file (Windows cannot rename an open file), moves
// it to "<path>.1" and reopens an empty one. If the rename fails the old file
// is reopened and its size counted from zero, so the next attempt comes only
// after another max bytes instead of on every write.
func (r *rotatingFile) rotate() error {
	_ = r.f.Close()
	r.f = nil
	renameErr := os.Rename(r.path, r.path+".1")
	if err := r.open(); err != nil {
		return err
	}
	if renameErr != nil {
		r.size = 0
	}
	return nil
}

func (r *rotatingFile) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.f == nil {
		return nil
	}
	err := r.f.Close()
	r.f = nil
	return err
}
