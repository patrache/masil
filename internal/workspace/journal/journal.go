// Package journal keeps unsaved shared-document text on the owner's PC so a
// crash does not lose it: <cache>/workspace-journal/<folder hash>/<path
// hash>.json, 0600 files in 0700 folders, written atomically.
package journal

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/patrache/masil/internal/config"
	"github.com/patrache/masil/internal/workspace/fsroot"
)

// Entry is one unsaved document: its text and the disk fingerprint it was
// based on.
type Entry struct {
	Rel  string             `json:"rel"`
	Base fsroot.Fingerprint `json:"base"`
	Text string             `json:"text"`
	At   time.Time          `json:"at"`
}

// Journal is the recovery folder of one shared folder.
type Journal struct {
	dir string
}

func hash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// Open opens (creating) the journal of folderAbs under cacheDir.
func Open(cacheDir, folderAbs string) (*Journal, error) {
	dir := filepath.Join(cacheDir, "workspace-journal", hash(folderAbs)[:16])
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(dir, 0o700); err != nil {
			return nil, err
		}
	}
	return &Journal{dir: dir}, nil
}

func (j *Journal) path(rel string) string { return filepath.Join(j.dir, hash(rel)+".json") }

// Put stores (replacing) the unsaved text of rel.
func (j *Journal) Put(rel, text string, base fsroot.Fingerprint) error {
	data, err := json.Marshal(Entry{Rel: rel, Base: base, Text: text, At: time.Now().UTC()})
	if err != nil {
		return err
	}
	return config.WriteFileAtomic(j.path(rel), data, 0o600)
}

// Drop forgets rel (after a save or a discard). Missing entries are fine.
func (j *Journal) Drop(rel string) error {
	err := os.Remove(j.path(rel))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// Pending lists the stored entries sorted by path; unreadable files are
// skipped.
func (j *Journal) Pending() ([]Entry, error) {
	des, err := os.ReadDir(j.dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Entry
	for _, de := range des {
		name := de.Name()
		if !de.Type().IsRegular() || !strings.HasSuffix(name, ".json") || strings.HasPrefix(name, ".") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(j.dir, name))
		if err != nil {
			continue
		}
		var e Entry
		if json.Unmarshal(data, &e) != nil || e.Rel == "" || hash(e.Rel)+".json" != name {
			continue
		}
		out = append(out, e)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Rel < out[b].Rel })
	return out, nil
}
