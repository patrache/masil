package termhost

import (
	"fmt"
	"strings"
	"testing"

	"github.com/patrache/masil/internal/textwidth"
)

func TestReplayNotCounted(t *testing.T) {
	before := LivePanes()
	var rs []*Replay
	for range 10 {
		r := NewReplay(Size{W: 80, H: 24}, 0)
		r.Write([]byte("hello"))
		rs = append(rs, r)
	}
	if n := LivePanes(); n != before {
		t.Fatalf("LivePanes() = %d with 10 replays, want %d", n, before)
	}
	if len(rs) != 10 {
		t.Fatal("unreachable")
	}
}

func TestReplayRendersAndIgnoresOSC(t *testing.T) {
	r := NewReplay(Size{W: 20, H: 3}, 0)
	// Queries must not block (no one reads the replies) and OSC 52/8/titles
	// are not kept.
	r.Write([]byte("\x1b[6n\x1b[c\x1b]11;?\a\x1b]52;c;aGk=\a\x1b]2;title\a\x1b]8;;http://x\alink\x1b]8;;\a ok"))
	out := r.Render()
	if strings.Contains(out, "\x1b]") {
		t.Fatalf("Render() keeps OSC: %q", out)
	}
	lines := strings.Split(out, "\n")
	if len(lines) != 3 || !strings.HasPrefix(plain(lines[0]), "link ok") {
		t.Fatalf("Render() = %q", out)
	}
	for _, l := range lines {
		if textwidth.Width(l) != 20 {
			t.Fatalf("line width %d: %q", textwidth.Width(l), l)
		}
	}
	if x, y, vis := r.Cursor(); x != 7 || y != 0 || !vis {
		t.Fatalf("Cursor() = %d,%d,%v", x, y, vis)
	}
	r.Resize(Size{W: 30, H: 4})
	r.Resize(Size{}) // ignored
	if s := r.Size(); s != (Size{W: 30, H: 4}) {
		t.Fatalf("Size() = %+v", s)
	}
	if n := len(strings.Split(r.Render(), "\n")); n != 4 {
		t.Fatalf("Render() after resize has %d lines", n)
	}
}

func TestReplayScrollback(t *testing.T) {
	r := NewReplay(Size{W: 80, H: 24}, 5000)
	var b strings.Builder
	for i := 1; i <= 6000; i++ {
		if i > 1 {
			b.WriteString("\r\n")
		}
		fmt.Fprintf(&b, "line %d", i)
	}
	r.Write([]byte(b.String()))
	sb := r.Scrollback()
	if len(sb) != 5000 {
		t.Fatalf("len(Scrollback()) = %d, want 5000", len(sb))
	}
	if got := strings.TrimRight(plain(sb[len(sb)-1]), " "); got != "line 5976" {
		t.Fatalf("last scrollback line = %q, want line 5976", got)
	}
	if got := strings.TrimRight(plain(sb[0]), " "); got != "line 977" {
		t.Fatalf("first scrollback line = %q, want line 977", got)
	}
	if first := strings.TrimRight(plain(strings.Split(r.Render(), "\n")[0]), " "); first != "line 5977" {
		t.Fatalf("top screen line = %q, want line 5977", first)
	}
	if n := len(NewReplay(Size{W: 10, H: 2}, 0).Scrollback()); n != 0 {
		t.Fatalf("fresh scrollback = %d", n)
	}
}

func TestReplayDefaultScrollback(t *testing.T) {
	r := NewReplay(Size{W: 10, H: 2}, 0)
	r.Write([]byte(strings.Repeat("x\r\n", 2500)))
	if n := len(r.Scrollback()); n != 2000 {
		t.Fatalf("default scrollback = %d, want 2000", n)
	}
}

func TestReplayRenderRegion(t *testing.T) {
	r := NewReplay(Size{W: 200, H: 60}, 0)
	// Row 40 (0-based): "한글" starting at column 149, so the region starting
	// at column 150 cuts "한" in half; column 199 holds "Z".
	r.Write([]byte("\x1b[41;150H한글\x1b[41;200HZ\x1b[60;152Hbottom"))
	out := r.RenderRegion(150, 40, 80, 24)
	lines := strings.Split(out, "\n")
	if len(lines) != 24 {
		t.Fatalf("RenderRegion has %d lines, want 24", len(lines))
	}
	for i, l := range lines {
		if w := textwidth.Width(l); w != 80 {
			t.Fatalf("line %d width %d: %q", i, w, l)
		}
	}
	want0 := " 글" + strings.Repeat(" ", 46) + "Z" + strings.Repeat(" ", 30)
	if lines[0] != want0 {
		t.Fatalf("line 0 = %q\nwant     %q", lines[0], want0)
	}
	if want := " bottom" + strings.Repeat(" ", 73); lines[19] != want {
		t.Fatalf("line 19 = %q, want %q", lines[19], want)
	}
	for i := 20; i < 24; i++ { // below the screen
		if strings.TrimSpace(lines[i]) != "" {
			t.Fatalf("line %d below the screen not blank: %q", i, lines[i])
		}
	}
	// A wide character cut by the right edge becomes a blank.
	cut := strings.Split(r.RenderRegion(148, 40, 2, 1), "\n")[0]
	if cut != "  " {
		t.Fatalf("region ending inside a wide char = %q, want two blanks", cut)
	}
	if r.RenderRegion(0, 0, 0, 5) != "" {
		t.Fatal("empty region not empty")
	}
}
