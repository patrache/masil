package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"time"
)

// renameBudget bounds how long WriteFileAtomic keeps retrying a rename that
// fails with a transient error (Windows only; see isTransientRenameErr).
const renameBudget = 500 * time.Millisecond

// renameRetry calls rename(oldpath, newpath) and, while it fails with an error
// for which transient reports true, retries with a small growing backoff until
// budget has elapsed (like cmd/go/internal/robustio). It returns the last error.
func renameRetry(rename func(string, string) error, transient func(error) bool, oldpath, newpath string, budget time.Duration) error {
	start := time.Now()
	delay := time.Millisecond
	for {
		err := rename(oldpath, newpath)
		if err == nil || !transient(err) {
			return err
		}
		left := budget - time.Since(start)
		if left <= 0 {
			return err
		}
		time.Sleep(min(delay, left))
		delay = min(2*delay, 50*time.Millisecond)
	}
}

// WriteFileAtomic writes data to a temp file ".<name>.tmp-<pid>" in the same
// directory, fsyncs it and renames it over path.
//
// The temp name carries an extra random suffix (".<name>.tmp-<pid>-<rand>") so
// that concurrent writers inside one process never share a temp file; the
// last rename wins and readers always see one complete payload.
//
// On Windows the rename is retried for up to renameBudget while it fails with
// ERROR_ACCESS_DENIED / ERROR_SHARING_VIOLATION / ERROR_FILE_NOT_FOUND (another
// process has the target open, or a concurrent rename to it is in flight); the
// temp file is removed only after the retries give up.
func WriteFileAtomic(path string, data []byte, perm os.FileMode) (err error) {
	dir, base := filepath.Split(path)
	if dir == "" {
		dir = "."
	}
	f, err := os.CreateTemp(dir, "."+base+".tmp-"+strconv.Itoa(os.Getpid())+"-*")
	if err != nil {
		return fmt.Errorf("config: atomic write %s: %w", path, err)
	}
	tmp := f.Name()
	defer func() {
		if err != nil {
			_ = f.Close()
			_ = os.Remove(tmp)
		}
	}()
	if _, err = f.Write(data); err != nil {
		return fmt.Errorf("config: atomic write %s: %w", path, err)
	}
	if err = f.Sync(); err != nil {
		return fmt.Errorf("config: atomic write %s: fsync: %w", path, err)
	}
	if err = f.Close(); err != nil {
		return fmt.Errorf("config: atomic write %s: close: %w", path, err)
	}
	if runtime.GOOS != "windows" {
		if err = os.Chmod(tmp, perm); err != nil {
			return fmt.Errorf("config: atomic write %s: chmod: %w", path, err)
		}
	}
	if err = renameRetry(os.Rename, isTransientRenameErr, tmp, path, renameBudget); err != nil {
		return fmt.Errorf("config: atomic write %s: rename: %w", path, err)
	}
	return nil
}
