// Package daemon is the background notification daemon behind `masil daemon run`
// (CA 9.2, events.md 7). It connects in notify mode, decides whether to show OS
// notifications for alerts, and keeps no state beyond its resume point.
package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"time"

	"github.com/patrache/masil/internal/config"
	"github.com/patrache/masil/internal/realtime"
)

// stateVersion is the only daemon-state.json schema version (CA 3.6).
const stateVersion = 1

// State is the daemon's resume point persisted in daemon-state.json (CA 3.6, 9.2).
type State struct {
	Version int       `json:"version"`
	UserID  string    `json:"user_id"`
	Epoch   string    `json:"epoch"`
	LastSeq int64     `json:"last_seq"`
	SavedAt time.Time `json:"saved_at"`
}

// loadState reads daemon-state.json. A missing file is (nil, nil); an unreadable
// or corrupt file is (nil, err) so the caller can log it and fall back to
// use_device_ack.
func loadState(p config.Paths) (*State, error) {
	data, err := os.ReadFile(p.DaemonState)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("daemon: read state: %w", err)
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("daemon: parse state: %w", err)
	}
	return &s, nil
}

// saveState writes s to daemon-state.json atomically with 0600, forcing the
// current schema version.
func saveState(p config.Paths, s State) error {
	s.Version = stateVersion
	data, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("daemon: encode state: %w", err)
	}
	if err := config.WriteFileAtomic(p.DaemonState, data, 0o600); err != nil {
		return fmt.Errorf("daemon: write state: %w", err)
	}
	return nil
}

// resumePoint resumes from the saved epoch/seq only when the state belongs to
// userID and carries an epoch; otherwise it asks the server for the device ack
// (CA 3.6: missing file or different user_id -> {"use_device_ack": true}).
func resumePoint(s *State, userID string) realtime.ResumePoint {
	if s == nil || s.UserID != userID || s.Epoch == "" {
		return realtime.ResumePoint{UseDeviceAck: true}
	}
	epoch, seq := s.Epoch, s.LastSeq
	return realtime.ResumePoint{Epoch: &epoch, LastSeq: &seq}
}
