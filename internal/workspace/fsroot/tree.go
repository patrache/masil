package fsroot

import (
	"io/fs"
	"sort"
)

// MaxDirEntries caps one folder listing.
const MaxDirEntries = 5000

// Entry is one listed child. Denied covers deny-list names and links whose
// real target is denied or outside the folder; Ignored covers ignore rules and
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
				}
				if target != child && target != realChild {
					qRels, qDirs, qIdx = append(qRels, target), append(qDirs, e.Dir), append(qIdx, len(all))
				}
			}
		} else if fi, ierr := de.Info(); ierr == nil {
			e.Size = sizeOf(fi)
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
		if ierr != nil && forPeer {
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

func sizeOf(fi fs.FileInfo) int64 {
	if fi.IsDir() {
		return 0
	}
	return fi.Size()
}
