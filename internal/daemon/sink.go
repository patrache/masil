package daemon

import (
	"log/slog"
	"sync/atomic"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/notify"
)

// ledgerSink is the daemon's alertSink: notify.Decide, then the shared ledger's
// MarkIfNew, then backend.Show — the same steps as notify.Notifier.Handle, but
// it also reports whether a notification was actually displayed, so daemon.log
// can tell "shown" from "the ledger already had it" (the TUI showed it, or a
// replay after a restart or within the state-save debounce).
type ledgerSink struct {
	backend notify.Backend
	ledger  *notify.Ledger
	log     *slog.Logger
	lang    atomic.Value // i18n.Locale the notifications render in (default ko)
}

func newLedgerSink(b notify.Backend, l *notify.Ledger, log *slog.Logger) *ledgerSink {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	s := &ledgerSink{backend: b, ledger: l, log: log}
	s.lang.Store(i18n.Ko)
	return s
}

// setLocale picks the language alert templates render in (the daemon's
// effective language, re-read on config reload).
func (s *ledgerSink) setLocale(l i18n.Locale) { s.lang.Store(l) }

// catalog is the message catalog of the current language.
func (s *ledgerSink) catalog() *i18n.T { return i18n.For(s.lang.Load().(i18n.Locale)) }

// Handle returns the decision, whether the alert was displayed, and a display
// (backend) failure. A ledger failure still shows (a duplicate is better than a
// miss, CA 3.7) and is logged as a warning without title or body.
func (s *ledgerSink) Handle(a api.Alert, in notify.Input) (notify.Decision, bool, error) {
	d := notify.Decide(a, in)
	if d != notify.Popup {
		return d, false, nil
	}
	isNew := true
	if s.ledger != nil {
		var err error
		isNew, err = s.ledger.MarkIfNew(a.ID)
		if err != nil {
			isNew = true
			s.log.Warn("alert ledger unavailable; showing anyway", "alert_id", a.ID, "kind", string(a.Kind), "err", err)
		}
	}
	if !isNew || s.backend == nil {
		return d, false, nil
	}
	if err := s.backend.Show(notify.Format(a, s.catalog())); err != nil {
		return d, false, err
	}
	return d, true, nil
}
