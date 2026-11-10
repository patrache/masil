package termhost

import "testing"

func TestParseCwd(t *testing.T) {
	cases := []struct {
		raw  string
		want string
		ok   bool
	}{
		{"file://host/Users/me/code", "/Users/me/code", true},
		{"file:///tmp", "/tmp", true},
		{"file://host/", "/", true},
		{"file://host/a%20b/C%23", "/a b/C#", true},
		{"file://host/%ED%95%9C%EA%B8%80", "/한글", true},
		{"file://host/tmp/%1b%5d52;c;aGk=%07x", "", false}, // ESC/BEL smuggled in
		{"file://host/tmp/%c2%9bx", "", false},             // C1 CSI
		{"file://host/tmp/%7fx", "", false},                // DEL
		{"file://host/tmp/a%e2%80%aeb", "", false},         // bidi override
		{"file://host/tmp/a%0ab", "", false},               // newline
		{"file://host/tmp/%ff", "", false},                 // invalid UTF-8
		{"file://host/a/../etc", "", false},                // not clean
		{"file://host/a//b", "", false},
		{"http://host/tmp", "", false},
		{"file://host", "", false},
		{"relative", "", false},
	}
	for _, c := range cases {
		got, ok := parseCwd(c.raw)
		if got != c.want || ok != c.ok {
			t.Errorf("parseCwd(%q) = %q, %v; want %q, %v", c.raw, got, ok, c.want, c.ok)
		}
	}
}

// testScreen is a vtScreen with its Responses pipe drained, stopped at cleanup.
func testScreen(t *testing.T, s Size) *vtScreen {
	t.Helper()
	v := newVTScreen(s, 0, screenHooks{})
	go v.Responses(func([]byte) {})
	t.Cleanup(v.stop)
	return v
}

// TestSnapshotCarriesState: a screen rebuilt from Snapshot and then fed the
// same later bytes must end up identical to the source — the snapshot carries
// the pen, margins, modes, saved cursor and an unfinished sequence, not only
// the cells (AttachTap's "nothing lost" promise).
func TestSnapshotCarriesState(t *testing.T) {
	cases := []struct{ name, before, after string }{
		{"pen", "a\x1b[31m", "RED"},
		{"pen rich", "\x1b[1;3;4;48;5;22;38;2;1;2;3mx", "Y\x1b[22mZ"},
		{"pen reset by RIS", "\x1b[31m\x1bc", "plain"},
		{"scroll region", "1\r\n2\r\n3\r\n4\r\n5\x1b[2;4r\x1b[4;1H", "\n\nA\nB"},
		{"scroll region alt", "\x1b[?1049h\x1b[2;3r\x1b[3;1Hq", "\n\nA\nB"},
		{"autowrap off", "\x1b[?7l", "0123456789abcdefghijklmnop"},
		{"autowrap back on", "\x1b[?7l\x1b[?7h", "0123456789abcdefghijklmnop"},
		{"origin mode", "\x1b[3;5r\x1b[?6h", "\x1b[1;1HX\x1b[9;1HY"},
		{"left right margins", "\x1b[?69h\x1b[3;8s\x1b[1;3H", "abcdefghijklm\rZ"},
		{"newline mode", "\x1b[20h", "a\nb"},
		{"saved cursor ESC 7", "\x1b[3;5H\x1b[32m\x1b7\x1b[0m\x1b[1;1H", "\x1b8G"},
		{"saved cursor CSI s", "\x1b[2;2H\x1b[33m\x1b[s\x1b[0m\x1b[5;5H", "\x1b8Y"},
		{"saved cursor 1048", "\x1b[4;4H\x1b[34m\x1b[?1048h\x1b[0m\x1b[1;1H", "\x1b[?1048lB"},
		{"alt screen pen", "\x1b[35m\x1b[?1049h", "Z"},
		{"split CSI", "hello\x1b[3", "1mX"},
		{"split UTF-8", "hi \xed\x95", "\x9c!"},
		{"split OSC", "a\x1b]0;tit", "le\x07b"},
		{"split ESC", "a\x1b", "[1mB"},
		{"line drawing G0", "\x1b(0", "lqqk"},
		{"line drawing G0 back", "\x1b(0\x1b(B", "lqqk"},
	}
	size := Size{W: 20, H: 6}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := testScreen(t, size)
			src.Write([]byte(c.before))
			dst := testScreen(t, size)
			dst.Write(src.Snapshot())
			src.Write([]byte(c.after))
			dst.Write([]byte(c.after))
			got, want := dst.RenderRegion(0, 0, size.W, size.H), src.RenderRegion(0, 0, size.W, size.H)
			if got != want {
				t.Fatalf("replay differs\n got %q\nwant %q", got, want)
			}
			sx, sy, sv := src.Cursor()
			dx, dy, dv := dst.Cursor()
			if sx != dx || sy != dy || sv != dv {
				t.Fatalf("cursor replay = %d,%d,%v, source = %d,%d,%v", dx, dy, dv, sx, sy, sv)
			}
		})
	}
}
