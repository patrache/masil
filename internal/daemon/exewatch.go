package daemon

import (
	"os"
	"path/filepath"
	"runtime"
)

// watchedExecutable is the path whose replacement means "restart on the new
// binary". Windows has no supervisor to relaunch the daemon, so watching is off
// there (""). Elsewhere an absolute os.Args[0] is used as is: launchd/systemd
// pass the registered <prefix>/bin/masil symlink, and only the unresolved link
// path shows brew upgrade's link swap. Otherwise os.Executable().
func watchedExecutable() string {
	if runtime.GOOS == "windows" {
		return ""
	}
	if len(os.Args) > 0 && filepath.IsAbs(os.Args[0]) {
		return os.Args[0]
	}
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return exe
}

// exeWatch notices when the file at path is replaced (new inode, size or
// modification time). It is used from one goroutine at a time.
type exeWatch struct {
	path    string
	base    os.FileInfo // what path pointed at when the daemon started
	pending os.FileInfo // a changed file seen once, waiting to be seen again unchanged
}

// newExeWatch returns nil (no watching) when path is "" or cannot be stat'ed.
func newExeWatch(path string) *exeWatch {
	if path == "" {
		return nil
	}
	fi, err := os.Stat(path)
	if err != nil {
		return nil
	}
	return &exeWatch{path: path, base: fi}
}

// changed reports whether path now holds a different, settled file. os.Stat
// follows symlinks. A missing file (mid-upgrade) is not a change, and a new
// file must look the same on two consecutive calls before it counts, so a
// half-written binary never triggers a restart.
func (w *exeWatch) changed() bool {
	if w == nil {
		return false
	}
	fi, err := os.Stat(w.path)
	if err != nil {
		w.pending = nil
		return false
	}
	if sameFile(fi, w.base) {
		w.pending = nil
		return false
	}
	if w.pending != nil && sameFile(fi, w.pending) {
		return true
	}
	w.pending = fi
	return false
}

func sameFile(a, b os.FileInfo) bool {
	return os.SameFile(a, b) && a.Size() == b.Size() && a.ModTime().Equal(b.ModTime())
}
