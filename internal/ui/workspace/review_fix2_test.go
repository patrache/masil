package workspace

import (
	"testing"

	"github.com/patrache/masil/internal/workspace/share"
	"github.com/patrache/masil/internal/workspace/wire"
)

// A participant's replay screen never takes an owner size past the limit
// (a tampered owner client could otherwise make it allocate gigabytes).
func TestReplaySizeCapped(t *testing.T) {
	rt := newReplayTerm(1)
	rt.setStatus(wire.TermStatus{Term: 1, Cols: 100000, Rows: 100000})
	if sz := rt.size(); sz.W > share.MaxTermCols || sz.H > share.MaxTermRows {
		t.Fatalf("replay size %dx%d past the cap", sz.W, sz.H)
	}
	rt.keyframe([]byte("hi"))
	rt.setStatus(wire.TermStatus{Term: 1, Cols: 200000, Rows: 3})
	if sz := rt.size(); sz.W > share.MaxTermCols {
		t.Fatalf("resize to %dx%d past the cap", sz.W, sz.H)
	}
}
