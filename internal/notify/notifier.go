package notify

import (
	"log/slog"

	"github.com/patrache/masil/internal/api"
)

// Notifier combines a backend, the ledger and a logger.
type Notifier struct {
	backend Backend
	ledger  *Ledger
	log     *slog.Logger
}

// New creates a Notifier. A nil logger discards.
func New(b Backend, l *Ledger, log *slog.Logger) *Notifier {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Notifier{backend: b, ledger: l, log: log}
}

// Handle runs Decide; if Popup => ledger.MarkIfNew; only new => backend.Show.
// The returned error is a display (backend) failure only. A ledger failure
// still shows (a duplicate is better than a miss, CA 3.7) and is logged as a
// warning. Titles and bodies are never logged.
func (n *Notifier) Handle(a api.Alert, in Input) (Decision, error) {
	d := Decide(a, in)
	if d != Popup {
		return d, nil
	}
	isNew := true
	if n.ledger != nil {
		var err error
		isNew, err = n.ledger.MarkIfNew(a.ID)
		if err != nil {
			n.log.Warn("alert ledger unavailable; showing anyway", "alert_id", a.ID, "kind", string(a.Kind), "err", err)
		}
	}
	if !isNew || n.backend == nil {
		return d, nil
	}
	return d, n.backend.Show(Format(a))
}
