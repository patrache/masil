package fsroot

import (
	"errors"
	"io/fs"
	"sort"
	"sync"
)

// Change is one outside change to a tracked file.
type Change struct {
	Rel     string
	Deleted bool
}

// Watcher notices outside changes to tracked files by polling (the engine
// calls Poll every 0.5 s). A size or mtime difference triggers a re-hash, and
// only a different hash is reported, so rewriting the same bytes is quiet.
// Files above MaxView are not read: any size or mtime change is a change.
type Watcher struct {
	r     *Root
	mu    sync.Mutex
	files map[string]Fingerprint
}

// NewWatcher watches files of r.
func NewWatcher(r *Root) *Watcher { return &Watcher{r: r, files: map[string]Fingerprint{}} }

// Track starts (or re-bases, after a save) watching rel from fp; a zero fp
// means the file is absent.
func (w *Watcher) Track(rel string, fp Fingerprint) {
	w.mu.Lock()
	w.files[rel] = fp
	w.mu.Unlock()
}

// Untrack stops watching rel.
func (w *Watcher) Untrack(rel string) {
	w.mu.Lock()
	delete(w.files, rel)
	w.mu.Unlock()
}

// Poll returns the real changes since the last Poll (nil when none), sorted
// by path. Each change is reported once; a deleted file that comes back is a
// change again.
func (w *Watcher) Poll() []Change {
	w.mu.Lock()
	defer w.mu.Unlock()
	rels := make([]string, 0, len(w.files))
	for rel := range w.files {
		rels = append(rels, rel)
	}
	sort.Strings(rels)
	var out []Change
	for _, rel := range rels {
		old := w.files[rel]
		cur, ok := w.current(rel, old)
		if !ok {
			continue
		}
		w.files[rel] = cur
		if old.sameContent(cur) {
			continue
		}
		out = append(out, Change{Rel: rel, Deleted: cur.IsZero()})
	}
	return out
}

// current fingerprints rel; ok is false on transient errors (try next poll).
// It skips hashing when size and mtime still match old.
func (w *Watcher) current(rel string, old Fingerprint) (Fingerprint, bool) {
	_, real, err := w.r.resolve(rel)
	if err != nil {
		return Fingerprint{}, true
	}
	fi, err := w.r.root.Stat(osPath(real))
	if errors.Is(err, fs.ErrNotExist) {
		return Fingerprint{}, true
	}
	// A folder or a special file (FIFO, device, socket) is no longer an
	// editable file: reported like a deletion, never opened.
	if err != nil || fi.IsDir() || !fi.Mode().IsRegular() {
		return Fingerprint{}, err == nil
	}
	if !old.IsZero() && fi.Size() == old.Size && fi.ModTime().Equal(old.ModTime) {
		return old, true
	}
	fp, _, err := w.r.fingerprintOf(real, fi)
	if errors.Is(err, errNotRegular) {
		return Fingerprint{}, true
	}
	if err != nil {
		return Fingerprint{}, false
	}
	return fp, true
}
