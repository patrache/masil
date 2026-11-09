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
