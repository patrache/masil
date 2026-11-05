package auth

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/patrache/masil/internal/config"
	"github.com/patrache/masil/internal/ids"
)

// DeviceID reads or creates device_id (ids.New()); it never rewrites an existing valid ULID.
// A missing or garbage file is replaced atomically with a fresh ULID plus "\n" (0600).
func DeviceID(p config.Paths) (string, error) {
	b, err := os.ReadFile(p.DeviceID)
	switch {
	case err == nil:
		if id := strings.TrimSpace(string(b)); ids.Valid(id) {
			return id, nil
		}
	case !errors.Is(err, os.ErrNotExist):
		return "", fmt.Errorf("auth: read device_id: %w", err)
	}
	if err := config.EnsureDir(p); err != nil {
		return "", err
	}
	id := ids.New()
	if err := config.WriteFileAtomic(p.DeviceID, []byte(id+"\n"), 0o600); err != nil {
		return "", fmt.Errorf("auth: write device_id: %w", err)
	}
	return id, nil
}
