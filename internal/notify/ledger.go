package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/gofrs/flock"

	"github.com/patrache/masil/internal/config"
)

// Ledger limits and timing (CA 3.7).
const (
	ledgerVersion = 1
	ledgerCap     = 500
	lockWait      = 2 * time.Second
	lockRetry     = 50 * time.Millisecond
)

// Ledger is alerts-shown.json guarded by alerts-shown.lock (CA 3.7).
type Ledger struct {
	path, lockPath string
	gate           chan struct{} // in-process gate shared by every Ledger on lockPath
}

type ledgerFile struct {
	Version int      `json:"version"`
	IDs     []string `json:"ids"`
}

// gates serializes goroutines of one process before they contend for the
// flock: a polling TryLock alone lets one goroutine starve another.
var (
	gatesMu sync.Mutex
	gates   = map[string]chan struct{}{}
)

func gateFor(lockPath string) chan struct{} {
	key := filepath.Clean(lockPath)
	gatesMu.Lock()
	defer gatesMu.Unlock()
	g, ok := gates[key]
	if !ok {
		g = make(chan struct{}, 1)
		gates[key] = g
	}
	return g
}

// OpenLedger returns the device ledger.
func OpenLedger(p config.Paths) *Ledger {
	return &Ledger{path: p.AlertsShown, lockPath: p.AlertsShownLock, gate: gateFor(p.AlertsShownLock)}
}

// MarkIfNew does flock + read + append + atomic write (CA 3.7).
// If the lock cannot be taken within 2s it returns (true, err): showing a
// duplicate is better than missing a notification.
func (l *Ledger) MarkIfNew(alertID string) (isNew bool, err error) {
	if alertID == "" {
		return true, nil // nothing to de-duplicate on
	}
	ctx, cancel := context.WithTimeout(context.Background(), lockWait)
	defer cancel()

	select {
	case l.gate <- struct{}{}:
		defer func() { <-l.gate }()
	case <-ctx.Done():
		return true, fmt.Errorf("notify: ledger busy: %w", ctx.Err())
	}

	if err := os.MkdirAll(filepath.Dir(l.lockPath), 0o700); err != nil {
		return true, fmt.Errorf("notify: ledger dir: %w", err)
	}
	fl := flock.New(l.lockPath, flock.SetPermissions(0o600))
	locked, err := fl.TryLockContext(ctx, lockRetry)
	if err != nil || !locked {
		_ = fl.Close()
		if err == nil {
			err = errors.New("lock not acquired")
		}
		return true, fmt.Errorf("notify: lock %s: %w", l.lockPath, err)
	}
	defer func() {
		_ = fl.Unlock()
		_ = fl.Close()
	}()

	ids := l.read()
	if slices.Contains(ids, alertID) {
		return false, nil
	}
	ids = append(ids, alertID)
	if over := len(ids) - ledgerCap; over > 0 {
		ids = ids[over:]
	}
	data, err := json.Marshal(ledgerFile{Version: ledgerVersion, IDs: ids})
	if err != nil {
		return true, fmt.Errorf("notify: encode ledger: %w", err)
	}
	if err := config.WriteFileAtomic(l.path, data, 0o600); err != nil {
		return true, fmt.Errorf("notify: write ledger: %w", err)
	}
	return true, nil
}

// Seen reports whether alertID is recorded. The file is replaced atomically,
// so reading needs no lock; a missing or corrupt file records nothing.
func (l *Ledger) Seen(alertID string) (bool, error) {
	return slices.Contains(l.read(), alertID), nil
}

// read returns the recorded ids, oldest first; missing, unreadable or corrupt
// files (bad JSON, wrong version) count as an empty record (CA 3.7).
func (l *Ledger) read() []string {
	b, err := os.ReadFile(l.path)
	if err != nil {
		return nil
	}
	var f ledgerFile
	if json.Unmarshal(b, &f) != nil || f.Version != ledgerVersion {
		return nil
	}
	return f.IDs
}
