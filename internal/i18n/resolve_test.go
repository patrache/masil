package i18n_test

import (
	"slices"
	"testing"

	"github.com/patrache/masil/internal/i18n"
)

func TestParse(t *testing.T) {
	cases := []struct {
		in   string
		want i18n.Locale
		ok   bool
	}{
		{"ko_KR.UTF-8", i18n.Ko, true},
		{"ko", i18n.Ko, true},
		{"ko-KR", i18n.Ko, true},
		{"zh_CN", i18n.ZhHans, true},
		{"zh_SG", i18n.ZhHans, true},
		{"zh-Hans-CN", i18n.ZhHans, true},
		{"zh", i18n.ZhHans, true},
		{"zh_TW", i18n.ZhHans, true},
		{"zh_HK", i18n.ZhHans, true},
		{"zh_MO", i18n.ZhHans, true},
		{"zh-Hant", i18n.ZhHans, true},
		{"zh-Hant-TW", i18n.ZhHans, true},
		{"en_US", i18n.En, true},
		{"en", i18n.En, true},
		{"ja", i18n.En, true},
		{"de_DE.UTF-8", i18n.En, true},
		{"fr_FR.UTF-8@euro", i18n.En, true},
		{"C", "", false},
		{"C.UTF-8", "", false},
		{"POSIX", "", false},
		{"", "", false},
		{"   ", "", false},
		{"x-pseudo", i18n.Pseudo, true},
	}
	for _, c := range cases {
		got, ok := i18n.Parse(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("Parse(%q) = %q,%v; want %q,%v", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestResolveOrder(t *testing.T) {
	cases := []struct {
		name string
		in   i18n.Inputs
		want i18n.Resolution
	}{
		{"env bypasses gate", i18n.Inputs{Env: "en", Config: "ko", Lang: "ko_KR.UTF-8"},
			i18n.Resolution{Requested: i18n.En, Effective: i18n.En, Source: i18n.SourceEnv}},
		{"env pseudo", i18n.Inputs{Env: "x-pseudo"},
			i18n.Resolution{Requested: i18n.Pseudo, Effective: i18n.Pseudo, Source: i18n.SourceEnv}},
		{"bad env ignored", i18n.Inputs{Env: "C", Config: "ko"},
			i18n.Resolution{Requested: i18n.Ko, Effective: i18n.Ko, Source: i18n.SourceConfig}},
		{"config en closed falls to ko", i18n.Inputs{Config: "en"},
			i18n.Resolution{Requested: i18n.En, Effective: i18n.Ko, Source: i18n.SourceConfig}},
		{"legacy beats os", i18n.Inputs{Config: "auto", Legacy: true, Lang: "en_US"},
			i18n.Resolution{Requested: i18n.Ko, Effective: i18n.Ko, Source: i18n.SourceLegacy}},
		{"lc_all first", i18n.Inputs{LCAll: "zh_CN.UTF-8", LCMessages: "en_US", Lang: "ko_KR"},
			i18n.Resolution{Requested: i18n.ZhHans, Effective: i18n.Ko, Source: i18n.SourceOS}},
		{"lc_messages second", i18n.Inputs{LCMessages: "en_US", Lang: "ko_KR"},
			i18n.Resolution{Requested: i18n.En, Effective: i18n.Ko, Source: i18n.SourceOS}},
		{"lang third", i18n.Inputs{Lang: "ko_KR.UTF-8", Preferred: []string{"en-US"}},
			i18n.Resolution{Requested: i18n.Ko, Effective: i18n.Ko, Source: i18n.SourceOS}},
		{"posix C falls to preferred", i18n.Inputs{Lang: "C", Preferred: []string{"zh-Hans-CN", "en"}},
			i18n.Resolution{Requested: i18n.ZhHans, Effective: i18n.Ko, Source: i18n.SourceOS}},
		{"preferred first", i18n.Inputs{Preferred: []string{"en-US", "ko-KR"}},
			i18n.Resolution{Requested: i18n.En, Effective: i18n.Ko, Source: i18n.SourceOS}},
		{"nothing", i18n.Inputs{},
			i18n.Resolution{Requested: i18n.En, Effective: i18n.Ko, Source: i18n.SourceDefault}},
		{"config auto is not a choice", i18n.Inputs{Config: "AUTO"},
			i18n.Resolution{Requested: i18n.En, Effective: i18n.Ko, Source: i18n.SourceDefault}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := i18n.Resolve(c.in); got != c.want {
				t.Fatalf("Resolve(%+v) = %+v, want %+v", c.in, got, c.want)
			}
		})
	}
}

func TestGateFallsBackThroughEn(t *testing.T) {
	i18n.SetOpenForTest(t, i18n.Ko, i18n.En)
	got := i18n.Resolve(i18n.Inputs{Config: "zh-Hans"})
	if got.Requested != i18n.ZhHans || got.Effective != i18n.En {
		t.Fatalf("Resolve = %+v, want zh-Hans requested, en effective", got)
	}
	if !i18n.IsOpen(i18n.En) || i18n.IsOpen(i18n.ZhHans) {
		t.Fatalf("open = %v", i18n.OpenLocales())
	}
	if got := i18n.Resolve(i18n.Inputs{Config: "en"}); got.Effective != i18n.En {
		t.Fatalf("open en must be used: %+v", got)
	}
}

func TestSetOpenForTestRestores(t *testing.T) {
	t.Run("inner", func(t *testing.T) {
		i18n.SetOpenForTest(t, i18n.Ko, i18n.En, i18n.ZhHans)
		if got := i18n.OpenLocales(); !slices.Equal(got, []i18n.Locale{i18n.Ko, i18n.En, i18n.ZhHans}) {
			t.Fatalf("OpenLocales = %v", got)
		}
	})
	if got := i18n.OpenLocales(); !slices.Equal(got, []i18n.Locale{i18n.Ko}) {
		t.Fatalf("after subtest OpenLocales = %v, want [ko]", got)
	}
	if !i18n.IsOpen(i18n.Ko) || i18n.IsOpen(i18n.En) || i18n.IsOpen(i18n.ZhHans) || i18n.IsOpen(i18n.Pseudo) {
		t.Fatal("0.1.3 gate must open ko only")
	}
}

func TestNative(t *testing.T) {
	for l, want := range map[i18n.Locale]string{
		i18n.Ko: "한국어", i18n.En: "English", i18n.ZhHans: "简体中文", i18n.Pseudo: "x-pseudo", "fr": "fr",
	} {
		if got := i18n.Native(l); got != want {
			t.Errorf("Native(%q) = %q, want %q", l, got, want)
		}
	}
}

func TestSourceConstants(t *testing.T) {
	named := []i18n.Source{i18n.SourceEnv, i18n.SourceConfig, i18n.SourceLegacy, i18n.SourceOS, i18n.SourceDefault}
	for _, in := range []i18n.Inputs{
		{Env: "ko"}, {Config: "en"}, {Legacy: true}, {Lang: "ko_KR"}, {Preferred: []string{"en"}}, {},
	} {
		if s := i18n.Resolve(in).Source; !slices.Contains(named, s) {
			t.Errorf("Resolve(%+v).Source = %q, not a named constant", in, s)
		}
	}
	if !slices.Equal(i18n.Supported, []i18n.Locale{i18n.Ko, i18n.En, i18n.ZhHans}) {
		t.Fatalf("Supported = %v", i18n.Supported)
	}
}

func TestDetectOSReadsEnv(t *testing.T) {
	t.Setenv("MASIL_LANG", "en")
	t.Setenv("LC_ALL", "zh_CN.UTF-8")
	t.Setenv("LC_MESSAGES", "ja_JP")
	t.Setenv("LANG", "ko_KR.UTF-8")
	in := i18n.DetectOS()
	if in.Env != "en" || in.LCAll != "zh_CN.UTF-8" || in.LCMessages != "ja_JP" || in.Lang != "ko_KR.UTF-8" {
		t.Fatalf("DetectOS = %+v", in)
	}
	if in.Config != "" || in.Legacy {
		t.Fatalf("DetectOS must not fill config/legacy: %+v", in)
	}
}

func BenchmarkDetectOS(b *testing.B) {
	b.Setenv("LC_ALL", "")
	b.Setenv("LC_MESSAGES", "")
	b.Setenv("LANG", "")
	b.Setenv("MASIL_LANG", "")
	for b.Loop() {
		_ = i18n.DetectOS()
	}
}
