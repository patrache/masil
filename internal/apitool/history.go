package apitool

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/patrache/masil/internal/config"
)

// HistoryBodyMax is how much of a response body a history entry keeps (C3.7).
const HistoryBodyMax = 256 << 10

// History entry kinds.
const (
	HistoryRequest = "request"
	HistoryRun     = "run"
)

// HistoryOptions are the [tools.api] history settings (CA 3.3).
type HistoryOptions struct {
	Enabled bool
	Max     int           // entries kept (<= 0: 500)
	MaxAge  time.Duration // older entries are pruned (<= 0: 30 days)
}

// DefaultHistoryOptions: on, 500 entries, 30 days.
func DefaultHistoryOptions() HistoryOptions {
	return HistoryOptions{Enabled: true, Max: 500, MaxAge: 30 * 24 * time.Hour}
}

// RunSummary is the result of one runner run (history, the api.run card).
type RunSummary struct {
	Collection string        `json:"collection"`
	Requests   int           `json:"requests"`
	Passed     int           `json:"passed"`
	Failed     int           `json:"failed"`
	Total      time.Duration `json:"total"`
	Failures   []RunFailure  `json:"failures,omitempty"`
}

// RunFailure is one failed request of a run. Reason is a Reason* code.
type RunFailure struct {
	Name   string `json:"name"`
	Method string `json:"method"`
	Status int    `json:"status,omitempty"`
	Reason string `json:"reason"`
}

// HistoryEntry is one history file. Request is the template before
// expansion and URL the expanded URL, both redacted; Header and BodyHead are
// the response's, redacted (secret values and sensitive headers are Mask).
type HistoryEntry struct {
	ID       string      `json:"id"`
	At       time.Time   `json:"at"`
	Kind     string      `json:"kind"`
	Request  Request     `json:"request,omitzero"`
	URL      string      `json:"url,omitempty"`
	Status   int         `json:"status,omitempty"`
	Timing   Timing      `json:"timing,omitzero"`
	Size     int64       `json:"size,omitempty"`
	Header   http.Header `json:"header,omitempty"`
	BodyHead []byte      `json:"body_head,omitempty"`
	Run      *RunSummary `json:"run,omitempty"`
}

// History is the local request history api/history/<ULID>.json (0600). It
// never leaves this PC. Safe for concurrent use.
type History struct {
	dir  string
	o    HistoryOptions
	now  func() time.Time
	mu   sync.Mutex
	last [16]byte // last id issued (monotonic)
	init bool
}

// OpenHistory is the history in dir with options o; now nil = time.Now.
func OpenHistory(dir string, o HistoryOptions, now func() time.Time) *History {
	if o.Max <= 0 {
		o.Max = 500
	}
	if o.MaxAge <= 0 {
		o.MaxAge = 30 * 24 * time.Hour
	}
	if now == nil {
		now = time.Now
	}
	return &History{dir: dir, o: o, now: now}
}

// secretTexts are the values of the secret variables p used.
func secretTexts(p *Prepared) []string {
	if p == nil {
		return nil
	}
	var out []string
	for _, u := range p.Used {
		if u.Secret && u.Text != "" {
			out = append(out, u.Text)
		}
	}
	return out
}

// Add records one exchange (resp nil: nothing came back).
func (h *History) Add(r *Request, p *Prepared, resp *Response) error {
	if !h.o.Enabled {
		return nil
	}
	sv := secretTexts(p)
	e := HistoryEntry{Kind: HistoryRequest}
	if r != nil {
		e.Request = RedactRequest(r, sv)
	}
	if p != nil {
		e.URL = redactURL(newRedactor(sv).replace(p.URL), false)
	}
	if resp != nil {
		e.Status = resp.Status
		e.Timing = resp.Timing
		e.Size = resp.Size
		e.Header = RedactHeader(resp.Header, sv)
		e.BodyHead = redactHead(resp.Body, sv, HistoryBodyMax, !resp.Binary)
	}
	return h.write(e)
}

// AddRun records a runner run.
func (h *History) AddRun(s RunSummary) error {
	if !h.o.Enabled {
		return nil
	}
	s.Failures = slices.Clone(s.Failures)
	return h.write(HistoryEntry{Kind: HistoryRun, Run: &s})
}

func (h *History) write(e HistoryEntry) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	now := h.now()
	e.At = now
	e.ID = h.newID(now)
	b, err := canonicalJSON(e)
	if err != nil {
		return err
	}
	if err := mkdirs(h.dir); err != nil {
		return err
	}
	if err := config.WriteFileAtomic(filepath.Join(h.dir, e.ID+".json"), b, filePerm); err != nil {
		return err
	}
	return h.prune(now)
}

// prune drops entries older than MaxAge and the oldest beyond Max. Ids sort
// by time, so neither needs the files read.
func (h *History) prune(now time.Time) error {
	ids, err := h.ids()
	if err != nil {
		return err
	}
	cutoff := now.Add(-h.o.MaxAge)
	drop := 0
	for drop < len(ids) && ulidTime(ids[drop]).Before(cutoff) {
		drop++
	}
	if over := len(ids) - h.o.Max; over > drop {
		drop = over
	}
	var errs []error
	for _, id := range ids[:drop] {
		if err := os.Remove(filepath.Join(h.dir, id+".json")); err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// ids lists the entry ids, oldest first.
func (h *History) ids() ([]string, error) {
	ents, err := os.ReadDir(h.dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range ents {
		id, ok := strings.CutSuffix(e.Name(), ".json")
		if ok && !e.IsDir() && validULID(id) {
			out = append(out, id)
		}
	}
	slices.Sort(out)
	return out, nil
}

// List returns up to n entries (n <= 0: all), newest first. An unreadable
// file is skipped.
func (h *History) List(n int) ([]HistoryEntry, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	ids, err := h.ids()
	if err != nil {
		return nil, err
	}
	slices.Reverse(ids)
	var out []HistoryEntry
	for _, id := range ids {
		if n > 0 && len(out) >= n {
			break
		}
		var e HistoryEntry
		if err := readJSON(filepath.Join(h.dir, id+".json"), &e); err != nil {
			continue
		}
		out = append(out, e)
	}
	return out, nil
}

// Clear removes every entry.
func (h *History) Clear() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	ids, err := h.ids()
	if err != nil {
		return err
	}
	var errs []error
	for _, id := range ids {
		if err := os.Remove(filepath.Join(h.dir, id+".json")); err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// --- ids: ULIDs from the injected clock, monotonic within a History ---

const crockfordAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// newID is a ULID for t: 48-bit milliseconds + 80 random bits; within the
// same (or an earlier) millisecond as the last id it is the last id + 1, so
// file names always sort in creation order.
func (h *History) newID(t time.Time) string {
	if !h.init {
		h.init = true
		if ids, _ := h.ids(); len(ids) > 0 {
			if b, ok := decodeULID(ids[len(ids)-1]); ok {
				h.last = b
			}
		}
	}
	ms := uint64(max(t.UnixMilli(), 0)) & (1<<48 - 1)
	lastMs := uint64(binary.BigEndian.Uint16(h.last[0:2]))<<32 | uint64(binary.BigEndian.Uint32(h.last[2:6]))
	var b [16]byte
	if h.last != ([16]byte{}) && ms <= lastMs {
		b = h.last
		for i := 15; i >= 6; i-- { // increment the random part
			b[i]++
			if b[i] != 0 {
				break
			}
		}
	} else {
		binary.BigEndian.PutUint16(b[0:2], uint16(ms>>32))
		binary.BigEndian.PutUint32(b[2:6], uint32(ms))
		_, _ = rand.Read(b[6:])
	}
	h.last = b
	return encodeULID(b)
}

func encodeULID(b [16]byte) string {
	hi := binary.BigEndian.Uint64(b[0:8])
	lo := binary.BigEndian.Uint64(b[8:16])
	var out [26]byte
	for i := 25; i >= 0; i-- {
		out[i] = crockfordAlphabet[lo&31]
		lo = lo>>5 | hi<<59
		hi >>= 5
	}
	return string(out[:])
}

func decodeULID(s string) ([16]byte, bool) {
	var b [16]byte
	if !validULID(s) {
		return b, false
	}
	var hi, lo uint64
	for i := 0; i < 26; i++ {
		v := uint64(strings.IndexByte(crockfordAlphabet, s[i]))
		hi = hi<<5 | lo>>59
		lo = lo<<5 | v
	}
	binary.BigEndian.PutUint64(b[0:8], hi)
	binary.BigEndian.PutUint64(b[8:16], lo)
	return b, true
}

func validULID(s string) bool {
	if len(s) != 26 || s[0] > '7' {
		return false
	}
	for i := 0; i < len(s); i++ {
		if strings.IndexByte(crockfordAlphabet, s[i]) < 0 {
			return false
		}
	}
	return true
}

func ulidTime(s string) time.Time {
	b, ok := decodeULID(s)
	if !ok {
		return time.Time{}
	}
	ms := uint64(binary.BigEndian.Uint16(b[0:2]))<<32 | uint64(binary.BigEndian.Uint32(b[2:6]))
	return time.UnixMilli(int64(ms))
}

// --- redaction of templates, URLs and body heads ---

// authSchemes are the scheme words kept before a placeholder credential.
var authSchemes = map[string]bool{"bearer": true, "basic": true, "token": true, "digest": true}

// literalCredential reports whether v holds a credential typed in as text
// (not only {{placeholders}}, an auth scheme word or Mask).
func literalCredential(v string) bool {
	rest := strings.TrimSpace(placeholder.ReplaceAllString(v, ""))
	if rest == "" || rest == Mask {
		return false
	}
	if HasPlaceholder(v) && authSchemes[strings.ToLower(rest)] {
		return false
	}
	return true
}

// maskValue hides the value of a sensitive name unless it is a placeholder.
func maskValue(name, v string) string {
	if SensitiveHeader(name) && literalCredential(v) {
		return Mask
	}
	return v
}

// redactURL masks the values of sensitive query parameters (template: keep
// placeholders).
func redactURL(u string, template bool) string {
	q := strings.IndexByte(u, '?')
	if q < 0 {
		return u
	}
	end := len(u)
	if f := strings.IndexByte(u[q:], '#'); f >= 0 {
		end = q + f
	}
	parts := strings.Split(u[q+1:end], "&")
	for i, p := range parts {
		k, v, ok := strings.Cut(p, "=")
		if !ok || v == "" || v == Mask {
			continue
		}
		name := k
		if dk, err := url.QueryUnescape(k); err == nil {
			name = dk
		}
		if !SensitiveHeader(name) {
			continue
		}
		if template && !literalCredential(v) {
			continue
		}
		parts[i] = k + "=" + Mask
	}
	return u[:q+1] + strings.Join(parts, "&") + u[end:]
}

// RedactRequest is a copy of the template r safe to keep or export (C3.10
// layer 4): every secret value is Mask; a sensitive header, query or form
// row and the auth password/token/key keep only {{placeholders}} — a typed-in
// credential becomes Mask; sensitive URL query values likewise.
func RedactRequest(r *Request, secretValues []string) Request {
	out := cloneRequest(r)
	rd := newRedactor(secretValues)
	out.Name = rd.replace(out.Name)
	out.URL = redactURL(rd.replace(out.URL), true)
	for i := range out.Query {
		out.Query[i].Value = maskValue(out.Query[i].Key, rd.replace(out.Query[i].Value))
	}
	for i := range out.Headers {
		out.Headers[i].Value = maskValue(out.Headers[i].Key, rd.replace(out.Headers[i].Value))
	}
	out.Body.Text = rd.replace(out.Body.Text)
	for i := range out.Body.Form {
		f := &out.Body.Form[i]
		f.Value = maskValue(f.Key, rd.replace(f.Value))
	}
	out.Auth.User = rd.replace(out.Auth.User)
	for _, p := range []*string{&out.Auth.Pass, &out.Auth.Token, &out.Auth.Value} {
		*p = rd.replace(*p)
		if literalCredential(*p) {
			*p = Mask
		}
	}
	for i := range out.Asserts {
		out.Asserts[i].Value = rd.replace(out.Asserts[i].Value)
	}
	return out
}

// redactHead is the first n bytes of body after redaction (a secret on the
// cut is hidden whole). Text is cut on a rune boundary.
func redactHead(body []byte, secretValues []string, n int, text bool) []byte {
	if len(body) == 0 {
		return nil
	}
	slack := 0
	for _, v := range secretValues {
		slack = max(slack, 3*len(v))
	}
	head := body[:min(len(body), n+slack)]
	out := []byte(newRedactor(secretValues).replace(string(head)))
	if len(out) > n {
		cut := n
		if text {
			for i := 0; i < utf8.UTFMax && cut > 0 && !utf8.RuneStart(out[cut]); i++ {
				cut--
			}
		}
		out = out[:cut]
	}
	return out
}

// cloneRequest is a deep copy of r.
func cloneRequest(r *Request) Request {
	out := *r
	out.Query = slices.Clone(r.Query)
	out.Headers = slices.Clone(r.Headers)
	out.Body.Form = slices.Clone(r.Body.Form)
	out.Asserts = slices.Clone(r.Asserts)
	out.Extracts = slices.Clone(r.Extracts)
	return out
}
