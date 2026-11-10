package archdoc

import (
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"

	"github.com/patrache/masil/internal/config"
	"github.com/patrache/masil/internal/textwidth"
)

// Store errors.
var (
	ErrBadName = errors.New("archdoc: bad design name")
	ErrExists  = errors.New("archdoc: a design with that name exists")
)

// MaxName is the longest design name in graphemes. Names are also kept to
// maxNameBytes so the temp file of a save fits every file system.
const MaxName = 64

const (
	fileExt = ".json"
	// maxNameBytes leaves room in a 255-byte file name (ext4, xfs, btrfs) for
	// config.WriteFileAtomic's temp file "." + name + ".json" + ".tmp-<pid>-<rand>"
	// with a 10-digit pid and a 10-digit random part (P3 review).
	maxNameBytes = 255 - len(".") - len(fileExt) - len(".tmp-") - 10 - len("-") - 10
)

// Store is the personal design folder (<config dir>/arch). Designs never leave
// the machine. Every read, rename and delete goes through os.Root so nothing
// outside the folder is touched; writes are atomic files of mode 0600.
type Store struct{ dir string }

// Entry is one listed design. A file that does not decode shows Title "" and
// Nodes 0 and does not stop the listing.
type Entry struct {
	Name     string
	Title    string
	Modified time.Time
	Nodes    int
}

// OpenStore opens (creating with mode 0700) the design folder dir.
func OpenStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("archdoc: open store: %w", err)
	}
	r, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("archdoc: open store: %w", err)
	}
	_ = r.Close()
	return &Store{dir: dir}, nil
}

// SafeName returns the NFC form of a design name, or ErrBadName for names that
// are empty, "." or "..", start with ".", start or end with a space, hold
// / \ : * ? " < > |, a control or invisible character, more than MaxName
// graphemes or more than 223 bytes.
func SafeName(s string) (string, error) {
	if !utf8.ValidString(s) {
		return "", ErrBadName
	}
	s = norm.NFC.String(s)
	if s == "" || strings.HasPrefix(s, ".") || len(s) > maxNameBytes || graphemes(s) > MaxName {
		return "", ErrBadName
	}
	first, _ := utf8.DecodeRuneInString(s)
	last, _ := utf8.DecodeLastRuneInString(s)
	if unicode.IsSpace(first) || unicode.IsSpace(last) {
		return "", ErrBadName
	}
	for _, r := range s {
		if strings.ContainsRune(`/\:*?"<>|`, r) || unicode.IsControl(r) || textwidth.Invisible(r) {
			return "", ErrBadName
		}
	}
	return s, nil
}

func (s *Store) root() (*os.Root, error) {
	r, err := os.OpenRoot(s.dir)
	if err != nil {
		return nil, fmt.Errorf("archdoc: store: %w", err)
	}
	return r, nil
}

func fileOf(name string) (string, error) {
	n, err := SafeName(name)
	if err != nil {
		return "", err
	}
	return n + fileExt, nil
}

// List returns the designs, most recently modified first (then by name).
func (s *Store) List() ([]Entry, error) {
	r, err := s.root()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	f, err := r.Open(".")
	if err != nil {
		return nil, fmt.Errorf("archdoc: list: %w", err)
	}
	ents, err := f.ReadDir(-1)
	_ = f.Close()
	if err != nil {
		return nil, fmt.Errorf("archdoc: list: %w", err)
	}
	var out []Entry
	for _, de := range ents {
		name, ok := strings.CutSuffix(de.Name(), fileExt)
		if !ok || !de.Type().IsRegular() {
			continue
		}
		if safe, err := SafeName(name); err != nil || safe != name {
			continue // temp files, foreign names
		}
		info, err := de.Info()
		if err != nil {
			continue
		}
		e := Entry{Name: name, Modified: info.ModTime()}
		if info.Size() <= MaxBytes {
			if data, err := r.ReadFile(de.Name()); err == nil {
				if d, err := Decode(data); err == nil {
					e.Title, e.Nodes = d.Title, len(d.Nodes)
				}
			}
		}
		out = append(out, e)
	}
	slices.SortFunc(out, func(a, b Entry) int {
		return cmp.Or(b.Modified.Compare(a.Modified), cmp.Compare(a.Name, b.Name))
	})
	return out, nil
}

// Load reads and validates one design. A missing design wraps fs.ErrNotExist;
// a broken one is an *InvalidError.
func (s *Store) Load(name string) (*Design, error) {
	file, err := fileOf(name)
	if err != nil {
		return nil, err
	}
	r, err := s.root()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	info, err := r.Stat(file)
	if err != nil {
		return nil, fmt.Errorf("archdoc: load %q: %w", name, err)
	}
	if info.Size() > MaxBytes {
		return nil, invalid(Issue{Code: IssueTooLarge})
	}
	data, err := r.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("archdoc: load %q: %w", name, err)
	}
	return Decode(data)
}

// Save writes d as name (mode 0600). With overwrite false an existing design of
// that name is left alone and ErrExists returned. An invalid design is refused.
func (s *Store) Save(name string, d *Design, overwrite bool) error {
	file, err := fileOf(name)
	if err != nil {
		return err
	}
	if is := d.Validate(); len(is) > 0 {
		return invalid(is...)
	}
	data, err := d.Encode()
	if err != nil {
		return err
	}
	if len(data) > MaxBytes {
		return invalid(Issue{Code: IssueTooLarge})
	}
	if !overwrite {
		if exists, err := s.exists(file); err != nil {
			return err
		} else if exists {
			return ErrExists
		}
	}
	// file has no separators (SafeName), so the path stays inside the folder.
	if err := config.WriteFileAtomic(filepath.Join(s.dir, file), data, 0o600); err != nil {
		return fmt.Errorf("archdoc: save %q: %w", name, err)
	}
	return nil
}

func (s *Store) exists(file string) (bool, error) {
	r, err := s.root()
	if err != nil {
		return false, err
	}
	defer r.Close()
	_, err = r.Lstat(file)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, fs.ErrNotExist):
		return false, nil
	}
	return false, fmt.Errorf("archdoc: store: %w", err)
}

// Rename renames a design; ErrExists if to is another design (a case-only
// rename of the same file is allowed on case-insensitive file systems).
func (s *Store) Rename(from, to string) error {
	src, err := fileOf(from)
	if err != nil {
		return err
	}
	dst, err := fileOf(to)
	if err != nil {
		return err
	}
	if src == dst {
		return nil
	}
	r, err := s.root()
	if err != nil {
		return err
	}
	defer r.Close()
	if taken, err := s.exists(dst); err != nil {
		return err
	} else if taken && !sameFile(r, src, dst) {
		return ErrExists // a case-only rename on APFS/NTFS finds src itself
	}
	if err := r.Rename(src, dst); err != nil {
		return fmt.Errorf("archdoc: rename %q: %w", from, err)
	}
	return nil
}

func sameFile(r *os.Root, a, b string) bool {
	fa, err := r.Lstat(a)
	if err != nil {
		return false
	}
	fb, err := r.Lstat(b)
	return err == nil && os.SameFile(fa, fb)
}

// Delete removes a design.
func (s *Store) Delete(name string) error {
	file, err := fileOf(name)
	if err != nil {
		return err
	}
	r, err := s.root()
	if err != nil {
		return err
	}
	defer r.Close()
	if err := r.Remove(file); err != nil {
		return fmt.Errorf("archdoc: delete %q: %w", name, err)
	}
	return nil
}
