// Package fsroot is the owner-side gate to the shared folder: every file
// access goes through an os.Root, peer paths are cleaned and checked (links
// followed to their real target) against the secret deny list and the
// ignore rules, and text files are classified, saved atomically and watched
// for outside changes.
package fsroot

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// windowsNames enables the Windows-only name checks in cleanRel (a var so
// the tests can run them on any OS).
var windowsNames = runtime.GOOS == "windows"

// windowsLinks makes follow resolve link targets the way Windows does: the
// target is joined to the link's folder (and to the elements after the link)
// and the whole is cleaned lexically, so "s\..\config" is "config" whatever
// s points to. CreateSymbolicLinkW already stores an absolute target cleaned,
// and os.Root on Windows follows links the same way (rootCleanPath), so the
// deny list must see the path cleaned this way too, or a Create/Rename/
// Remove through os.Root could touch a file Check never looked at. A var so
// the tests can run the Windows model on any OS.
var windowsLinks = runtime.GOOS == "windows"

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
	cleans, errs := r.CheckBatch([]string{rel}, forPeer)
	return cleans[0], errs[0]
}

// CheckBatch runs Check on every path of rels and asks the ignore rules once
// for all of them (one git check-ignore, not one per path): cleans[i] and
// errs[i] are what Check(rels[i], forPeer) returns. An ignore-rule failure
// hides every path it was asked about (fail closed).
func (r *Root) CheckBatch(rels []string, forPeer bool) (cleans []string, errs []error) {
	cleans, errs = make([]string, len(rels)), make([]error, len(rels))
	type pending struct {
		i           int
		clean, real string
	}
	var ask []pending
	for i, rel := range rels {
		clean, real, err := r.checkPath(rel)
		if err != nil {
			errs[i] = err
			continue
		}
		if forPeer {
			if isRuleFile(clean) || isRuleFile(real) {
				errs[i] = reject(rel, ReasonIgnored)
				continue
			}
			if clean != "." {
				ask = append(ask, pending{i, clean, real})
				continue
			}
		}
		cleans[i] = clean
	}
	if len(ask) == 0 {
		return cleans, errs
	}
	ig := r.ignorer()
	if ig == nil {
		for _, p := range ask {
			cleans[p.i] = p.clean
		}
		return cleans, errs
	}
	var paths []string
	var dirs []bool
	for _, p := range ask {
		dir := false
		if fi, err := r.root.Stat(osPath(p.real)); err == nil {
			dir = fi.IsDir()
		}
		paths, dirs = append(paths, p.clean), append(dirs, dir)
		if p.real != p.clean {
			paths, dirs = append(paths, p.real), append(dirs, dir)
		}
	}
	res, err := ig.Ignored(paths, dirs)
	if err == nil && len(res) != len(paths) {
		err = errIgnorerAnswer
	}
	k := 0
	for _, p := range ask {
		hidden := err != nil || res[k]
		k++
		if p.real != p.clean {
			hidden = hidden || (err == nil && res[k])
			k++
		}
		if hidden {
			errs[p.i] = reject(rels[p.i], ReasonIgnored)
		} else {
			cleans[p.i] = p.clean
		}
	}
	return cleans, errs
}

// errIgnorerAnswer: an Ignorer gave a different number of answers than it
// was asked (treated like any ignore-rule failure: hidden).
var errIgnorerAnswer = errors.New("fsroot: ignorer answered the wrong number of paths")

// checkPath is Check without the ignore rules: syntax, escape, deny list on
// the cleaned path and on the real path after links.
func (r *Root) checkPath(rel string) (clean, real string, err error) {
	clean, err = cleanRel(rel)
	if err != nil {
		return "", "", err
	}
	if Denied(clean) {
		return "", "", reject(rel, ReasonDenied)
	}
	real, err = r.follow(rel, clean)
	if err != nil {
		return "", "", err
	}
	if Denied(real) {
		return "", "", reject(rel, ReasonDenied)
	}
	return clean, real, nil
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
	if windowsNames && windowsAlias(clean) {
		return "", reject(rel, ReasonInvalid)
	}
	return clean, nil
}

// windowsAlias reports an element Windows does not store literally: a ':'
// (alternate data stream, ".env::$DATA" is ".env") or a trailing dot or
// space (".env." and ".env " are ".env"). Such spellings reach or create a
// file under a name the deny list and the ignore rules never see (v0.1.3
// security (g)). Win32 cannot create these names, so refusing them costs the
// owner nothing. 8.3 short names ("SECRET~1.JSO") are not refused here: an
// existing one is mapped to its stored long name by diskName, and one that
// does not exist is an ordinary long name ("Copy~1.txt"), which the owner
// must keep being able to open.
func windowsAlias(clean string) bool {
	for _, el := range split(clean) {
		if el == "." || el == ".." {
			continue
		}
		if strings.ContainsRune(el, ':') || strings.HasSuffix(el, ".") || strings.HasSuffix(el, " ") {
			return true
		}
	}
	return false
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
// walk it either). With windowsLinks the target is cleaned lexically
// against the link's folder instead, the way Windows opens it.
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
		name, ok := r.diskName(done, seg, fi)
		if !ok {
			return "", reject(rel, ReasonInvalid)
		}
		seg = name
		cand = joinRel(append(done[:len(done):len(done)], seg))
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
			if windowsLinks {
				segs, ok = lexicalJoin(nil, in, segs)
				if !ok {
					return "", reject(rel, ReasonEscape)
				}
				done = nil
				continue
			}
			segs = append(strings.Split(in, "/"), segs...)
			done = nil
			continue
		}
		if windowsLinks {
			var ok bool
			segs, ok = lexicalJoin(done, filepath.ToSlash(target), segs)
			if !ok {
				return "", reject(rel, ReasonEscape)
			}
			done = nil
			continue
		}
		// Relative target: resolved from the link's folder (done as is).
		segs = append(strings.Split(filepath.ToSlash(target), "/"), segs...)
	}
	return joinRel(done), nil
}

// lexicalJoin is the Windows link step: folder elements, the raw target and
// the elements after the link, cleaned as one path. false = above the root.
// The result is walked again from the root (its elements can be links too).
func lexicalJoin(folder []string, target string, rest []string) ([]string, bool) {
	parts := make([]string, 0, len(folder)+len(rest)+1)
	parts = append(parts, folder...)
	if target != "" {
		parts = append(parts, target)
	}
	parts = append(parts, rest...)
	c := path.Clean(strings.Join(parts, "/"))
	if strings.HasPrefix(c, "/") || escapes(c) {
		return nil, false
	}
	return split(c), true
}

// diskName returns the name stored on disk for the existing element seg of
// the folder done (fi is seg's Lstat). Case-insensitive or folding file
// systems (APFS, NTFS: "SECRETS.JSON", U+017F "ſ" for "s", 8.3 short names,
// trailing dots or spaces) open a file under spellings the deny list and
// the ignore rules never see, so every check runs on the stored name: an
// exact entry match keeps seg, otherwise the one entry that is the same file
// replaces it. No match, more than one (hard links) or an unreadable folder
// is false (fail closed).
func (r *Root) diskName(done []string, seg string, fi fs.FileInfo) (string, bool) {
	dir := joinRel(done)
	f, err := r.root.Open(osPath(dir))
	if err != nil {
		return "", false
	}
	names, err := f.Readdirnames(-1)
	_ = f.Close()
	if err != nil {
		return "", false
	}
	for _, n := range names {
		if n == seg {
			return seg, true
		}
	}
	match, count := "", 0
	for _, n := range names {
		efi, err := r.root.Lstat(osPath(joinRel(append(done[:len(done):len(done)], n))))
		if err == nil && os.SameFile(fi, efi) {
			match, count = n, count+1
		}
	}
	return match, count == 1
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
