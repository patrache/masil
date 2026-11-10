package fsroot

import (
	"errors"
	"io/fs"
	"sort"
)

// MaxDirEntries caps one folder listing.
const MaxDirEntries = 5000

// Entry is one listed child. Denied covers deny-list names, special files
// (FIFO, device, socket) and links whose real target is denied, special or
// outside the folder; Ignored covers ignore rules and
// the rule files themselves (both kinds are hidden from peers).
type Entry struct {
	Name    string
	Dir     bool
	Size    int64
	Denied  bool
	Ignored bool
}

// List lists one folder: folders first, then byte order of names. For a peer
// denied and ignored entries are left out; for the owner they are marked.
// Past MaxDirEntries the rest is only counted in more.
func (r *Root) List(dirRel string, forPeer bool) (entries []Entry, more int, err error) {
	return r.list(dirRel, forPeer, forPeer)
}

// list is List; strict makes an ignore-rule error fail the listing even in
// the owner's (marking) view.
func (r *Root) list(dirRel string, forPeer, strict bool) (entries []Entry, more int, err error) {
	clean, err := r.Check(dirRel, forPeer)
	if err != nil {
		return nil, 0, err
	}
	_, real, err := r.resolve(clean)
	if err != nil {
		return nil, 0, err
	}
	f, err := r.root.Open(osPath(real))
	if err != nil {
		return nil, 0, err
	}
	des, err := f.ReadDir(-1)
	_ = f.Close()
	if err != nil {
		return nil, 0, err
	}

	all := make([]Entry, 0, len(des))
	var qRels []string
	var qDirs []bool
	var qIdx []int
	for _, de := range des {
		name := de.Name()
		child := name
		if clean != "." {
			child = clean + "/" + name
		}
		// realChild uses the stored names and resolved links of the folder,
		// so a peer's other spelling of dirRel cannot dodge the rules.
		realChild := name
		if real != "." {
			realChild = real + "/" + name
		}
		e := Entry{Name: name, Dir: de.IsDir(), Denied: Denied(child) || Denied(realChild)}
		if de.Type()&fs.ModeSymlink != 0 {
			e.Dir = false
			if target, ferr := r.follow(child, realChild); ferr != nil {
				e.Denied = true
			} else {
				if Denied(target) {
					e.Denied = true
				}
				if fi, serr := r.root.Stat(osPath(target)); serr == nil {
					e.Dir, e.Size = fi.IsDir(), sizeOf(fi)
					if special(fi) {
						e.Denied = true
					}
				}
				if target != child && target != realChild {
					qRels, qDirs, qIdx = append(qRels, target), append(qDirs, e.Dir), append(qIdx, len(all))
				}
			}
		} else if fi, ierr := de.Info(); ierr == nil {
			e.Size = sizeOf(fi)
			if special(fi) {
				e.Denied = true
			}
		}
		if isRuleFile(child) {
			e.Ignored = true
		}
		qRels, qDirs, qIdx = append(qRels, child), append(qDirs, e.Dir), append(qIdx, len(all))
		if realChild != child {
			qRels, qDirs, qIdx = append(qRels, realChild), append(qDirs, e.Dir), append(qIdx, len(all))
		}
		all = append(all, e)
	}
	if ig := r.ignorer(); ig != nil && len(qRels) > 0 {
		res, ierr := ig.Ignored(qRels, qDirs)
		if ierr != nil && strict {
			return nil, 0, ierr
		}
		for i, v := range res {
			if v {
				all[qIdx[i]].Ignored = true
			}
		}
	}

	out := all[:0]
	for _, e := range all {
		if forPeer && (e.Denied || e.Ignored) {
			continue
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Dir != out[j].Dir {
			return out[i].Dir
		}
		return out[i].Name < out[j].Name
	})
	if len(out) > MaxDirEntries {
		more = len(out) - MaxDirEntries
		out = out[:MaxDirEntries]
	}
	return out, more, nil
}

// special reports FIFOs, devices and sockets: never opened (a FIFO open
// blocks), so they are marked Denied (owner sees the lock, peers do not see
// them).
func special(fi fs.FileInfo) bool { return !fi.IsDir() && !fi.Mode().IsRegular() }

func sizeOf(fi fs.FileInfo) int64 {
	if fi.IsDir() {
		return 0
	}
	return fi.Size()
}

// MaxSubtreeEntries bounds the walk of CheckSubtree; a bigger folder is
// refused.
const MaxSubtreeEntries = 20000

// CheckSubtree refuses (a PathError on rel) a participant's rename or
// delete of a folder holding anything hidden from participants: renaming
// the folder would move its denied and ignored entries along, out from
// under the ignore rules that hid them (v0.1.3 security (w)). Denied entries
// give ReasonDenied; ignored ones, rule files, an ignore-rule error or more
// than MaxSubtreeEntries entries give ReasonIgnored. A file, a link (renamed
// itself) or a missing path is nil. Links inside are not descended: their
// targets do not move.
func (r *Root) CheckSubtree(rel string) error {
	clean, err := cleanRel(rel)
	if err != nil {
		return err
	}
	fi, err := r.root.Lstat(osPath(clean))
	if err != nil || !fi.IsDir() {
		return nil
	}
	seen := 0
	queue := []string{clean}
	for len(queue) > 0 {
		dir := queue[0]
		queue = queue[1:]
		entries, more, err := r.list(dir, false, true)
		if err != nil {
			var pe *PathError
			if errors.As(err, &pe) && pe.Reason == ReasonDenied {
				return reject(rel, ReasonDenied)
			}
			return reject(rel, ReasonIgnored)
		}
		seen += len(entries) + more
		if more > 0 || seen > MaxSubtreeEntries {
			return reject(rel, ReasonIgnored)
		}
		for _, e := range entries {
			if e.Denied {
				return reject(rel, ReasonDenied)
			}
		}
		for _, e := range entries {
			if e.Ignored {
				return reject(rel, ReasonIgnored)
			}
		}
		for _, e := range entries {
			if !e.Dir {
				continue
			}
			child := e.Name
			if dir != "." {
				child = dir + "/" + e.Name
			}
			if lfi, err := r.root.Lstat(osPath(child)); err == nil && lfi.IsDir() {
				queue = append(queue, child)
			}
		}
	}
	return nil
}
