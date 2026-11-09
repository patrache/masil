//go:build windows

package termhost

import (
	"errors"
	"testing"
)

func TestStartPaneUnsupportedOnWindows(t *testing.T) {
	_, err := StartPane(Request{Program: "cmd.exe"}, Size{W: 80, H: 24}, PaneOptions{})
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("StartPane err = %v, want ErrUnsupported", err)
	}
	if LivePanes() != 0 {
		t.Fatal("an unsupported start left a live pane")
	}
}
