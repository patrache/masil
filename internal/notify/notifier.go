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

// Handle runs Decide; if Popup => Show (ledger.MarkIfNew; only new => backend.Show).
// The returned error is a display (backend) failure only. Titles and bodies are
// never logged.
func (n *Notifier) Handle(a api.Alert, in Input) (Decision, error) {
	d := Decide(a, in)
	if d != Popup {
		return d, nil
	}
	return d, n.Show(a)
}

// Show is the Popup half of Handle: ledger.MarkIfNew, and backend.Show only when
// the alert is new. It blocks on file I/O (up to the 2s ledger lock wait) and on
// the OS notifier, so the TUI runs it inside a tea.Cmd after a synchronous Decide.
// The returned error is a display (backend) failure only: a ledger failure still
// shows (a duplicate is better than a miss, CA 3.7) and is logged as a warning.
func (n *Notifier) Show(a api.Alert) error {
	isNew := true
	if n.ledger != nil {
		var err error
		isNew, err = n.ledger.MarkIfNew(a.ID)
		if err != nil {
			n.log.Warn("alert ledger unavailable; showing anyway", "alert_id", a.ID, "kind", string(a.Kind), "err", err)
		}
	}
	if !isNew || n.backend == nil {
		return nil
	}
	return n.backend.Show(Format(a))
}
