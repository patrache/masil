package notify_test

import (
	"bytes"
	"errors"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gofrs/flock"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/config"
	"github.com/patrache/masil/internal/notify"
)

func tuiInput() notify.Input {
	return notify.Input{Role: notify.RoleTUI, Now: now, DesktopEnabled: true}
}

func TestNotifierShowsOnce(t *testing.T) {
	p := newPaths(t)
	rec := &notify.Recorder{}
	n := notify.New(rec, notify.OpenLedger(p), slog.New(slog.DiscardHandler))
	a := api.Alert{ID: "P1", Kind: api.AlertDM, Title: "민지", Body: "안녕 ❤️", CreatedAt: now.Add(-time.Minute)}

	for i := 0; i < 2; i++ {
		d, err := n.Handle(a, tuiInput())
		if err != nil || d != notify.Popup {
			t.Fatalf("Handle #%d = %v, %v", i, d, err)
		}
	}
	if len(rec.Shown) != 1 {
		t.Fatalf("shown %d", len(rec.Shown))
	}
	if want := notify.Format(a, nil); rec.Shown[0] != want {
		t.Errorf("shown %+v, want %+v", rec.Shown[0], want)
	}

	silent := api.Alert{ID: "S1", Kind: api.AlertDM, CreatedAt: now, Suppressed: true}
	if d, err := n.Handle(silent, tuiInput()); err != nil || d != notify.Silent {
		t.Fatalf("suppressed = %v, %v", d, err)
	}
	in := tuiInput()
	in.Role = notify.RoleDaemon
	if d, err := n.Handle(silent, in); err != nil || d != notify.Skip {
		t.Fatalf("daemon suppressed = %v, %v", d, err)
	}
	if len(rec.Shown) != 1 {
		t.Fatalf("silent/skip shown: %d", len(rec.Shown))
	}
	if seen, _ := notify.OpenLedger(p).Seen("S1"); seen {
		t.Error("silent alert must not be recorded")
	}
}

type failBackend struct{ calls int }

func (f *failBackend) Show(notify.OSNotification) error { f.calls++; return errors.New("boom") }

func TestNotifierBackendErrorReturned(t *testing.T) {
	fb := &failBackend{}
	n := notify.New(fb, notify.OpenLedger(newPaths(t)), slog.New(slog.DiscardHandler))
	d, err := n.Handle(api.Alert{ID: "E1", CreatedAt: now}, tuiInput())
	if d != notify.Popup || err == nil || fb.calls != 1 {
		t.Fatalf("Handle = %v, %v (calls %d)", d, err, fb.calls)
	}
}

// A ledger lock timeout still shows (duplicate over miss), logs a warning and
// is not reported as a display failure.
func TestNotifierLedgerTimeoutStillShows(t *testing.T) {
	p := newPaths(t)
	if err := config.EnsureDir(p); err != nil {
		t.Fatal(err)
	}
	holder := flock.New(p.AlertsShownLock)
	if err := holder.Lock(); err != nil {
		t.Fatal(err)
	}
	defer holder.Close()

	var buf bytes.Buffer
	rec := &notify.Recorder{}
	n := notify.New(rec, notify.OpenLedger(p), slog.New(slog.NewTextHandler(&buf, nil)))
	d, err := n.Handle(api.Alert{ID: "L1", Title: "secret title", CreatedAt: now}, tuiInput())
	if d != notify.Popup || err != nil || len(rec.Shown) != 1 {
		t.Fatalf("Handle = %v, %v (shown %d)", d, err, len(rec.Shown))
	}
	if !strings.Contains(buf.String(), "level=WARN") || !strings.Contains(buf.String(), "L1") {
		t.Errorf("log = %q", buf.String())
	}
	if strings.Contains(buf.String(), "secret title") {
		t.Error("alert title must not be logged")
	}
}

func TestNotifierNilLogger(t *testing.T) {
	rec := &notify.Recorder{}
	n := notify.New(rec, notify.OpenLedger(newPaths(t)), nil)
	if _, err := n.Handle(api.Alert{ID: "N1", CreatedAt: now}, tuiInput()); err != nil || len(rec.Shown) != 1 {
		t.Fatalf("err %v shown %d", err, len(rec.Shown))
	}
}

func TestRecorder(t *testing.T) {
	r := &notify.Recorder{}
	if err := r.Show(notify.OSNotification{Title: "a", AlertID: "1"}); err != nil {
		t.Fatal(err)
	}
	if err := r.Show(notify.OSNotification{Title: "b", AlertID: "2"}); err != nil {
		t.Fatal(err)
	}
	if len(r.Shown) != 2 || r.Shown[1].AlertID != "2" {
		t.Fatalf("%+v", r.Shown)
	}
}

func TestOSBackend(t *testing.T) {
	b := notify.NewOSBackend("masil")
	if b == nil {
		t.Fatal("nil backend")
	}
	if os.Getenv("MASIL_TEST_OS_NOTIFY") != "1" {
		t.Skip("set MASIL_TEST_OS_NOTIFY=1 to show a real OS notification")
	}
	if err := b.Show(notify.OSNotification{Title: "masil 시험", Body: "OS 알림 확인 ❤", AlertID: "test"}); err != nil {
		t.Fatal(err)
	}
}

func TestNotifierShowIsThePopupHalf(t *testing.T) {
	p := newPaths(t)
	rec := &notify.Recorder{}
	n := notify.New(rec, notify.OpenLedger(p), nil)
	a := api.Alert{ID: "SH1", Kind: api.AlertDM, Title: "민지", Body: "안녕", CreatedAt: now}
	for i := 0; i < 2; i++ {
		if err := n.Show(a); err != nil {
			t.Fatalf("Show #%d: %v", i, err)
		}
	}
	if len(rec.Shown) != 1 {
		t.Fatalf("shown %d, want 1 (ledger de-dupes)", len(rec.Shown))
	}
	if d, err := n.Handle(a, tuiInput()); err != nil || d != notify.Popup || len(rec.Shown) != 1 {
		t.Fatalf("Handle after Show = %v, %v, shown %d", d, err, len(rec.Shown))
	}
}
