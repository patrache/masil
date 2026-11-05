// Package auth reads and writes credentials.json and device_id (CA 3.4).
package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/patrache/masil/internal/config"
)

// Credentials mirrors credentials.json.
type Credentials struct {
	Version   int       `json:"version"` // 1
	Server    string    `json:"server"`
	Token     string    `json:"token"`
	SessionID string    `json:"session_id"`
	UserID    string    `json:"user_id"`
	Handle    string    `json:"handle"`
	SavedAt   time.Time `json:"saved_at"`
}

// ErrNoCredentials means the user is not logged in.
var ErrNoCredentials = errors.New("auth: not logged in")

// redacted is the only form a token may take in logs.
const redacted = "msl_…(redacted)"

// Load returns ErrNoCredentials if the file is missing or the token is empty.
// A corrupt file also yields ErrNoCredentials (wrapped with the parse error) and
// is left on disk untouched; a file with permissions wider than 0600 is narrowed.
func Load(p config.Paths) (*Credentials, error) {
	b, err := os.ReadFile(p.Credentials)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrNoCredentials
		}
		return nil, fmt.Errorf("auth: read credentials: %w", err)
	}
	if err := narrowPerm(p.Credentials); err != nil {
		return nil, err
	}
	var c Credentials
	if err := json.Unmarshal(b, &c); err != nil {
		// Never include file contents in the error: they may hold a token.
		return nil, fmt.Errorf("%w: %v", ErrNoCredentials, jsonErrKind(err))
	}
	if c.Token == "" {
		return nil, ErrNoCredentials
	}
	return &c, nil
}

// Save writes credentials.json atomically with 0600.
func Save(p config.Paths, c *Credentials) error {
	if c == nil {
		return errors.New("auth: save nil credentials")
	}
	if err := config.EnsureDir(p); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("auth: encode credentials: %w", err)
	}
	b = append(b, '\n')
	if err := config.WriteFileAtomic(p.Credentials, b, 0o600); err != nil {
		return fmt.Errorf("auth: save credentials: %w", err)
	}
	return nil
}

// Clear removes the file; nil if absent.
func Clear(p config.Paths) error {
	if err := os.Remove(p.Credentials); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("auth: clear credentials: %w", err)
	}
	return nil
}

// Matches reports c.Server == server (trailing slash ignored).
func (c *Credentials) Matches(server string) bool {
	if c == nil {
		return false
	}
	a, b := strings.TrimRight(c.Server, "/"), strings.TrimRight(server, "/")
	return a != "" && a == b
}

// Redacted returns "msl_…(redacted)" for logs.
func (c *Credentials) Redacted() string {
	return redacted
}

// narrowPerm sets path to 0600 when its permission bits are wider (not on Windows).
func narrowPerm(path string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	st, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("auth: stat %s: %w", path, err)
	}
	if st.Mode().Perm()&^0o600 != 0 {
		if err := os.Chmod(path, 0o600); err != nil {
			return fmt.Errorf("auth: chmod %s: %w", path, err)
		}
	}
	return nil
}

// jsonErrKind describes a JSON decode error without echoing input bytes.
func jsonErrKind(err error) string {
	var se *json.SyntaxError
	var te *json.UnmarshalTypeError
	switch {
	case errors.As(err, &se):
		return fmt.Sprintf("invalid json at offset %d", se.Offset)
	case errors.As(err, &te):
		return fmt.Sprintf("invalid json type for field %q", te.Field)
	default:
		return "invalid json"
	}
}
