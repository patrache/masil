package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/patrache/masil/internal/config"
)

// runStatusVersion is the only daemon-status.json schema version.
const runStatusVersion = 1

// runStatus is daemon-status.json: what the running daemon itself sees, so
// `masil daemon status` compares against the daemon's environment instead of
// the shell's (plan 06 Review Focus 1). Daemon-only file, outside CA 3.2.
type runStatus struct {
	Version           int       `json:"version"`
	PID               int       `json:"pid"`
	UpdatedAt         time.Time `json:"updated_at"`
	ConfigServer      string    `json:"config_server"`      // cfg.ServerURL() in the daemon's own environment
	CredentialsServer string    `json:"credentials_server"` // "" when not logged in
	ServerMismatch    bool      `json:"server_mismatch"`
	EnvServer         bool      `json:"env_server"` // MASIL_SERVER set in the daemon's environment
}

func runStatusPath(p config.Paths) string {
	return filepath.Join(p.Dir, "daemon-status.json")
}

// writeRunStatus writes s atomically with 0600, forcing the schema version.
func writeRunStatus(p config.Paths, s runStatus) error {
	s.Version = runStatusVersion
	data, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("daemon: encode run status: %w", err)
	}
	if err := config.WriteFileAtomic(runStatusPath(p), data, 0o600); err != nil {
		return fmt.Errorf("daemon: write run status: %w", err)
	}
	return nil
}

// readRunStatus reads daemon-status.json; a missing file is (nil, nil).
func readRunStatus(p config.Paths) (*runStatus, error) {
	data, err := os.ReadFile(runStatusPath(p))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("daemon: read run status: %w", err)
	}
	var s runStatus
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("daemon: parse run status: %w", err)
	}
	return &s, nil
}
