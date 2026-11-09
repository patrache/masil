package secrets

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"runtime"

	"github.com/patrache/masil/internal/config"
)

// The SecretsDir files (A9.2): the 0600 value file (only after consent), the
// name index (no values) and a .gitignore so a dotfiles repository never picks
// them up.
const (
	fileValues   = "tools.json"
	fileIndex    = "index.json"
	fileIgnore   = ".gitignore"
	ignoreText   = "*\n"
	fileVersion  = 1
	maxFileBytes = 1 << 20
)

// errNoFolder: the file fallback needs a secrets folder (bare tests have none).
var errNoFolder = errors.New("secrets: no secrets folder for the file fallback")

type valuesFile struct {
	Version int               `json:"version"`
	Values  map[string]string `json:"values"`
}

type indexFile struct {
	Version int                `json:"version"`
	Entries map[string]Backend `json:"entries"` // account => backend
}

// readJSON reads a SecretsDir file into v; a missing file is (false, nil), a
// broken or oversized one is logged and reads as missing.
func (s *Store) readJSON(name string, v any) bool {
	if s.dir == "" {
		return false
	}
	path := filepath.Join(s.dir, name)
	f, err := os.Open(path)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			slog.Warn("secrets: read", "file", name, "err", err)
		}
		return false
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxFileBytes+1))
	if err != nil {
		slog.Warn("secrets: read", "file", name, "err", err)
		return false
	}
	if len(data) > maxFileBytes || json.Unmarshal(data, v) != nil {
		// The decode error is not logged: it can quote the file's characters.
		slog.Warn("secrets: unreadable file ignored", "file", name)
		return false
	}
	return true
}

// writeJSON writes v to a SecretsDir file atomically, 0600, in a 0700 folder
// with the .gitignore.
func (s *Store) writeJSON(name string, v any) error {
	if s.dir == "" {
		return errNoFolder
	}
	if err := s.ensureDir(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("secrets: encode %s: %w", name, err)
	}
	if err := config.WriteFileAtomic(filepath.Join(s.dir, name), append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("secrets: write %s: %w", name, err)
	}
	return nil
}

// ensureDir creates SecretsDir 0700 (narrowing wider bits) and its .gitignore.
func (s *Store) ensureDir() error {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return fmt.Errorf("secrets: create folder: %w", err)
	}
	if runtime.GOOS != "windows" {
		st, err := os.Stat(s.dir)
		if err != nil {
			return fmt.Errorf("secrets: stat folder: %w", err)
		}
		if st.Mode().Perm() != 0o700 {
			if err := os.Chmod(s.dir, 0o700); err != nil {
				return fmt.Errorf("secrets: chmod folder: %w", err)
			}
		}
	}
	gi := filepath.Join(s.dir, fileIgnore)
	if b, err := os.ReadFile(gi); err == nil && string(b) == ignoreText {
		return nil
	}
	if err := config.WriteFileAtomic(gi, []byte(ignoreText), 0o600); err != nil {
		return fmt.Errorf("secrets: write .gitignore: %w", err)
	}
	return nil
}

// readValuesLocked is tools.json's values (nil when there is none).
func (s *Store) readValuesLocked() map[string]string {
	var f valuesFile
	if !s.readJSON(fileValues, &f) {
		return nil
	}
	return f.Values
}

func (s *Store) getFileLocked(acct string) (string, bool) {
	v, ok := s.readValuesLocked()[acct]
	return v, ok
}

func (s *Store) putFileLocked(acct, value string) error {
	if s.dir == "" {
		return errNoFolder
	}
	vals := maps.Clone(s.readValuesLocked())
	if vals == nil {
		vals = map[string]string{}
	}
	vals[acct] = value
	return s.writeJSON(fileValues, valuesFile{Version: fileVersion, Values: vals})
}

// dropFileLocked removes acct from tools.json (the file goes when it empties).
func (s *Store) dropFileLocked(acct string) {
	vals := s.readValuesLocked()
	if _, ok := vals[acct]; !ok {
		return
	}
	delete(vals, acct)
	var err error
	if len(vals) == 0 {
		err = os.Remove(filepath.Join(s.dir, fileValues))
	} else {
		err = s.writeJSON(fileValues, valuesFile{Version: fileVersion, Values: vals})
	}
	if err != nil {
		slog.Warn("secrets: drop a file value", "account", acct, "err", err)
	}
}

// indexLocked is the name index, read from index.json on first use.
func (s *Store) indexLocked() map[string]Backend {
	if !s.loaded {
		s.loaded = true
		var f indexFile
		if s.readJSON(fileIndex, &f) && f.Entries != nil {
			s.index = f.Entries
		}
		if s.index == nil {
			s.index = map[string]Backend{}
		}
	}
	return s.index
}

func (s *Store) setIndexLocked(acct string, b Backend) {
	idx := s.indexLocked()
	if idx[acct] == b {
		return
	}
	idx[acct] = b
	s.saveIndexLocked()
}

func (s *Store) deleteIndexLocked(acct string) {
	idx := s.indexLocked()
	if _, ok := idx[acct]; !ok {
		return
	}
	delete(idx, acct)
	s.saveIndexLocked()
}

// saveIndexLocked writes index.json (in memory only without a folder). A
// failure is only logged: the index is a name list, the value is stored.
func (s *Store) saveIndexLocked() {
	if s.dir == "" {
		return
	}
	if err := s.writeJSON(fileIndex, indexFile{Version: fileVersion, Entries: s.index}); err != nil {
		slog.Warn("secrets: save index", "err", err)
	}
}
