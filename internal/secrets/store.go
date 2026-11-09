package secrets

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/patrache/masil/internal/config"
)

// Backend is where a value is kept (shown on the settings "도구 로그인" rows).
type Backend string

// Backends.
const (
	BackendKeychain      Backend = "keychain"       // macOS login keychain
	BackendCredMan       Backend = "credman"        // Windows Credential Manager
	BackendSecretService Backend = "secret-service" // Linux Secret Service (GNOME Keyring, KWallet)
	BackendFile          Backend = "file"           // <SecretsDir>/tools.json, 0600 (after consent)
	BackendMemory        Backend = "memory"         // this run only (after consent; also test OS fakes)
)

// Fallback is what the user agreed to when the OS store cannot be used (A9.2).
type Fallback int

// Fallbacks.
const (
	FallbackUnset   Fallback = iota // not asked yet: Put returns ErrNeedConsent
	FallbackFile                    // ① the 0600 file
	FallbackSession                 // ② this run's memory (the default choice)
)

// Errors.
var (
	// ErrNotFound: no value under that key.
	ErrNotFound = errors.New("secrets: not found")
	// ErrNeedConsent: the OS store cannot be used and no fallback was chosen;
	// ask (secretsui.OverlayConsent), SetFallback, then Put again.
	ErrNeedConsent = errors.New("secrets: the OS store is unavailable and no fallback was chosen")
	// ErrUnavailable: an OSBackend cannot be used (no store, no D-Bus, locked
	// and the unlock was dismissed, the user denied access …).
	ErrUnavailable = errors.New("secrets: the OS store is unavailable")
)

// OSBackend is one OS store. Get returns ErrNotFound for a missing value and
// every method returns ErrUnavailable (wrapped) when the store cannot be used.
// Errors never carry the value.
type OSBackend interface {
	Name() Backend
	Set(ctx context.Context, service, account, value string) error
	Get(ctx context.Context, service, account string) (string, error)
	Delete(ctx context.Context, service, account string) error
}

// Options configure Open.
type Options struct {
	OS    OSBackend    // nil => this OS's store (none on other platforms)
	Paths config.Paths // SecretsDir holds the file fallback and the name index
}

// Store is the tool-secret store; tools reach it only through ctx.Secrets. It
// is safe for concurrent use; OS calls (which may show a keychain prompt) run
// outside its lock.
type Store struct {
	os  OSBackend // never nil (noOS when there is none)
	dir string    // SecretsDir ("" => no file fallback, in-memory index)

	mu       sync.Mutex
	fallback Fallback
	osDown   bool              // the OS store said unavailable this run: Put/Get skip it (no repeated prompts)
	session  map[string]string // account => value (FallbackSession)
	index    map[string]Backend
	loaded   bool // index read from disk
}

// Open builds the store. It does no I/O (the index is read on first use).
func Open(o Options) *Store {
	b := o.OS
	if b == nil {
		b = defaultOS()
	}
	if b == nil {
		b = noOS{}
	}
	return &Store{os: b, dir: o.Paths.SecretsDir, session: map[string]string{}}
}

// SetFallback records the user's choice for when the OS store cannot be used.
func (s *Store) SetFallback(f Fallback) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fallback = f
}

// Fallback is the current choice.
func (s *Store) Fallback() Fallback {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fallback
}

// Put stores v under k: in the OS store, else in the chosen fallback. Without
// an OS store and without a fallback it returns ErrNeedConsent. The value is
// removed from every other local place so Get sees only the new one.
func (s *Store) Put(ctx context.Context, k Key, v Secret) (Backend, error) {
	if err := k.validate(); err != nil {
		return "", err
	}
	acct := k.Account()
	if !s.down() {
		err := ctxErr(ctx, s.os.Set(ctx, Service, acct, v.Reveal()))
		if err == nil {
			s.mu.Lock()
			defer s.mu.Unlock()
			delete(s.session, acct)
			s.dropFileLocked(acct)
			s.setIndexLocked(acct, s.os.Name())
			return s.os.Name(), nil
		}
		if !errors.Is(err, ErrUnavailable) {
			return "", fmt.Errorf("secrets: put %s: %w", acct, err)
		}
		s.markDown(err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	switch s.fallback {
	case FallbackSession:
		s.session[acct] = v.Reveal()
		if s.indexLocked()[acct] == BackendFile {
			s.dropFileLocked(acct)
			s.deleteIndexLocked(acct)
		}
		return BackendMemory, nil
	case FallbackFile:
		if err := s.putFileLocked(acct, v.Reveal()); err != nil {
			return "", err
		}
		delete(s.session, acct)
		s.setIndexLocked(acct, BackendFile)
		return BackendFile, nil
	default:
		return "", ErrNeedConsent
	}
}

// Get reads k: this run's memory, then the OS store, then the file. A value
// the index puts in an OS store that cannot be read now is ErrUnavailable.
func (s *Store) Get(ctx context.Context, k Key) (Secret, Backend, error) {
	if err := k.validate(); err != nil {
		return Secret{}, "", err
	}
	acct := k.Account()
	s.mu.Lock()
	if v, ok := s.session[acct]; ok {
		s.mu.Unlock()
		return New(v), BackendMemory, nil
	}
	where := s.indexLocked()[acct]
	down := s.osDown
	s.mu.Unlock()

	var osErr error
	if where != BackendFile {
		if down {
			osErr = ErrUnavailable
		} else {
			v, err := s.os.Get(ctx, Service, acct)
			err = ctxErr(ctx, err)
			switch {
			case err == nil:
				return New(v), s.os.Name(), nil
			case errors.Is(err, ErrNotFound):
			case errors.Is(err, ErrUnavailable):
				s.markDown(err)
				osErr = err
			default:
				return Secret{}, "", fmt.Errorf("secrets: get %s: %w", acct, err)
			}
		}
	}
	s.mu.Lock()
	v, ok := s.getFileLocked(acct)
	s.mu.Unlock()
	if ok {
		return New(v), BackendFile, nil
	}
	if osErr != nil && where != "" && where == s.os.Name() {
		return Secret{}, "", osErr
	}
	return Secret{}, "", ErrNotFound
}

// Delete removes k everywhere (OS store, file, memory). A missing value is not
// an error. The OS store is always tried, even when it said unavailable before.
func (s *Store) Delete(ctx context.Context, k Key) error {
	if err := k.validate(); err != nil {
		return err
	}
	return s.deleteAccount(ctx, k.Account())
}

func (s *Store) deleteAccount(ctx context.Context, acct string) error {
	var keep error
	if err := ctxErr(ctx, s.os.Delete(ctx, Service, acct)); err != nil && !errors.Is(err, ErrNotFound) {
		s.mu.Lock()
		where := s.indexLocked()[acct]
		inOS := where != "" && where == s.os.Name()
		s.mu.Unlock()
		if !errors.Is(err, ErrUnavailable) || inOS {
			keep = fmt.Errorf("secrets: delete %s: %w", acct, err)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.session, acct)
	s.dropFileLocked(acct)
	if keep == nil {
		s.deleteIndexLocked(acct)
	}
	return keep
}

// List returns the value names (never values) the tool keeps for that masil
// account, sorted.
func (s *Store) List(_ context.Context, tool, server, userID string) ([]string, error) {
	if err := (Key{tool, server, userID, "x"}).validate(); err != nil {
		return nil, err
	}
	prefix := tool + "/" + server + "/" + userID + "/"
	var out []string
	for _, acct := range s.accounts() {
		if name, ok := strings.CutPrefix(acct, prefix); ok && name != "" {
			out = append(out, name)
		}
	}
	return out, nil
}

// DeleteAll removes every tool's values for that masil account (logout's
// "이 기기의 도구 로그인도 지우기").
func (s *Store) DeleteAll(ctx context.Context, server, userID string) error {
	var errs []error
	for _, acct := range s.accounts() {
		k, ok := parseAccount(acct)
		if !ok || k.Server != server || k.UserID != userID {
			continue
		}
		if err := s.deleteAccount(ctx, acct); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Where is where k is kept ("" = unknown); it never calls the OS store.
func (s *Store) Where(k Key) Backend {
	acct := k.Account()
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.session[acct]; ok {
		return BackendMemory
	}
	return s.indexLocked()[acct]
}

// accounts is every account the store knows (index, memory, file), sorted.
func (s *Store) accounts() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	set := map[string]bool{}
	for a := range s.indexLocked() {
		set[a] = true
	}
	for a := range s.session {
		set[a] = true
	}
	for a := range s.readValuesLocked() {
		set[a] = true
	}
	return slices.Sorted(maps.Keys(set))
}

// ctxErr is err, except that a failure while ctx is done is ctx's error and
// never ErrUnavailable: a cancel or a timeout says nothing about the OS store,
// so it must not mark the store down or ask for consent (Review Focus 4).
func ctxErr(ctx context.Context, err error) error {
	if err == nil || errors.Is(err, ErrNotFound) {
		return err
	}
	if cerr := ctx.Err(); cerr != nil {
		return fmt.Errorf("secrets: %w", cerr)
	}
	return err
}

func (s *Store) down() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.osDown
}

func (s *Store) markDown(err error) {
	s.mu.Lock()
	first := !s.osDown
	s.osDown = true
	s.mu.Unlock()
	if first {
		slog.Info("secrets: OS store unavailable", "backend", s.os.Name(), "err", err)
	}
}

// ValuesForTest returns the values held in this run's memory and in the file
// (account => value) — secrettest.Values; it panics outside tests.
func ValuesForTest(s *Store) map[string]string {
	if !testing.Testing() {
		panic("secrets: ValuesForTest called outside a test")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := maps.Clone(s.readValuesLocked())
	if out == nil {
		out = map[string]string{}
	}
	maps.Copy(out, s.session)
	return out
}
