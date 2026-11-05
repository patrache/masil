package notify

import (
	"log/slog"

	"github.com/patrache/masil/internal/api"
)

// Notifier combines a backend, the ledger and a logger.
type Notifier struct { /* backend + ledger + logger */
}

// New creates a Notifier.
func New(b Backend, l *Ledger, log *slog.Logger) *Notifier {
	// TODO(core): Task 15
	return nil
}

// Handle runs Decide; if Popup => ledger.MarkIfNew; only new => backend.Show.
func (n *Notifier) Handle(a api.Alert, in Input) (Decision, error) {
	// TODO(core): Task 15
	return Skip, nil
}
