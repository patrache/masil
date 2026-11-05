// Package uitest is the deterministic UI test harness: fake clock, synchronous
// Cmd execution, sizes and integrity checks (CA 10.2).
package uitest

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api/apitest"
	"github.com/patrache/masil/internal/notify"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/app"
)

// Harness drives an app.Model synchronously.
type Harness struct {
	T      testing.TB
	Model  *app.Model
	API    *apitest.Server
	Notify *notify.Recorder
}

// New builds a harness with a temp MASIL_CONFIG_DIR/MASIL_CACHE_DIR.
func New(tb testing.TB, opts ...Option) *Harness {
	// TODO(core): Task 25
	return nil
}

// Resize sends a WindowSizeMsg.
func (h *Harness) Resize(w, hgt int) {
	// TODO(core): Task 25
}

// Press sends keys: "ctrl+k", "j", "enter", "shift+tab", "esc".
func (h *Harness) Press(keys ...string) {
	// TODO(core): Task 25
}

// Type sends one KeyPressMsg per rune/syllable (Korean OK).
func (h *Harness) Type(s string) {
	// TODO(core): Task 25
}

// Paste sends a PasteMsg.
func (h *Harness) Paste(s string) {
	// TODO(core): Task 25
}

// Click sends a left click.
func (h *Harness) Click(x, y int) {
	// TODO(core): Task 25
}

// Wheel sends wheel events.
func (h *Harness) Wheel(x, y, delta int) {
	// TODO(core): Task 25
}

// Send runs Update + resulting Cmds synchronously (CA 10.2 Send rules).
func (h *Harness) Send(msg tea.Msg) {
	// TODO(core): Task 25
}

// Advance moves the FakeClock and delivers due timer msgs via Send, in order.
func (h *Harness) Advance(d time.Duration) {
	// TODO(core): Task 25
}

// Clock returns the fake clock.
func (h *Harness) Clock() *FakeClock {
	// TODO(core): Task 25
	return nil
}

// TeaMsgs returns bubbletea-internal msgs produced by Cmds (recorded, not re-dispatched).
func (h *Harness) TeaMsgs() []tea.Msg {
	// TODO(core): Task 25
	return nil
}

// Event injects a realtime.EventMsg (seq auto-increments for logged types).
func (h *Harness) Event(typ string, data any) {
	// TODO(core): Task 25
}

// Screen is ansi.Strip(View().Content).
func (h *Harness) Screen() string {
	// TODO(core): Task 25
	return ""
}

// Styled is the raw View().Content.
func (h *Harness) Styled() string {
	// TODO(core): Task 25
	return ""
}

// Golden runs h.Check() + golden.RequireEqual(Screen) under t.Run(name).
func (h *Harness) Golden(name string) {
	// TODO(core): Task 25
}

// Check runs AssertIntegrity without a golden (sweeps).
func (h *Harness) Check() {
	// TODO(core): Task 25
}

// Context returns the app's ui.Context.
func (h *Harness) Context() *ui.Context {
	// TODO(core): Task 25
	return nil
}

// Cursor returns the View cursor.
func (h *Harness) Cursor() *tea.Cursor {
	// TODO(core): Task 25
	return nil
}

// Nav returns the app's NavState.
func (h *Harness) Nav() app.NavState {
	// TODO(core): Task 25
	return app.NavState{}
}

// Instance is Model.Instance; tests type-assert to *stub.Chat etc.
func (h *Harness) Instance(routeKey string) ui.Component {
	// TODO(core): Task 25
	return nil
}
