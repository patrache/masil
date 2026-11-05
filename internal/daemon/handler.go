package daemon

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/notify"
	"github.com/patrache/masil/internal/realtime"
)

// alertSink decides and shows one alert; *notify.Notifier satisfies it.
type alertSink interface {
	Handle(a api.Alert, in notify.Input) (notify.Decision, error)
}

// deferred is an alert held for the TUI grace period, judged again once.
type deferred struct {
	alert    api.Alert
	replayed bool
}

// handler is the daemon's alert queue (events.md 7, CA 9.2). onEvent runs on
// the realtime goroutine and only appends to an unbounded slice, so it never
// blocks and never drops; run processes events one by one in arrival order on
// its own goroutine and marks every logged seq after handling it.
type handler struct {
	sink    alertSink
	pr      *presence
	desktop func() bool
	now     func() time.Time
	after   func(time.Duration) <-chan time.Time
	mark    func(seq int64)
	log     *slog.Logger

	mu    sync.Mutex
	queue []realtime.Event

	wake       chan struct{} // capacity 1: "queue is non-empty"
	deferredCh chan deferred
}

func newHandler(sink alertSink, pr *presence, desktop func() bool, now func() time.Time,
	after func(time.Duration) <-chan time.Time, mark func(seq int64), log *slog.Logger) *handler {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &handler{
		sink: sink, pr: pr, desktop: desktop, now: now, after: after, mark: mark, log: log,
		wake:       make(chan struct{}, 1),
		deferredCh: make(chan deferred),
	}
}

// onEvent queues ev. It is called from realtime OnEvent and must never block:
// the queue has no capacity limit (resume replay is bounded by the server's
// replay window, and notify mode only carries alert/notification/focus
// events), and the wake signal is a non-blocking send.
func (h *handler) onEvent(ev realtime.Event) {
	h.mu.Lock()
	h.queue = append(h.queue, ev)
	h.mu.Unlock()
	select {
	case h.wake <- struct{}{}:
	default:
	}
}

// run processes queued events until ctx is done.
func (h *handler) run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-h.wake:
			h.mu.Lock()
			batch := h.queue
			h.queue = nil
			h.mu.Unlock()
			for _, ev := range batch {
				if ctx.Err() != nil {
					return
				}
				h.process(ctx, ev)
			}
		case d := <-h.deferredCh:
			present, wait := h.pr.decide()
			if wait > 0 {
				present = false // still in grace after waiting once: treat as absent
			}
			h.show(d.alert, d.replayed, present)
		}
	}
}

// process handles one event and then marks its seq, keeping marks in order.
func (h *handler) process(ctx context.Context, ev realtime.Event) {
	switch ev.Type {
	case "alert":
		h.onAlert(ctx, ev)
	case "ready":
		var r realtime.Ready
		if err := json.Unmarshal(ev.Data, &r); err == nil {
			h.log.Info("realtime ready", "resumed", r.Resumed, "resync_required", r.ResyncRequired, "replayed", r.Replayed)
		} else {
			h.log.Info("realtime ready")
		}
	default:
		// Not ours to parse (notification.*, focus.*, ...): dropped, still acked.
	}
	if ev.Seq != nil {
		h.mark(*ev.Seq)
	}
}

// onAlert judges presence; during the grace period it parks the alert for one
// re-judgement after the remaining wait instead of blocking the queue.
func (h *handler) onAlert(ctx context.Context, ev realtime.Event) {
	var body struct {
		Alert api.Alert `json:"alert"`
	}
	if err := json.Unmarshal(ev.Data, &body); err != nil {
		h.log.Warn("alert decode failed", "err", err)
		return
	}
	present, wait := h.pr.decide()
	if wait > 0 {
		// Take the timer now (before the caller marks the seq) so the deferral
		// is fully in place by the time it is acknowledged.
		timer := h.after(wait)
		d := deferred{alert: body.Alert, replayed: ev.Replayed}
		go func() {
			select {
			case <-timer:
				select {
				case h.deferredCh <- d:
				case <-ctx.Done():
				}
			case <-ctx.Done():
			}
		}()
		return
	}
	h.show(body.Alert, ev.Replayed, present)
}

// show hands the alert to the sink and logs the outcome without title or body.
func (h *handler) show(a api.Alert, replayed, present bool) {
	dec, err := h.sink.Handle(a, notify.Input{
		Role:           notify.RoleDaemon,
		Now:            h.now(),
		Replayed:       replayed,
		DesktopEnabled: h.desktop(),
		TUIPresent:     present,
	})
	if err != nil {
		h.log.Warn("alert display failed", "alert_id", a.ID, "kind", string(a.Kind), "err", err)
		return
	}
	msg := "alert skipped"
	if dec == notify.Popup {
		msg = "alert shown"
	}
	h.log.Info(msg, "alert_id", a.ID, "kind", string(a.Kind), "decision", dec.String())
}
