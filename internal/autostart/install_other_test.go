//go:build !darwin && !linux && !windows

package autostart

import (
	"errors"
	"testing"

	"github.com/patrache/masil/internal/config"
)

func TestOtherUnsupported(t *testing.T) {
	if err := Install("/usr/local/bin/masil", config.Paths{}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Install: %v", err)
	}
	if err := Uninstall(); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Uninstall: %v", err)
	}
	if st, err := Query(); !errors.Is(err, ErrUnsupported) || st != (Status{}) {
		t.Fatalf("Query: %+v, %v", st, err)
	}
}
