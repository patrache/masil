// Package auth reads and writes credentials.json and device_id (CA 3.4).
package auth

import (
	"errors"
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

// Load returns ErrNoCredentials if the file is missing or the token is empty.
func Load(p config.Paths) (*Credentials, error) {
	// TODO(core): Task 5
	return nil, nil
}

// Save writes credentials.json atomically with 0600.
func Save(p config.Paths, c *Credentials) error {
	// TODO(core): Task 5
	return nil
}

// Clear removes the file; nil if absent.
func Clear(p config.Paths) error {
	// TODO(core): Task 5
	return nil
}

// Matches reports c.Server == server (trailing slash ignored).
func (c *Credentials) Matches(server string) bool {
	// TODO(core): Task 5
	return false
}

// Redacted returns "msl_…(redacted)" for logs.
func (c *Credentials) Redacted() string {
	// TODO(core): Task 5
	return ""
}
