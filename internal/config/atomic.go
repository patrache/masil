package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
)

// WriteFileAtomic writes data to a temp file ".<name>.tmp-<pid>" in the same
// directory, fsyncs it and renames it over path.
//
// The temp name carries an extra random suffix (".<name>.tmp-<pid>-<rand>") so
// that concurrent writers inside one process never share a temp file; the
// last rename wins and readers always see one complete payload.
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
	if err = os.Rename(tmp, path); err != nil {
		return fmt.Errorf("config: atomic write %s: rename: %w", path, err)
	}
	return nil
}
