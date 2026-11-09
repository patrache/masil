package secrets

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

const raw = "s3cr3t-value-xyz"

func TestSecretNeverPrints(t *testing.T) {
	s := New(raw)
	type holder struct {
		Exported   Secret
		unexported Secret
	}
	h := holder{Exported: s, unexported: s}
	outs := map[string]string{
		"Sprint":  fmt.Sprint(s),
		"%v":      fmt.Sprintf("%v", s),
		"%+v":     fmt.Sprintf("%+v", s),
		"%#v":     fmt.Sprintf("%#v", s),
		"%s":      fmt.Sprintf("%s", s),
		"%q":      fmt.Sprintf("%q", s),
		"%x":      fmt.Sprintf("%x", s),
		"String":  s.String(),
		"ptr %v":  fmt.Sprintf("%v", &s),
		"holder":  fmt.Sprintf("%v", h),
		"holder+": fmt.Sprintf("%+v", h),
		"holder#": fmt.Sprintf("%#v", h),
	}
	for name, out := range outs {
		if strings.Contains(out, raw) {
			t.Errorf("%s leaked the value: %q", name, out)
		}
	}
	for _, name := range []string{"Sprint", "%v", "%+v", "%#v", "%s", "String"} {
		if outs[name] != "(redacted)" {
			t.Errorf("%s = %q, want (redacted)", name, outs[name])
		}
	}

	js, err := json.Marshal(h)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(js), raw) || !strings.Contains(string(js), `"Exported":"(redacted)"`) {
		t.Errorf("json = %s", js)
	}
	txt, err := s.MarshalText()
	if err != nil || string(txt) != "(redacted)" {
		t.Errorf("MarshalText = %q, %v", txt, err)
	}

	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))
	log.Info("put", "value", s, "holder", h)
	if strings.Contains(buf.String(), raw) || !strings.Contains(buf.String(), `"value":"(redacted)"`) {
		t.Errorf("slog = %s", buf.String())
	}
	buf.Reset()
	slog.New(slog.NewTextHandler(&buf, nil)).Info("put", "value", s)
	if strings.Contains(buf.String(), raw) {
		t.Errorf("slog text = %s", buf.String())
	}

	if s.Reveal() != raw {
		t.Errorf("Reveal = %q", s.Reveal())
	}
}

func TestMasked(t *testing.T) {
	for in, want := range map[string]string{
		"abcdefgh": "••••efgh",
		"abcd":     "••••",
		"":         "••••",
		"가나다라마바":   "••••다라마바",
	} {
		if got := New(in).Masked(); got != want {
			t.Errorf("Masked(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSecretIsZero(t *testing.T) {
	var z Secret
	if !z.IsZero() || !New("").IsZero() || New("x").IsZero() {
		t.Error("IsZero")
	}
	if z.Reveal() != "" {
		t.Error("zero Reveal")
	}
}

func TestAccountNaming(t *testing.T) {
	k := Key{"leet", "masil.example", "U1", "cookie.session"}
	if got := k.Account(); got != "leet/masil.example/U1/cookie.session" {
		t.Errorf("Account = %q", got)
	}
	if Service != "masil" {
		t.Errorf("Service = %q", Service)
	}
	back, ok := parseAccount(k.Account())
	if !ok || back != k {
		t.Errorf("parseAccount = %+v, %v", back, ok)
	}
	slashName := Key{"api", "h", "U1", "env/prod/TOKEN"}
	if back, ok := parseAccount(slashName.Account()); !ok || back != slashName {
		t.Errorf("parseAccount(slash name) = %+v, %v", back, ok)
	}
	for _, bad := range []Key{
		{"", "h", "U", "n"}, {"t", "", "U", "n"}, {"t", "h", "", "n"}, {"t", "h", "U", ""},
		{"t/x", "h", "U", "n"}, {"t", "h/x", "U", "n"}, {"t", "h", "U/x", "n"}, {"t", "h", "U", "a\nb"},
	} {
		if bad.validate() == nil {
			t.Errorf("validate(%+v) = nil", bad)
		}
	}
}
