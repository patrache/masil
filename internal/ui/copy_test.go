package ui_test

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/clipboard"
	"github.com/patrache/masil/internal/ui"
)

// runCopy runs the Cmd of ui.Copy and returns the OSC 52 payloads it emitted
// and its toasts.
func runCopy(t *testing.T, cmd tea.Cmd) (osc52 int, toasts []ui.ToastMsg) {
	t.Helper()
	if cmd == nil {
		t.Fatal("ui.Copy returned nil")
	}
	var walk func(tea.Msg)
	walk = func(m tea.Msg) {
		switch m := m.(type) {
		case tea.BatchMsg:
			for _, c := range m {
				if c != nil {
					walk(c())
				}
			}
		case ui.ToastMsg:
			toasts = append(toasts, m)
		case nil:
		default:
			if reflect.TypeOf(m).Name() == "setClipboardMsg" {
				osc52++
			}
		}
	}
	walk(cmd())
	return osc52, toasts
}

// TestCopyWritesBothPaths: ui.Copy sends OSC 52 and writes the system
// clipboard with the same sanitized text; the toast is honest about the
// system write.
func TestCopyWritesBothPaths(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want ui.ToastMsg
	}{
		{"system ok", nil, ui.ToastMsg{Text: "복사했어요", Kind: ui.ToastSuccess}},
		{"ssh: OSC 52 is the path", clipboard.ErrRemote, ui.ToastMsg{Text: "복사했어요", Kind: ui.ToastSuccess}},
		{"no program", clipboard.ErrUnavailable, ui.ToastMsg{Text: ui.CopyHint, Kind: ui.ToastInfo}},
		{"program failed", errors.New("exit status 1"), ui.ToastMsg{Text: ui.CopyHint, Kind: ui.ToastInfo}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := &clipboard.Recorder{}
			rec.Fail(c.err)
			ctx := &ui.Context{Clipboard: rec}
			osc, toasts := runCopy(t, ui.Copy(ctx, "안녕\r\n\x1b[31m빨강\x1b[0m\x00", "복사했어요"))
			if osc != 1 {
				t.Fatalf("OSC 52 sent %d times, want 1", osc)
			}
			if got := rec.Texts(); !slices.Equal(got, []string{"안녕\n빨강"}) {
				t.Fatalf("system clipboard got %q", got)
			}
			if len(toasts) != 1 || toasts[0].Text != c.want.Text || toasts[0].Kind != c.want.Kind {
				t.Fatalf("toasts %+v, want %+v", toasts, c.want)
			}
		})
	}
}

// TestCopyWithoutWriter: no system Writer (unit tests) is OSC 52 only.
func TestCopyWithoutWriter(t *testing.T) {
	osc, toasts := runCopy(t, ui.Copy(&ui.Context{}, "x", "복사했어요"))
	if osc != 1 || len(toasts) != 1 || toasts[0].Kind != ui.ToastSuccess {
		t.Fatalf("osc=%d toasts=%+v", osc, toasts)
	}
	osc, toasts = runCopy(t, ui.Copy(nil, "x", "복사했어요"))
	if osc != 1 || len(toasts) != 1 || toasts[0].Kind != ui.ToastSuccess {
		t.Fatalf("nil ctx: osc=%d toasts=%+v", osc, toasts)
	}
}

// TestCopyClamps: the payload is capped at clipboard.MaxBytes.
func TestCopyClamps(t *testing.T) {
	rec := &clipboard.Recorder{}
	runCopy(t, ui.Copy(&ui.Context{Clipboard: rec}, strings.Repeat("가", clipboard.MaxBytes), "ok"))
	if got := rec.Texts(); len(got) != 1 || len(got[0]) > clipboard.MaxBytes {
		t.Fatalf("len %d", len(got[0]))
	}
}
