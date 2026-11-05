package config

import "os"

// WriteFileAtomic writes data to a temp file ".<name>.tmp-<pid>" in the same
// directory, fsyncs it and renames it over path.
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	// TODO(core): Task 4
	return nil
}
