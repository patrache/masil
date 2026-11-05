package imgrender_test

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image/color"
	"image/png"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/ansi/kitty"

	"github.com/patrache/masil/internal/imgrender"
	"github.com/patrache/masil/internal/termcaps"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

var testBG = color.RGBA{0x28, 0x28, 0x28, 0xff}

func kittyCaps() termcaps.Caps { return termcaps.Caps{Graphics: termcaps.GraphicsKitty} }

func opts(cols, rows int) imgrender.Options {
	return imgrender.Options{Cols: cols, Rows: rows, BG: testBG}
}

// checkWidths asserts rows lines of exactly cols cells by both width rules.
func checkWidths(t *testing.T, lines []string, cols, rows int) {
	t.Helper()
	if len(lines) != rows {
		t.Fatalf("len(lines) = %d, want %d: %q", len(lines), rows, lines)
	}
	for i, l := range lines {
		if w := ansi.StringWidth(l); w != cols {
			t.Fatalf("line %d StringWidth = %d, want %d: %q", i, w, cols, l)
		}
		if w := ansi.StringWidthWc(l); w != cols {
			t.Fatalf("line %d StringWidthWc = %d, want %d: %q", i, w, cols, l)
		}
	}
}

func checkHalfBlocks(t *testing.T, lines []string, cols, rows int) {
	t.Helper()
	checkWidths(t, lines, cols, rows)
	for i, l := range lines {
		if s := ansi.Strip(l); s != strings.Repeat("▀", cols) {
			t.Fatalf("line %d stripped = %q, want %d half blocks", i, s, cols)
		}
	}
}

// apcKeys returns the control keys ("a=T", "i=3", ...) of the n-th APC in s.
func apcKeys(t *testing.T, s string, n int) map[string]bool {
	t.Helper()
	seqs := strings.Split(s, "\x1b\\")
	if n >= len(seqs)-1 {
		t.Fatalf("want at least %d APCs in %.80q", n+1, s)
	}
	body, ok := strings.CutPrefix(seqs[n], "\x1b_G")
	if !ok {
		t.Fatalf("APC %d = %.40q, want a kitty APC", n, seqs[n])
	}
	ctrl, _, _ := strings.Cut(body, ";")
	keys := map[string]bool{}
	for _, kv := range strings.Split(ctrl, ",") {
		keys[kv] = true
	}
	return keys
}

func checkPlaceholders(t *testing.T, out imgrender.Rendered, cols, rows int) {
	t.Helper()
	checkWidths(t, out.Lines, cols, rows)
	id := out.ID
	fg := fmt.Sprintf("38;2;%d;%d;%d", byte(id>>16), byte(id>>8), byte(id))
	for r, l := range out.Lines {
		if !strings.Contains(l, fg) {
			t.Fatalf("line %d = %q, want the image id %d as fg color %s", r, l, id, fg)
		}
		want := ""
		for c := range cols {
			want += string(kitty.Placeholder) + string(kitty.Diacritic(r)) + string(kitty.Diacritic(c))
		}
		if s := ansi.Strip(l); s != want {
			t.Fatalf("line %d stripped = %q, want %q", r, s, want)
		}
	}
}

func TestRenderKittyPlaceholders(t *testing.T) {
	r := imgrender.New(kittyCaps(), nil)
	if r.Caps() != kittyCaps() {
		t.Fatalf("Caps() = %+v", r.Caps())
	}
	img := fixtures.TestImage()
	out := r.Render("u1", img, opts(10, 4))
	if out.ID == 0 {
		t.Fatalf("ID = 0, want a positive kitty image id")
	}
	if !strings.HasPrefix(out.Setup, "\x1b_G") {
		t.Fatalf("Setup = %.40q, want a kitty APC", out.Setup)
	}
	keys := apcKeys(t, out.Setup, 0)
	// f=32 (RGBA) and t=d (direct) are the protocol defaults, so not sent.
	for _, kv := range []string{"a=T", "U=1", "c=10", "r=4", "q=2", fmt.Sprintf("i=%d", out.ID)} {
		if !keys[kv] {
			t.Fatalf("first APC %v lacks %q", keys, kv)
		}
	}
	for kv := range keys {
		if strings.HasPrefix(kv, "f=") || strings.HasPrefix(kv, "t=") {
			t.Fatalf("first APC %v has non-default %q, want RGBA direct", keys, kv)
		}
	}
	checkPlaceholders(t, out, 10, 4)

	again := r.Render("u1", img, opts(10, 4))
	if again.Setup != "" || again.ID != out.ID {
		t.Fatalf("second Render: Setup %.30q ID %d, want empty and %d", again.Setup, again.ID, out.ID)
	}
	checkPlaceholders(t, again, 10, 4)

	if u2 := r.Render("u2", img, opts(10, 4)); u2.ID == out.ID || u2.Setup == "" {
		t.Fatalf("u2: ID %d Setup empty=%v, want a new id and a transmit", u2.ID, u2.Setup == "")
	}

	// Size round trip 10 -> 12 -> 10: each size keeps its own placement.
	o12 := r.Render("u1", img, opts(12, 4))
	if o12.Setup == "" || o12.ID == out.ID {
		t.Fatalf("12 cols: ID %d Setup empty=%v, want a new id and a transmit", o12.ID, o12.Setup == "")
	}
	checkPlaceholders(t, o12, 12, 4)
	back := r.Render("u1", img, opts(10, 4))
	if back.ID != out.ID || back.Setup != "" {
		t.Fatalf("back to 10 cols: ID %d Setup %.30q, want %d and no transmit", back.ID, back.Setup, out.ID)
	}

	// Fit and BG are part of the combination too.
	cover := opts(10, 4)
	cover.Fit = imgrender.FitCover
	if oc := r.Render("u1", img, cover); oc.ID == out.ID || oc.Setup == "" {
		t.Fatalf("cover: ID %d, want its own id", oc.ID)
	}
	otherBG := opts(10, 4)
	otherBG.BG = color.RGBA{1, 2, 3, 255}
	if ob := r.Render("u1", img, otherBG); ob.ID == out.ID || ob.Setup == "" {
		t.Fatalf("other bg: ID %d, want its own id", ob.ID)
	}

	// Peek of a sent combination: same lines, ready.
	pk, ready := r.Peek("u1", img, opts(10, 4))
	if !ready || pk.Setup != "" || pk.ID != out.ID {
		t.Fatalf("Peek sent: ready %v Setup %.30q ID %d", ready, pk.Setup, pk.ID)
	}
	checkPlaceholders(t, pk, 10, 4)

	// Peek before any Render: half blocks, not ready, and no state change.
	p3, ready := r.Peek("u3", img, opts(10, 4))
	if ready || p3.Setup != "" {
		t.Fatalf("Peek unsent: ready %v Setup %.30q, want false and empty", ready, p3.Setup)
	}
	checkHalfBlocks(t, p3.Lines, 10, 4)
	if p3b, _ := r.Peek("u3", img, opts(10, 4)); p3b.Setup != "" {
		t.Fatalf("second Peek emitted Setup")
	}
	if u3 := r.Render("u3", img, opts(10, 4)); u3.Setup == "" {
		t.Fatalf("Render after Peek: Setup empty, Peek must not mark the combination sent")
	}
}

func TestRenderKittyTransmitsDecodablePixels(t *testing.T) {
	r := imgrender.New(kittyCaps(), nil)
	out := r.Render("px", fixtures.TestImage(), opts(4, 2))
	// Reassemble the chunked payload and check s/v describe it.
	var payload strings.Builder
	var w, h int
	for _, seq := range strings.Split(out.Setup, "\x1b\\") {
		if seq == "" {
			continue
		}
		body := strings.TrimPrefix(seq, "\x1b_G")
		ctrl, data, _ := strings.Cut(body, ";")
		for _, kv := range strings.Split(ctrl, ",") {
			fmt.Sscanf(kv, "s=%d", &w)
			fmt.Sscanf(kv, "v=%d", &h)
		}
		payload.WriteString(data)
	}
	raw, err := base64.StdEncoding.DecodeString(payload.String())
	if err != nil {
		t.Fatalf("payload base64: %v", err)
	}
	if w <= 0 || h <= 0 || len(raw) != w*h*4 {
		t.Fatalf("s=%d v=%d with %d RGBA bytes, want s*v*4", w, h, len(raw))
	}
}

func TestRenderKittyEvictionDeletes(t *testing.T) {
	r := imgrender.New(kittyCaps(), nil)
	imgrender.SetRenderLimit(r, 2)
	img := fixtures.TestImage()
	a := r.Render("a", img, opts(6, 3))
	b := r.Render("b", img, opts(6, 3))
	if a.Setup == "" || b.Setup == "" || a.ID == b.ID {
		t.Fatalf("a/b: %d %d", a.ID, b.ID)
	}
	c := r.Render("c", img, opts(6, 3))
	if !strings.HasPrefix(c.Setup, "\x1b_G") {
		t.Fatalf("c.Setup = %.40q, want a kitty APC", c.Setup)
	}
	del := fmt.Sprintf("i=%d", a.ID)
	di := strings.Index(c.Setup, "a=d")
	ti := strings.Index(c.Setup, "a=T")
	if di < 0 || ti < 0 || di > ti {
		t.Fatalf("c.Setup = %.200q, want a delete before the transmit", c.Setup)
	}
	dk := apcKeys(t, c.Setup, 0)
	for _, kv := range []string{"a=d", "d=I", del} {
		if !dk[kv] {
			t.Fatalf("delete APC %v lacks %q", dk, kv)
		}
	}
	if tk := apcKeys(t, c.Setup, 1); !tk["a=T"] || !tk[fmt.Sprintf("i=%d", c.ID)] {
		t.Fatalf("second APC %v, want the transmit of i=%d", tk, c.ID)
	}
	if strings.Count(c.Setup, "a=d") != 1 {
		t.Fatalf("c.Setup has %d deletes, want 1", strings.Count(c.Setup, "a=d"))
	}

	// b is still live; a was evicted and comes back with a new id.
	if b2 := r.Render("b", img, opts(6, 3)); b2.Setup != "" || b2.ID != b.ID {
		t.Fatalf("b again: Setup %.30q ID %d, want cached %d", b2.Setup, b2.ID, b.ID)
	}
	a2 := r.Render("a", img, opts(6, 3))
	if a2.ID == a.ID || a2.Setup == "" {
		t.Fatalf("a again: ID %d (old %d) Setup empty=%v, want a new id and transmit", a2.ID, a.ID, a2.Setup == "")
	}
	// a2 evicted c's peer (b or c): exactly one delete, never a's old id again.
	if strings.Count(a2.Setup, "a=d") != 1 || apcKeys(t, a2.Setup, 0)[del] {
		t.Fatalf("a2.Setup = %.200q, want one delete of another id", a2.Setup)
	}
}

func TestRenderTmuxFallsBackToHalfBlock(t *testing.T) {
	r := imgrender.New(termcaps.Caps{Graphics: termcaps.GraphicsKitty, InTmux: true}, nil)
	out := r.Render("t", fixtures.TestImage(), opts(8, 3))
	if out.Setup != "" || out.ID != 0 {
		t.Fatalf("tmux: Setup %.30q ID %d, want none", out.Setup, out.ID)
	}
	checkHalfBlocks(t, out.Lines, 8, 3)
	pk, ready := r.Peek("t", fixtures.TestImage(), opts(8, 3))
	if !ready {
		t.Fatalf("Peek in tmux not ready")
	}
	checkHalfBlocks(t, pk.Lines, 8, 3)
}

func TestRenderITerm2UsesHalfBlockInLists(t *testing.T) {
	r := imgrender.New(termcaps.Caps{Graphics: termcaps.GraphicsITerm2}, nil)
	img := fixtures.TestImage()
	out := r.Render("i", img, opts(8, 3))
	if out.Setup != "" || out.ID != 0 {
		t.Fatalf("iterm2: Setup %.30q ID %d, want none", out.Setup, out.ID)
	}
	checkHalfBlocks(t, out.Lines, 8, 3)
	want := imgrender.HalfBlock(img, 8, 3, testBG)
	if strings.Join(out.Lines, "\n") != strings.Join(want, "\n") {
		t.Fatalf("iterm2 lines differ from HalfBlock")
	}
	pk, ready := r.Peek("i", img, opts(8, 3))
	if !ready || strings.Join(pk.Lines, "\n") != strings.Join(out.Lines, "\n") {
		t.Fatalf("Peek: ready %v, lines differ", ready)
	}

	hb := imgrender.New(termcaps.Caps{Graphics: termcaps.GraphicsHalfBlock}, nil)
	cover := opts(8, 3)
	cover.Fit = imgrender.FitCover
	checkHalfBlocks(t, hb.Render("h", img, cover).Lines, 8, 3)
}

func TestRenderNoneGraphics(t *testing.T) {
	r := imgrender.New(termcaps.Caps{Graphics: termcaps.GraphicsNone}, nil)
	for _, c := range []struct{ cols, rows int }{{10, 4}, {11, 5}, {2, 1}, {1, 1}} {
		out := r.Render("n", fixtures.TestImage(), opts(c.cols, c.rows))
		if out.Setup != "" || out.ID != 0 {
			t.Fatalf("none: Setup %.30q ID %d", out.Setup, out.ID)
		}
		checkWidths(t, out.Lines, c.cols, c.rows)
		all := ansi.Strip(strings.Join(out.Lines, ""))
		icons := strings.Count(all, "🖼")
		wantIcons := 1 // "🖼" (no VS16) is one cell by every width rule
		if icons != wantIcons {
			t.Fatalf("%dx%d: %d icons in %q, want %d", c.cols, c.rows, icons, all, wantIcons)
		}
		if strings.Trim(strings.ReplaceAll(all, "🖼", ""), " ") != "" {
			t.Fatalf("%dx%d: %q, want only spaces besides the icon", c.cols, c.rows, all)
		}
	}
}

func TestRenderDegenerate(t *testing.T) {
	for _, caps := range []termcaps.Caps{kittyCaps(), {Graphics: termcaps.GraphicsHalfBlock}, {Graphics: termcaps.GraphicsNone}} {
		r := imgrender.New(caps, nil)
		if out := r.Render("d", fixtures.TestImage(), opts(0, 3)); out.Lines != nil || out.Setup != "" {
			t.Fatalf("%v: zero cols gave %+v", caps, out)
		}
		if out, _ := r.Peek("d", fixtures.TestImage(), opts(3, -1)); out.Lines != nil {
			t.Fatalf("%v: negative rows gave %+v", caps, out)
		}
		// A nil image still fills the box exactly and transmits nothing.
		out := r.Render("nil", nil, opts(4, 2))
		checkWidths(t, out.Lines, 4, 2)
		if out.Setup != "" {
			t.Fatalf("%v: nil image transmitted", caps)
		}
	}
}

func TestITerm2Inline(t *testing.T) {
	img := fixtures.TestImage()
	s, err := imgrender.ITerm2Inline(img, 4, 2, 20, 10)
	if err != nil {
		t.Fatalf("ITerm2Inline: %v", err)
	}
	if !strings.HasPrefix(s, ansi.SaveCursor+"\x1b[3;5H") {
		t.Fatalf("prefix %.20q, want save cursor + CUP 3;5", s)
	}
	if !strings.HasSuffix(s, "\x07"+ansi.RestoreCursor) {
		t.Fatalf("suffix %q, want BEL + restore cursor", s[len(s)-10:])
	}
	for _, part := range []string{"\x1b]1337;File=", "inline=1", "width=20", "height=10", "size="} {
		if !strings.Contains(s, part) {
			t.Fatalf("ITerm2Inline lacks %q: %.120q", part, s)
		}
	}
	i := strings.Index(s, ":")
	j := strings.LastIndex(s, "\x07")
	raw, err := base64.StdEncoding.DecodeString(s[i+1 : j])
	if err != nil {
		t.Fatalf("content base64: %v", err)
	}
	if _, err := png.Decode(bytes.NewReader(raw)); err != nil {
		t.Fatalf("content is not a PNG: %v", err)
	}
	if !strings.Contains(s, fmt.Sprintf("size=%d;", len(raw))) {
		t.Fatalf("size does not match the %d PNG bytes", len(raw))
	}

	if _, err := imgrender.ITerm2Inline(nil, 0, 0, 20, 10); err == nil {
		t.Fatalf("nil image: want an error")
	}
	if _, err := imgrender.ITerm2Inline(img, 0, 0, 0, 10); err == nil {
		t.Fatalf("zero cols: want an error")
	}
}

func TestPeekHalfBlockMemoizes(t *testing.T) {
	for _, g := range []termcaps.Graphics{termcaps.GraphicsHalfBlock, termcaps.GraphicsITerm2} {
		r := imgrender.New(termcaps.Caps{Graphics: g}, nil)
		img := fixtures.TestImage()
		before := imgrender.FitCalls()
		p1, ready := r.Peek("p", img, opts(8, 3))
		if !ready {
			t.Fatalf("%v: first Peek not ready", g)
		}
		p2, ready := r.Peek("p", img, opts(8, 3))
		if !ready || strings.Join(p1.Lines, "\n") != strings.Join(p2.Lines, "\n") {
			t.Fatalf("%v: second Peek: ready %v, lines differ", g, ready)
		}
		checkHalfBlocks(t, p2.Lines, 8, 3)
		if n := imgrender.FitCalls() - before; n != 1 {
			t.Fatalf("%v: two Peeks scaled %d times, want 1", g, n)
		}
		// Render reuses what Peek drew.
		out := r.Render("p", img, opts(8, 3))
		if strings.Join(out.Lines, "\n") != strings.Join(p1.Lines, "\n") || out.Setup != "" {
			t.Fatalf("%v: Render after Peek differs", g)
		}
		if n := imgrender.FitCalls() - before; n != 1 {
			t.Fatalf("%v: Render after Peek scaled again (%d)", g, n)
		}
	}
}
