// Package fsroot is the owner-side gate to the shared folder: every file
// access goes through an os.Root, peer paths are cleaned and checked (links
// followed to their real target) against the secret deny list and the
// ignore rules, and text files are classified, saved atomically and watched
// for outside changes.
package fsroot

import (
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"sync"
)

// maxLinkHops bounds symlink resolution in Check (like the kernel's ELOOP).
const maxLinkHops = 40

// Root is an opened shared folder.
type Root struct {
	dir  string // absolute path as opened
	real string // dir with symlinks evaluated (for absolute link targets)
	root *os.Root

	mu  sync.RWMutex
	ign Ignorer
}

// Open opens dir (made absolute) as the folder root. The ignore rules come
// from git when the folder has a .git entry and git is installed, otherwise
// from the built-in parser.
func Open(dir string) (*Root, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(abs)
	if err != nil {
		return nil, err
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		real = abs
	}
	r := &Root{dir: abs, real: real, root: root}
	if _, err := root.Lstat(".git"); err == nil {
		if _, err := exec.LookPath("git"); err == nil {
			r.ign = NewGitIgnorer(abs, nil)
		}
	}
	if r.ign == nil {
		r.ign = NewFileIgnorer(r)
	}
	return r, nil
}

// SetIgnorer replaces the ignore rules (nil = nothing is ignored except the
// rule files themselves for peers).
func (r *Root) SetIgnorer(i Ignorer) {
	r.mu.Lock()
	r.ign = i
	r.mu.Unlock()
}

func (r *Root) ignorer() Ignorer {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.ign
}

// Close releases the root.
func (r *Root) Close() error { return r.root.Close() }

// Dir is the absolute folder path.
func (r *Root) Dir() string { return r.dir }

// Check validates a peer- or UI-supplied slash path and returns it cleaned.
// Order: empty/NUL/backslash -> Invalid; absolute -> Absolute; leaves the
// folder after path.Clean -> Escape; on the deny list -> Denied; links are
// then followed element by element (at most 40 hops): a real target outside
// the folder -> Escape, a denied real target -> Denied, a loop -> Invalid.
// With forPeer the ignore rules apply too (-> Ignored).
func (r *Root) Check(rel string, forPeer bool) (string, error) {
	clean, err := cleanRel(rel)
	if err != nil {
		return "", err
	}
	if Denied(clean) {
		return "", reject(rel, ReasonDenied)
	}
	real, err := r.follow(rel, clean)
	if err != nil {
		return "", err
	}
	if Denied(real) {
		return "", reject(rel, ReasonDenied)
	}
	if forPeer {
		if err := r.checkIgnored(rel, clean, real); err != nil {
			return "", err
		}
	}
	return clean, nil
}

// resolve is the owner-side gate used by the file operations: syntax and
// escape checks without the deny list. It returns the cleaned path and the
// real path after links.
func (r *Root) resolve(rel string) (clean, real string, err error) {
	clean, err = cleanRel(rel)
	if err != nil {
		return "", "", err
	}
	real, err = r.follow(rel, clean)
	if err != nil {
		return "", "", err
	}
	return clean, real, nil
}

// Lstat stats rel without following a final link. Escapes are refused both
// here and by the os.Root itself.
func (r *Root) Lstat(rel string) (fs.FileInfo, error) {
	clean, err := cleanRel(rel)
	if err != nil {
		return nil, err
	}
	return r.root.Lstat(osPath(clean))
}

func cleanRel(rel string) (string, error) {
	if rel == "" || strings.ContainsRune(rel, 0) || strings.ContainsRune(rel, '\\') {
		return "", reject(rel, ReasonInvalid)
	}
	if strings.HasPrefix(rel, "/") || hasDrive(rel) {
		return "", reject(rel, ReasonAbsolute)
	}
	clean := path.Clean(rel)
	if escapes(clean) {
		return "", reject(rel, ReasonEscape)
	}
	return clean, nil
}

func hasDrive(p string) bool {
	if len(p) < 2 || p[1] != ':' {
		return false
	}
	c := p[0] | 0x20
	return c >= 'a' && c <= 'z'
}

func escapes(clean string) bool { return clean == ".." || strings.HasPrefix(clean, "../") }

func split(clean string) []string {
	if clean == "." || clean == "" {
		return nil
	}
	return strings.Split(clean, "/")
}

func joinRel(els []string) string {
	if len(els) == 0 {
		return "."
	}
	return strings.Join(els, "/")
}

func osPath(clean string) string { return filepath.FromSlash(clean) }

// follow resolves every symlink along clean and returns the real slash path
// inside the folder, the way the OS walks it: a link's target is spliced in
// raw in front of the remaining elements, and a ".." in it drops one element
// of the path resolved so far (Escape above the folder) — never a textual
// clean before the links in the target are followed. Missing elements end
// the walk: the rest is appended as is (a file that does not exist yet
// cannot be a link; a ".." after a missing element is Invalid, the OS cannot
// walk it either).
func (r *Root) follow(rel, clean string) (string, error) {
	segs := split(clean)
	var done []string
	hops := 0
	for len(segs) > 0 {
		seg := segs[0]
		segs = segs[1:]
		switch seg {
		case "", ".":
			continue
		case "..":
			if len(done) == 0 {
				return "", reject(rel, ReasonEscape)
			}
			done = done[:len(done)-1]
			continue
		}
		cand := joinRel(append(done[:len(done):len(done)], seg))
		fi, err := r.root.Lstat(osPath(cand))
		if err != nil {
			done = append(done, seg)
			for _, s := range segs {
				switch s {
				case "", ".":
				case "..":
					return "", reject(rel, ReasonInvalid)
				default:
					done = append(done, s)
				}
			}
			return joinRel(done), nil
		}
		if fi.Mode()&fs.ModeSymlink == 0 {
			done = append(done, seg)
			continue
		}
		hops++
		if hops > maxLinkHops {
			return "", reject(rel, ReasonInvalid)
		}
		target, err := r.root.Readlink(osPath(cand))
		if err != nil {
			return "", reject(rel, ReasonInvalid)
		}
		if filepath.IsAbs(target) || strings.HasPrefix(filepath.ToSlash(target), "/") {
			in, ok := r.insideAbs(target)
			if !ok {
				return "", reject(rel, ReasonEscape)
			}
			segs = append(strings.Split(in, "/"), segs...)
			done = nil
			continue
		}
		// Relative target: resolved from the link's folder (done as is).
		segs = append(strings.Split(filepath.ToSlash(target), "/"), segs...)
	}
	return joinRel(done), nil
}

// insideAbs maps an absolute link target to a raw (uncleaned) slash path
// inside the folder: the target must start with the folder path as opened
// or with its real path. Its ".." elements are left for follow to walk.
func (r *Root) insideAbs(target string) (string, bool) {
	for _, base := range []string{r.dir, r.real} {
		base = strings.TrimSuffix(base, string(filepath.Separator))
		if target == base {
			return "", true
		}
		if rest, ok := strings.CutPrefix(target, base+string(filepath.Separator)); ok {
			return filepath.ToSlash(rest), true
		}
	}
	return "", false
}

// checkIgnored rejects rule files and paths the ignore rules hide (checked
// on both the cleaned path and the real target of links). An ignorer error
// fails closed.
func (r *Root) checkIgnored(rel, clean, real string) error {
	if isRuleFile(clean) || isRuleFile(real) {
		return reject(rel, ReasonIgnored)
	}
	ig := r.ignorer()
	if ig == nil || clean == "." {
		return nil
	}
	dir := false
	if fi, err := r.root.Stat(osPath(real)); err == nil {
		dir = fi.IsDir()
	}
	rels, dirs := []string{clean}, []bool{dir}
	if real != clean {
		rels, dirs = append(rels, real), append(dirs, dir)
	}
	res, err := ig.Ignored(rels, dirs)
	if err != nil {
		return reject(rel, ReasonIgnored)
	}
	for _, v := range res {
		if v {
			return reject(rel, ReasonIgnored)
		}
	}
	return nil
}
