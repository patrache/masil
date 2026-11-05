package widgets

import (
	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/ui"
)

// Composer wraps a textarea: virtual cursor OFF, no line numbers, no prompt,
// CharLimit from limits; KeyMap removes ctrl+k (DeleteAfterCursor), ctrl+f
// (CharacterForward), ctrl+n (LineNext) — they are hard globals; InsertNewline =
// shift+enter, alt+enter, ctrl+j (enter is send). INPUT NORMALIZATION (CA 8.4):
// KeyPressMsg.Text, PasteMsg.Content and SetValue go through textwidth.SanitizeInput
// BEFORE reaching the textarea.
type Composer struct { /* textarea wrapper */
}

// Completion is one @mention / :emoji / /command popup row.
type Completion struct{ Label, Insert, Detail string }

// NewComposer creates a composer.
func NewComposer(placeholder string) *Composer {
	// TODO(core): Task 22
	return nil
}

// Value is already normalized; this exact string is what gets sent.
func (c *Composer) Value() string {
	// TODO(core): Task 22
	return ""
}

// SetValue normalizes (edit-last loads the server body through this).
func (c *Composer) SetValue(s string) {
	// TODO(core): Task 22
}

// Reset clears the input.
func (c *Composer) Reset() {
	// TODO(core): Task 22
}

// Focus focuses the input.
func (c *Composer) Focus() tea.Cmd {
	// TODO(core): Task 22
	return nil
}

// Blur blurs the input.
func (c *Composer) Blur() {
	// TODO(core): Task 22
}

// Focused reports focus.
func (c *Composer) Focused() bool {
	// TODO(core): Task 22
	return false
}

// Update feeds a message to the textarea (after normalization).
func (c *Composer) Update(ctx *ui.Context, msg tea.Msg) tea.Cmd {
	// TODO(core): Task 22
	return nil
}

// View auto-grows 1..maxRows, bordered with acc when focused.
func (c *Composer) View(ctx *ui.Context, width, maxRows int) string {
	// TODO(core): Task 22
	return ""
}

// Height returns the rendered height.
func (c *Composer) Height(width, maxRows int) int {
	// TODO(core): Task 22
	return 0
}

// Cursor is relative to the Composer.View origin.
func (c *Composer) Cursor() *tea.Cursor {
	// TODO(core): Task 22
	return nil
}

// SetCompletion sets the @mention / :emoji / /command popup above the input.
func (c *Composer) SetCompletion(items []Completion) {
	// TODO(core): Task 22
}
