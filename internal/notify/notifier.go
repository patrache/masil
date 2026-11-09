package notify

import (
	"log/slog"
	"sync"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/i18n"
)

// Notifier combines a backend, the ledger and a logger.
type Notifier struct {
	backend Backend
	ledger  *Ledger
	log     *slog.Logger

	mu     sync.Mutex
	locale i18n.Locale // catalog for Format (default ko)
}

// New creates a Notifier. A nil logger discards.
func New(b Backend, l *Ledger, log *slog.Logger) *Notifier {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Notifier{backend: b, ledger: l, log: log, locale: i18n.Ko}
}

// SetLocale picks the language OS notifications are rendered in (the
// effective UI language). Safe on a nil Notifier and from any goroutine.
func (n *Notifier) SetLocale(l i18n.Locale) {
	if n == nil {
		return
	}
	n.mu.Lock()
	n.locale = l
	n.mu.Unlock()
}

// Locale is the language set by SetLocale (default ko).
func (n *Notifier) Locale() i18n.Locale {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.locale
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
