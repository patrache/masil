package fsroot

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"io"
	"io/fs"
	"time"
	"unicode/utf8"
)

const (
	// MaxCollab is the largest file opened for shared editing.
	MaxCollab = 512 << 10
	// MaxView is the largest file opened at all (read-only above MaxCollab).
	MaxView = 2 << 20
	// sniffLen is how much of the head is searched for NUL bytes.
	sniffLen = 8 << 10
)

// Kind classifies a file for the editor.
type Kind int

const (
	KindText             Kind = iota // editable together
	KindReadOnlyLarge                // MaxCollab < size <= MaxView
	KindReadOnlyMixedEOL             // CRLF and lone LF mixed
	KindTooLarge                     // > MaxView, not read
	KindBinary                       // NUL in the head or not UTF-8
)

// EOL is the line ending a text file is saved back with.
type EOL int

const (
	EOLLF EOL = iota
	EOLCRLF
)

var bom = []byte("\xef\xbb\xbf")

// Fingerprint identifies file content on disk. The zero value means "no
// file".
type Fingerprint struct {
	Size    int64
	ModTime time.Time
	Sum     [32]byte
}

// IsZero reports the "no file" fingerprint.
func (f Fingerprint) IsZero() bool {
	return f.Size == 0 && f.ModTime.IsZero() && f.Sum == [32]byte{}
}

// sameContent compares what matters for a change: presence, size and hash.
func (f Fingerprint) sameContent(g Fingerprint) bool {
	if f.IsZero() || g.IsZero() {
		return f.IsZero() == g.IsZero()
	}
	return f.Size == g.Size && f.Sum == g.Sum
}

// FileInfo describes a text file as read, and is the base for WriteText.
type FileInfo struct {
	Rel  string
	Size int64
	Kind Kind
	EOL  EOL
	BOM  bool
	Mode fs.FileMode
	FP   Fingerprint
}

var (
	ErrNotText       = errors.New("fsroot: not a text file")
	ErrChangedOnDisk = errors.New("fsroot: changed on disk")
	errIsDir         = errors.New("fsroot: is a folder")
	errNotRegular    = errors.New("fsroot: not a regular file")
)

// readRegular reads real, refusing anything that is not a regular file
// (a FIFO open would block, a device never ends) and reading at most
// MaxView+1 bytes so a file growing during the read cannot get past the
// limit. fi is the Stat of the opened file.
func (r *Root) readRegular(real string) ([]byte, fs.FileInfo, error) {
	pre, err := r.root.Stat(osPath(real))
	if err != nil {
		return nil, nil, err
	}
	if !pre.Mode().IsRegular() {
		return nil, pre, errNotRegular
	}
	f, err := r.root.Open(osPath(real))
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, fi, errNotRegular
	}
	data, err := io.ReadAll(io.LimitReader(f, MaxView+1))
	if err != nil {
		return nil, fi, err
	}
	return data, fi, nil
}

// fingerprintOf fingerprints a regular file. Above MaxView (by Stat or by
// what the bounded read got) the content is not hashed: Sum is a marker
// derived from size and mtime, so any size or mtime change is a content
// change and memory use stays bounded by MaxView.
func (r *Root) fingerprintOf(real string, fi fs.FileInfo) (Fingerprint, fs.FileInfo, error) {
	if !fi.Mode().IsRegular() {
		return Fingerprint{}, fi, errNotRegular
	}
	if fi.Size() > MaxView {
		return largeFingerprint(fi), fi, nil
	}
	data, fi, err := r.readRegular(real)
	if err != nil {
		return Fingerprint{}, fi, err
	}
	if len(data) > MaxView {
		return largeFingerprint(fi), fi, nil
	}
	return Fingerprint{Size: int64(len(data)), ModTime: fi.ModTime(), Sum: sha256.Sum256(data)}, fi, nil
}

func largeFingerprint(fi fs.FileInfo) Fingerprint {
	var b [len("masil-large") + 16]byte
	n := copy(b[:], "masil-large")
	binary.BigEndian.PutUint64(b[n:], uint64(fi.Size()))
	binary.BigEndian.PutUint64(b[n+8:], uint64(fi.ModTime().UnixNano()))
	return Fingerprint{Size: fi.Size(), ModTime: fi.ModTime(), Sum: sha256.Sum256(b[:])}
}

// ReadText reads and classifies a file. Above MaxView it is not read
// (KindTooLarge + ErrNotText); a NUL in the first 8 KiB or invalid UTF-8 is
// KindBinary + ErrNotText. A BOM is removed, CRLF becomes LF unless lone LFs
// are mixed in (KindReadOnlyMixedEOL, text returned as is); above MaxCollab
// the file is KindReadOnlyLarge.
func (r *Root) ReadText(rel string) (string, FileInfo, error) {
	clean, real, err := r.resolve(rel)
	if err != nil {
		return "", FileInfo{}, err
	}
	fi, err := r.root.Stat(osPath(real))
	if err != nil {
		return "", FileInfo{}, err
	}
	info := FileInfo{Rel: clean, Size: fi.Size(), Mode: fi.Mode()}
	if fi.IsDir() {
		return "", info, errIsDir
	}
	if !fi.Mode().IsRegular() {
		info.Kind = KindBinary
		return "", info, ErrNotText
	}
	if fi.Size() > MaxView {
		info.Kind = KindTooLarge
		return "", info, ErrNotText
	}
	data, fi, err := r.readRegular(real)
	if errors.Is(err, errNotRegular) {
		info.Kind = KindBinary
		return "", info, ErrNotText
	}
	if err != nil {
		return "", info, err
	}
	info.Size = int64(len(data))
	if len(data) > MaxView {
		info.Kind = KindTooLarge
		return "", info, ErrNotText
	}
	info.FP = Fingerprint{Size: int64(len(data)), ModTime: fi.ModTime(), Sum: sha256.Sum256(data)}
	text, kind, eol, hasBOM := classify(data)
	info.Kind, info.EOL, info.BOM = kind, eol, hasBOM
	if kind == KindBinary {
		return "", info, ErrNotText
	}
	return text, info, nil
}

func classify(data []byte) (text string, kind Kind, eol EOL, hasBOM bool) {
	head := data[:min(len(data), sniffLen)]
	if bytes.IndexByte(head, 0) >= 0 || !utf8.Valid(data) {
		return "", KindBinary, EOLLF, false
	}
	body := data
	if bytes.HasPrefix(body, bom) {
		hasBOM = true
		body = body[len(bom):]
	}
	crlf := bytes.Count(body, []byte("\r\n"))
	lf := bytes.Count(body, []byte("\n")) - crlf
	switch {
	case crlf > 0 && lf > 0:
		return string(body), KindReadOnlyMixedEOL, EOLCRLF, hasBOM
	case crlf > 0:
		eol = EOLCRLF
		text = string(bytes.ReplaceAll(body, []byte("\r\n"), []byte("\n")))
	default:
		text = string(body)
	}
	if len(data) > MaxCollab {
		return text, KindReadOnlyLarge, eol, hasBOM
	}
	return text, KindText, eol, hasBOM
}

// Stat returns size and modification time of rel (links followed inside the
// folder).
func (r *Root) Stat(rel string) (size int64, mod time.Time, err error) {
	_, real, err := r.resolve(rel)
	if err != nil {
		return 0, time.Time{}, err
	}
	fi, err := r.root.Stat(osPath(real))
	if err != nil {
		return 0, time.Time{}, err
	}
	return fi.Size(), fi.ModTime(), nil
}
