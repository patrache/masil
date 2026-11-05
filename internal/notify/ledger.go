package notify

import "github.com/patrache/masil/internal/config"

// Ledger is alerts-shown.json guarded by alerts-shown.lock (CA 3.7).
type Ledger struct { /* path + lock path */
}

// OpenLedger returns the device ledger.
func OpenLedger(p config.Paths) *Ledger {
	// TODO(core): Task 15
	return nil
}

// MarkIfNew does flock + read + append + atomic write (CA 3.7).
func (l *Ledger) MarkIfNew(alertID string) (isNew bool, err error) {
	// TODO(core): Task 15
	return false, nil
}

// Seen reports whether alertID is recorded.
func (l *Ledger) Seen(alertID string) (bool, error) {
	// TODO(core): Task 15
	return false, nil
}
