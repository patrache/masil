// Package audit is the owner-side record of a share
// (~/.config/masil/workspace-audit/<share ID>.jsonl): one JSON object per
// line, append-only, 0600 in a 0700 folder, removed after 30 days. Only the
// events listed in spec B8.5 are written; the owner's own key input never is.
package audit

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Retention is how long a share's record is kept.
const Retention = 30 * 24 * time.Hour

// Event names (Entry.Ev). The comment lists the Data keys.
const (
	EvShareStart     = "share_start"
	EvShareEnd       = "share_end"
	EvJoinRequest    = "join_request"
	EvSAS            = "sas" // result: match|mismatch|skipped
	EvJoinAllow      = "join_allow"
	EvJoinDecline    = "join_decline"
	EvJoinExpired    = "join_expired"
	EvLevel          = "level"
	EvKeyboardGrant  = "keyboard_grant"
	EvKeyboardExpire = "keyboard_expire"
	EvKeyboardPause  = "keyboard_pause"
	EvKick           = "kick"
	EvProposal       = "proposal"        // text
	EvProposalAction = "proposal_action" // action, text
	EvKeyboardInput  = "keyboard_input"  // b64
	EvPeerSave       = "peer_save"       // rel, size
	EvFsRequest      = "fs_request"
	EvFsAction       = "fs_action"
	EvFrameRejected  = "frame_rejected" // reason, kind
	EvPathViolation  = "path_violation" // rel, reason
	EvTamperWarning  = "tamper_warning"
)

// Entry is one line.
type Entry struct {
	At   time.Time      `json:"at"`
	Ev   string         `json:"ev"`
	Seat int            `json:"seat,omitempty"`
	User string         `json:"user,omitempty"`
	Data map[string]any `json:"data,omitempty"`
}

// Log is an open record file.
type Log struct {
	mu sync.Mutex
	f  *os.File
}

var errShareID = errors.New("audit: bad share id")

func validID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	return !strings.ContainsFunc(id, func(c rune) bool {
		return !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-')
	})
}

// Open opens (creating) dir/<shareID>.jsonl for appending. The share ID must
// be [A-Za-z0-9_-]{1,64} so it cannot name another path.
func Open(dir, shareID string) (*Log, error) {
	if !validID(shareID) {
		return nil, errShareID
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(dir, 0o700); err != nil {
			return nil, err
		}
	}
	p := filepath.Join(dir, shareID+".jsonl")
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	if runtime.GOOS != "windows" {
		if err := f.Chmod(0o600); err != nil {
			_ = f.Close()
			return nil, err
		}
	}
	return &Log{f: f}, nil
}

// Write appends e as one line with a single write call.
func (l *Log) Write(e Entry) error {
	line, err := json.Marshal(e)
	if err != nil {
		return err
	}
	line = append(line, '\n')
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return os.ErrClosed
	}
	_, err = l.f.Write(line)
	return err
}

// Close closes the file.
func (l *Log) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return nil
	}
	err := l.f.Close()
	l.f = nil
	return err
}

// Purge removes *.jsonl files in dir last modified more than Retention
// before now and returns how many it removed. A missing dir is not an error.
func Purge(dir string, now time.Time) (int, error) {
	des, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	cutoff := now.Add(-Retention)
	n := 0
	var firstErr error
	for _, de := range des {
		if !de.Type().IsRegular() || filepath.Ext(de.Name()) != ".jsonl" {
			continue
		}
		fi, err := de.Info()
		if err != nil || !fi.ModTime().Before(cutoff) {
			continue
		}
		if err := os.Remove(filepath.Join(dir, de.Name())); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		n++
	}
	return n, firstErr
}
