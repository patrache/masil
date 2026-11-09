package fsroot

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path"
	"runtime"
	"strings"
)

// newFilePerm is the mode of files masil creates.
const newFilePerm fs.FileMode = 0o644

// WriteText saves text over rel atomically. prev is the FileInfo the text is
// based on: when the disk no longer matches prev.FP (changed, deleted, or
// created meanwhile for a zero prev) it returns ErrChangedOnDisk and leaves
// the disk alone; the caller merges and calls again with the fresh info.
// Line endings and BOM are restored from prev; the file keeps its mode (new
// files 0644). The bytes go to ".<name>.masil-<8hex>" in the same folder,
// are synced, and renamed over the file (a link is written through, not
// replaced). Read-only kinds are refused with ErrNotText.
func (r *Root) WriteText(rel, text string, prev FileInfo) (FileInfo, error) {
	clean, real, err := r.resolve(rel)
	if err != nil {
		return FileInfo{}, err
	}
	if prev.Kind != KindText {
		return FileInfo{}, ErrNotText
	}
	cur, mode, err := r.diskState(real)
	if err != nil {
		return FileInfo{}, err
	}
	if !cur.sameContent(prev.FP) {
		return FileInfo{}, ErrChangedOnDisk
	}
	perm := newFilePerm
	if !cur.IsZero() {
		perm = mode.Perm()
	}
	data := encodeText(text, prev.EOL, prev.BOM)
	if err := r.atomicWrite(real, data, perm); err != nil {
		return FileInfo{}, err
	}
	fi, err := r.root.Stat(osPath(real))
	if err != nil {
		return FileInfo{}, err
	}
	_, kind, eol, hasBOM := classify(data)
	if kind == KindReadOnlyMixedEOL {
		eol = prev.EOL
	}
	return FileInfo{
		Rel: clean, Size: int64(len(data)), Kind: kind, EOL: eol, BOM: hasBOM, Mode: fi.Mode(),
		FP: Fingerprint{Size: int64(len(data)), ModTime: fi.ModTime(), Sum: sha256.Sum256(data)},
	}, nil
}

// diskState fingerprints real (zero when absent) and returns its mode.
func (r *Root) diskState(real string) (Fingerprint, fs.FileMode, error) {
	fi, err := r.root.Stat(osPath(real))
	if errors.Is(err, fs.ErrNotExist) {
		return Fingerprint{}, 0, nil
	}
	if err != nil {
		return Fingerprint{}, 0, err
	}
	if fi.IsDir() {
		return Fingerprint{}, 0, errIsDir
	}
	fp, fi, err := r.fingerprintOf(real, fi)
	if errors.Is(err, errNotRegular) {
		return Fingerprint{}, 0, ErrNotText
	}
	if err != nil {
		return Fingerprint{}, 0, err
	}
	return fp, fi.Mode(), nil
}

func encodeText(text string, eol EOL, hasBOM bool) []byte {
	if eol == EOLCRLF {
		text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\n", "\r\n")
	}
	if hasBOM {
		return append(append([]byte{}, bom...), text...)
	}
	return []byte(text)
}

func (r *Root) atomicWrite(real string, data []byte, perm fs.FileMode) (err error) {
	var rnd [4]byte
	if _, err := rand.Read(rnd[:]); err != nil {
		return err
	}
	dir, name := path.Split(real)
	tmp := dir + "." + name + ".masil-" + hex.EncodeToString(rnd[:])
	f, err := r.root.OpenFile(osPath(tmp), os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = f.Close()
			_ = r.root.Remove(osPath(tmp))
		}
	}()
	if _, err = f.Write(data); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if runtime.GOOS != "windows" {
		if err = r.root.Chmod(osPath(tmp), perm); err != nil {
			return err
		}
	}
	return r.root.Rename(osPath(tmp), osPath(real))
}

// Create makes an empty file (fails if it exists), mode 0644.
func (r *Root) Create(rel string) error {
	clean, err := r.Check(rel, false)
	if err != nil {
		return err
	}
	f, err := r.root.OpenFile(osPath(clean), os.O_WRONLY|os.O_CREATE|os.O_EXCL, newFilePerm)
	if err != nil {
		return err
	}
	return f.Close()
}

// Rename moves from to to; both must pass Check. A link is renamed itself.
func (r *Root) Rename(from, to string) error {
	f, err := r.Check(from, false)
	if err != nil {
		return err
	}
	t, err := r.Check(to, false)
	if err != nil {
		return err
	}
	return r.root.Rename(osPath(f), osPath(t))
}

// Remove deletes one file or empty folder after Check.
func (r *Root) Remove(rel string) error {
	clean, err := r.Check(rel, false)
	if err != nil {
		return err
	}
	return r.root.Remove(osPath(clean))
}
